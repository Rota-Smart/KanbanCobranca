package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"rota-smart/internal/db"
	"rota-smart/internal/rota360"
)

// Avaliacao resume o perfil do cliente para a negociação — calculada em Go
// a partir do snapshot do 360 (cache) e das cobranças locais. Nunca é
// fonte de valores financeiros: só de contexto.
type Avaliacao struct {
	Renegociou       bool     `json:"renegociou"`
	QtdRenegociacoes int      `json:"qtdRenegociacoes"`
	Fiel             bool     `json:"fiel"`
	MesesDeCasa      int      `json:"mesesDeCasa"`
	ElegivelCortesia bool     `json:"elegivelCortesia"`
	MensalidadeCents int64    `json:"mensalidadeCents"`
	Observacoes      []string `json:"observacoes"`
}

// Avaliador calcula a avaliação do cliente.
type Avaliador struct {
	clientes  *db.Cliente360Repo
	cobrancas *db.CobrancaRepo
	agora     func() time.Time
}

func NovoAvaliador(clientes *db.Cliente360Repo, cobrancas *db.CobrancaRepo, agora func() time.Time) *Avaliador {
	return &Avaliador{clientes: clientes, cobrancas: cobrancas, agora: agora}
}

// Avaliar combina o snapshot do 360 com o histórico local do ticket.
// ticketCodService é o código service do ticket; ticketID liga ao histórico
// de cobranças locais (renegociações anteriores registradas no app).
func (a *Avaliador) Avaliar(ctx context.Context, ticketID int64, codService string) (*Avaliacao, error) {
	av := &Avaliacao{Observacoes: []string{}}
	cliente, err := a.clientes.Get(ctx, codService)
	if err != nil {
		return nil, err
	}

	// Renegociação: SOMENTE indícios no 360 (padrão de boletos em curso —
	// ver abaixo). Acordos salvos no app não contam: o PrepararCobranca
	// grava acordo padrão ao entrar na Cobrança e propostas antigas geradas
	// sob regras anteriores poluem o histórico (falso "renegociou").

	if cliente != nil {
		var dash struct {
			Sources struct {
				Octadesk struct {
					Data struct {
						TicketsRecentes []struct {
							Summary string `json:"summary"`
							Status  struct {
								Name string `json:"name"`
							} `json:"status"`
						} `json:"tickets_recentes"`
					} `json:"data"`
				} `json:"octadesk"`
				Produtos struct {
					Data []struct {
						Descricao            string    `json:"descricao"`
						ValorTotal           float64   `json:"valorTotal"`
						PrimeiraMovimentacao time.Time `json:"primeiraMovimentacao"`
					} `json:"data"`
				} `json:"produtos"`
			} `json:"sources"`
		}
		if cliente.HistoricoJSON != "" {
			if err := json.Unmarshal([]byte(cliente.HistoricoJSON), &dash); err == nil {
				// Fidelidade/tempo de casa: produto mais antigo.
				var maisAntigo time.Time
				var mensalidade float64
				for _, p := range dash.Sources.Produtos.Data {
					if p.ValorTotal > mensalidade {
						mensalidade = p.ValorTotal
					}
					if !p.PrimeiraMovimentacao.IsZero() && (maisAntigo.IsZero() || p.PrimeiraMovimentacao.Before(maisAntigo)) {
						maisAntigo = p.PrimeiraMovimentacao
					}
				}
				if !maisAntigo.IsZero() {
					av.MesesDeCasa = int(a.agora().Sub(maisAntigo).Hours() / (24 * 30))
				}
				av.MensalidadeCents = int64(mensalidade * 100)
				av.Fiel = av.MesesDeCasa >= 12
				if av.Fiel {
					av.Observacoes = append(av.Observacoes, fmt.Sprintf("Cliente há %d meses — fidelidade considerada.", av.MesesDeCasa))
				}
				// Cortesia: ativo há mais de 1 ano (regra do usuário).
				av.ElegivelCortesia = av.MesesDeCasa > 12
				if av.ElegivelCortesia {
					av.Observacoes = append(av.Observacoes, "Ativo há mais de 1 ano: avaliar elegibilidade a cortesia/desconto conforme parâmetros.")
				}
			}
		} else {
			av.Observacoes = append(av.Observacoes, "Sem snapshot do 360 no cache — avaliação limitada.")
		}
	} else {
		av.Observacoes = append(av.Observacoes, "Cliente ainda não sincronizado do 360.")
	}

	// Boletos em aberto do cache: padrão de renegociação em andamento.
	// Padrões só em boletos pagos (renegociação quitada) NÃO marcam —
	// quitada, o cliente volta a pagar 20% de entrada (Política v7).
	if faturas := faturasDoCache(cliente); len(faturas) > 0 {
		if motivo := renegociacaoPorBoletos(faturas, av.MensalidadeCents, a.agora()); motivo != "" {
			av.Renegociou = true
			if len(av.Observacoes) < 5 {
				av.Observacoes = append(av.Observacoes, motivo)
			}
		}
	}

	return av, nil
}

// Renegociação encerrada (quitada) NÃO conta como "já renegociou": depois
// de quitada, o cliente volta a ter entrada mínima de 20% (Política v7).
// Por isso os padrões abaixo só olham boletos EM ABERTO — rastro de boleto
// pago não marca nada. Também não é renegociação uma cobrança pontual
// (um único mês com dois boletos), só padrão repetido/em curso.

// faturasDoCache decodifica as faturas em aberto guardadas no snapshot.
func faturasDoCache(cli *db.Cliente360) []rota360.Fatura360 {
	if cli == nil || cli.FaturasJSON == "" {
		return []rota360.Fatura360{}
	}
	var faturas []rota360.Fatura360
	if json.Unmarshal([]byte(cli.FaturasJSON), &faturas) != nil {
		return []rota360.Fatura360{}
	}
	return faturas
}

// renegociacaoPorBoletos aplica as regras de renegociação em andamento sobre
// as faturas em aberto e devolve o motivo ("" = não é renegociação):
//   - algum mês nos últimos 12 meses com 2 boletos EM ATRASO no mesmo mês;
//   - boleto com vencimento FUTURO em aberto com valor diferente da
//     mensalidade — só quando há evidência de parcelamento (≥2 boletos
//     futuros em aberto ou o valor divergente se repetindo); um único
//     futuro divergente é variação da mensalidade;
//   - mês atual ou mês anterior com 2 boletos de valores diferentes.
//
// Boletos com selo Mensalidade no 360 (categoria "mensalidade" do ERP) são a
// cobrança recorrente do contrato: mesmo com valor diferente — proporcional,
// acréscimo, reajuste — são só variação do valor da mensalidade. Saem de
// TODAS as regras e, quando presentes, definem sozinhos a referência.
//
// Um único mês passado (nem atual/anterior, nem em atraso) com dois valores
// é cobrança pontual e não conta. Tudo pago = renegociação quitada, que
// também não conta.
func renegociacaoPorBoletos(faturas []rota360.Fatura360, mensalidadeCents int64, agora time.Time) string {
	inicioJanela := agora.AddDate(-1, 0, 0)
	porMes := map[string][]int64{}   // mês AAAA-MM → valores em centavos (em aberto, fora mensalidade)
	atrasoPorMes := map[string]int{} // mês → quantos boletos em aberto já venceram (total, com mensalidade)
	atrasoMensal := map[string]int{} // mês → desses, quantos são só variação da mensalidade
	var mensalidadeValores []int64   // valores das parcelas com selo Mensalidade
	for _, f := range faturas {
		if f.Vencimento.IsZero() || f.ValorDuplicata <= 0 {
			continue
		}
		valor := int64(math.Round(f.ValorDuplicata * 100))
		mensal := rota360.CategoriaEhMensalidade(f.Categoria)
		if mensal {
			mensalidadeValores = append(mensalidadeValores, valor)
		} else {
			mes := f.Vencimento.Format("2006-01")
			porMes[mes] = append(porMes[mes], valor)
		}
		if f.Vencimento.Before(agora) {
			mes := f.Vencimento.Format("2006-01")
			atrasoPorMes[mes]++
			if mensal {
				atrasoMensal[mes]++
			}
		}
	}

	// 1) dois boletos em aberto e em atraso no mesmo mês, nos últimos 12 meses
	// (exceto quando TODOS os do mês são variação da mensalidade).
	for mes, n := range atrasoPorMes {
		if n < 2 || n == atrasoMensal[mes] {
			continue
		}
		if dataMes, err := time.Parse("2006-01", mes); err != nil ||
			dataMes.Before(inicioJanela) || dataMes.After(agora) {
			continue
		}
		return "Dois boletos em aberto e em atraso no mesmo mês — renegociação em andamento."
	}

	// 2) boleto futuro em aberto com valor diferente da mensalidade — MAS
	// só quando há evidência de parcelamento em curso: um ÚNICO boleto
	// futuro divergente é variação da mensalidade (desconto, proporcional,
	// reajuste — caso real do 14535: série de 144 atrasada e o futuro de
	// novembro em 129). O dashboard360 não devolve a categoria do ERP
	// (sempre vazia — conferido ao vivo), então o padrão é o único sinal.
	// Evidência: ≥2 boletos futuros em aberto (parcela de acordo) ou o
	// valor divergente se repetindo entre os boletos em aberto.
	// Referência: 1º os valores das parcelas com selo Mensalidade (a fonte
	// mais confiável — o ERP diz o que é a mensalidade); 2º o valor que se
	// REPETE no histórico; 3º o valorTotal máximo dos produtos do snapshot.
	referencia := mensalidadeCents
	if len(mensalidadeValores) > 0 {
		referencia = valorQueMaisRepeteValores(mensalidadeValores)
	} else if n := contagemMaxima(porMes); n >= 2 {
		referencia = valorQueMaisRepete(porMes)
	}
	if referencia > 0 {
		futuros := 0
		divergente := int64(0)
		for _, f := range faturas {
			if !f.Vencimento.After(agora) || rota360.CategoriaEhMensalidade(f.Categoria) {
				continue
			}
			valor := int64(math.Round(f.ValorDuplicata * 100))
			futuros++
			if valor != referencia {
				divergente = valor
			}
		}
		repetidos := 0
		if divergente > 0 {
			for _, valores := range porMes {
				for _, v := range valores {
					if v == divergente {
						repetidos++
					}
				}
			}
		}
		if divergente > 0 && (futuros >= 2 || repetidos >= 2) {
			return "Boleto futuro em aberto com valor diferente da mensalidade — renegociação em andamento."
		}
	}

	// 3) mês atual ou anterior com dois boletos de valores diferentes
	distintos := func(valores []int64) bool {
		for i := 0; i < len(valores); i++ {
			for j := i + 1; j < len(valores); j++ {
				if valores[i] != valores[j] {
					return true
				}
			}
		}
		return false
	}
	mesAtual := agora.Format("2006-01")
	mesAnterior := agora.AddDate(0, -1, 0).Format("2006-01")
	for _, mes := range []string{mesAtual, mesAnterior} {
		if distintos(porMes[mes]) {
			return "Dois boletos de valores diferentes neste mês ou no anterior — renegociação em andamento."
		}
	}

	return ""
}

// contagemMaxima devolve quantas vezes o valor de boleto mais frequente
// aparece somados os meses (1 = todos os boletos têm valores distintos).
func contagemMaxima(porMes map[string][]int64) int {
	contagem := map[int64]int{}
	for _, valores := range porMes {
		for _, v := range valores {
			contagem[v]++
		}
	}
	max := 0
	for _, n := range contagem {
		if n > max {
			max = n
		}
	}
	return max
}

// valorQueMaisRepete devolve o valor de boleto mais frequente no histórico
// (a mensalidade real deduzida dos próprios boletos).
func valorQueMaisRepete(porMes map[string][]int64) int64 {
	var valores []int64
	for _, vs := range porMes {
		valores = append(valores, vs...)
	}
	return valorQueMaisRepeteValores(valores)
}

// valorQueMaisRepeteValores devolve o valor mais frequente da lista
// (empate = valor maior). É o mesmo critério de valorQueMaisRepete, mas para
// uma lista plana — usado para deduzir a mensalidade das parcelas com selo.
func valorQueMaisRepeteValores(valores []int64) int64 {
	contagem := map[int64]int{}
	for _, v := range valores {
		contagem[v]++
	}
	var melhor int64
	nMelhor := 0
	for v, n := range contagem {
		if n > nMelhor || (n == nMelhor && v > melhor) {
			melhor, nMelhor = v, n
		}
	}
	return melhor
}

// ResumoUmaLinha formata a avaliação para injetar no prompt da IA.
func (a *Avaliacao) ResumoUmaLinha() string {
	s := fmt.Sprintf("renegociou=%v(%d) fiel=%v mesesDeCasa=%d elegivelCortesia=%v",
		a.Renegociou, a.QtdRenegociacoes, a.Fiel, a.MesesDeCasa, a.ElegivelCortesia)
	return s
}
