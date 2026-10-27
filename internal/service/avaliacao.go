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

	// Renegociação: histórico de cobranças no app + indícios no 360
	// (documento de renegociação/confissão de dívida nos títulos, ou o
	// padrão de boletos de renegociação — ver abaixo).
	if n, err := a.clientes.ContaRenegociacoesTicket(ctx, ticketID); err == nil && n > 0 {
		av.Renegociou = true
		av.QtdRenegociacoes += n
		av.Observacoes = append(av.Observacoes, fmt.Sprintf("Já possui %d acordo(s) registrado(s) no app.", n))
	}

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
//   - boleto com vencimento FUTURO em aberto com valor diferente da mensalidade;
//   - mês atual ou mês anterior com 2 boletos de valores diferentes.
//
// Um único mês passado (nem atual/anterior, nem em atraso) com dois valores
// é cobrança pontual e não conta. Tudo pago = renegociação quitada, que
// também não conta.
func renegociacaoPorBoletos(faturas []rota360.Fatura360, mensalidadeCents int64, agora time.Time) string {
	inicioJanela := agora.AddDate(-1, 0, 0)
	porMes := map[string][]int64{}  // mês AAAA-MM → valores em centavos (em aberto)
	atrasoPorMes := map[string]int{} // mês → quantos boletos em aberto já venceram
	for _, f := range faturas {
		if f.Vencimento.IsZero() || f.ValorDuplicata <= 0 {
			continue
		}
		mes := f.Vencimento.Format("2006-01")
		porMes[mes] = append(porMes[mes], int64(math.Round(f.ValorDuplicata*100)))
		if f.Vencimento.Before(agora) {
			atrasoPorMes[mes]++
		}
	}

	// 1) dois boletos em aberto e em atraso no mesmo mês, nos últimos 12 meses
	for mes, n := range atrasoPorMes {
		if n < 2 {
			continue
		}
		if dataMes, err := time.Parse("2006-01", mes); err != nil ||
			dataMes.Before(inicioJanela) || dataMes.After(agora) {
			continue
		}
		return "Dois boletos em aberto e em atraso no mesmo mês — renegociação em andamento."
	}

	// 2) boleto futuro em aberto com valor diferente da mensalidade
	if mensalidadeCents > 0 {
		for _, f := range faturas {
			if f.Vencimento.After(agora) && int64(math.Round(f.ValorDuplicata*100)) != mensalidadeCents {
				return "Boleto futuro em aberto com valor diferente da mensalidade — renegociação em andamento."
			}
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

// ResumoUmaLinha formata a avaliação para injetar no prompt da IA.
func (a *Avaliacao) ResumoUmaLinha() string {
	s := fmt.Sprintf("renegociou=%v(%d) fiel=%v mesesDeCasa=%d elegivelCortesia=%v",
		a.Renegociou, a.QtdRenegociacoes, a.Fiel, a.MesesDeCasa, a.ElegivelCortesia)
	return s
}
