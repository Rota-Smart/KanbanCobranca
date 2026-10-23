package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"rota-smart/internal/db"
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

	// Renegociação: histórico de cobranças no app + tickets de Cobrança no 360.
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
				for _, t := range dash.Sources.Octadesk.Data.TicketsRecentes {
					if contemCobranca(t.Summary) {
						av.Renegociou = true
						av.QtdRenegociacoes++
					}
				}
				if av.Renegociou && len(av.Observacoes) < 5 {
					av.Observacoes = append(av.Observacoes, "Histórico do 360 mostra atendimento(s) de cobrança anterior(es).")
				}
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

	return av, nil
}

// contemCobranca identifica títulos de cobrança no histórico (ex.: "Cobrança — NOME (n títulos...)").
func contemCobranca(summary string) bool {
	s := strings.ToLower(summary)
	return strings.HasPrefix(s, "cobrança") || strings.HasPrefix(s, "cobranca")
}

// ResumoUmaLinha formata a avaliação para injetar no prompt da IA.
func (a *Avaliacao) ResumoUmaLinha() string {
	s := fmt.Sprintf("renegociou=%v(%d) fiel=%v mesesDeCasa=%d elegivelCortesia=%v",
		a.Renegociou, a.QtdRenegociacoes, a.Fiel, a.MesesDeCasa, a.ElegivelCortesia)
	return s
}
