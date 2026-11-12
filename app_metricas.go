package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// MetricaColuna é uma linha do resumo "cards por coluna".
type MetricaColuna struct {
	Coluna string `json:"coluna"`
	Total  int    `json:"total"`
}

// Metricas é o que o painel 📊 mostra: uma foto do quadro agora.
type Metricas struct {
	PorColuna          []MetricaColuna `json:"porColuna"`
	TotalCards         int             `json:"totalCards"`
	EntradasSemana     int             `json:"entradasSemana"`
	FechadosSemana     int             `json:"fechadosSemana"`
	TempoMedioDias     float64         `json:"tempoMedioDias"` // do card nascer até chegar em Ticket Fechado (últimos 30 dias)
	CobrancasAtivas    int             `json:"cobrancasAtivas"`
	ValorEmAbertoCents int64           `json:"valorEmAbertoCents"` // soma dos acordos ativos
	VisitasAgendadas   int             `json:"visitasAgendadas"`
	VisitasHoje        int             `json:"visitasHoje"`
}

// GetMetricas agrega do banco local; não vai na rede nem no 360.
func (a *App) GetMetricas() (*Metricas, error) {
	if a.db == nil {
		return nil, fmt.Errorf("banco indisponível nesta instalação")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m := &Metricas{PorColuna: []MetricaColuna{}}
	var err error
	if m.PorColuna, err = metricasPorColuna(ctx, a.db); err != nil {
		return nil, err
	}
	for _, c := range m.PorColuna {
		m.TotalCards += c.Total
	}
	if m.VisitasAgendadas, err = metricasUmValor(ctx, a.db,
		`SELECT count(*) FROM tickets WHERE coluna_atual = ?`, ColunaVisitaAgendada); err != nil {
		return nil, err
	}
	hoje := time.Now().Format("2006-01-02")
	if m.VisitasHoje, err = metricasUmValor(ctx, a.db,
		`SELECT count(*) FROM tickets WHERE coluna_atual = ? AND visita_data = ?`, ColunaVisitaAgendada, hoje); err != nil {
		return nil, err
	}
	// As janelas usam strftime no formato das colunas ('...T...Z'); o texto do
	// datetime('now') não casa e a comparação de string estoura a borda do dia.
	if m.EntradasSemana, err = metricasUmValor(ctx, a.db,
		`SELECT count(*) FROM tickets WHERE created_at >= strftime('%Y-%m-%dT%H:%M:%SZ','now','-7 days')`); err != nil {
		return nil, err
	}
	if m.FechadosSemana, err = metricasUmValor(ctx, a.db,
		`SELECT count(*) FROM movimentacoes WHERE para_coluna = ? AND created_at >= strftime('%Y-%m-%dT%H:%M:%SZ','now','-7 days')`,
		ColunaTicketFechado); err != nil {
		return nil, err
	}
	if m.TempoMedioDias, err = metricasUmFloat(ctx, a.db,
		`SELECT AVG(julianday(m.created_at) - julianday(t.created_at))
		 FROM movimentacoes m JOIN tickets t ON t.id = m.ticket_id
		 WHERE m.para_coluna = ? AND m.created_at >= strftime('%Y-%m-%dT%H:%M:%SZ','now','-30 days')`,
		ColunaTicketFechado); err != nil {
		return nil, err
	}
	var valor sql.NullInt64
	err = a.db.QueryRowContext(ctx,
		`SELECT count(*), COALESCE(SUM(final_cents),0) FROM cobrancas WHERE ativa = 1`,
	).Scan(&m.CobrancasAtivas, &valor)
	if err != nil {
		return nil, err
	}
	m.ValorEmAbertoCents = valor.Int64
	return m, nil
}

func metricasPorColuna(ctx context.Context, banco *sql.DB) ([]MetricaColuna, error) {
	linhas, err := banco.QueryContext(ctx,
		`SELECT coluna_atual, count(*) FROM tickets GROUP BY coluna_atual ORDER BY count(*) DESC`)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()
	saida := []MetricaColuna{}
	for linhas.Next() {
		var c MetricaColuna
		if err := linhas.Scan(&c.Coluna, &c.Total); err != nil {
			return nil, err
		}
		saida = append(saida, c)
	}
	return saida, linhas.Err()
}

func metricasUmValor(ctx context.Context, banco *sql.DB, q string, args ...any) (int, error) {
	var n int
	err := banco.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}

func metricasUmFloat(ctx context.Context, banco *sql.DB, q string, args ...any) (float64, error) {
	var f sql.NullFloat64
	err := banco.QueryRowContext(ctx, q, args...).Scan(&f)
	return f.Float64, err
}
