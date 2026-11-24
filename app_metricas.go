package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"rota-smart/internal/contasync"
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
// Reserva: usado só quando a nuvem não responde — os números oficiais do
// painel vêm de GetMetricasNuvem (mesmo banco e fórmula do site).
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
		`SELECT count(*) FROM tickets WHERE NOT apagado AND coluna_atual = ?`, ColunaVisitaAgendada); err != nil {
		return nil, err
	}
	hoje := time.Now().Format("2006-01-02")
	if m.VisitasHoje, err = metricasUmValor(ctx, a.db,
		`SELECT count(*) FROM tickets WHERE NOT apagado AND coluna_atual = ? AND visita_data = ?`, ColunaVisitaAgendada, hoje); err != nil {
		return nil, err
	}
	// As janelas usam strftime no formato das colunas ('...T...Z'); o texto do
	// datetime('now') não casa e a comparação de string estoura a borda do dia.
	if m.EntradasSemana, err = metricasUmValor(ctx, a.db,
		`SELECT count(*) FROM tickets WHERE NOT apagado AND created_at >= strftime('%Y-%m-%dT%H:%M:%SZ','now','-7 days')`); err != nil {
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
		`SELECT coluna_atual, count(*) FROM tickets WHERE NOT apagado GROUP BY coluna_atual ORDER BY count(*) DESC`)
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

// respostaNuvem espelha o JSON do GET /metricas do Site-Back (campos sem
// tag: nome Go exportado). Convertido para o Metricas do app logo abaixo.
type respostaNuvem struct {
	PorColuna []struct {
		Coluna string `json:"coluna"`
		Total  int    `json:"total"`
	} `json:"PorColuna"`
	TotalCards         int     `json:"TotalCards"`
	EntradasSemana     int     `json:"EntradasSemana"`
	FechadosSemana     int     `json:"FechadosSemana"`
	TempoMedioDias     float64 `json:"TempoMedioDias"`
	CobrancasAtivas    int     `json:"CobrancasAtivas"`
	ValorEmAbertoCents int64   `json:"ValorEmAbertoCents"`
	VisitasAgendadas   int     `json:"VisitasAgendadas"`
	VisitasHoje        int     `json:"VisitasHoje"`
}

// GetMetricasNuvem devolve o painel do MESMO banco e fórmula do site: lê o
// GET /metricas do Site-Back com o token da conta (os mesmos números que o
// navegador mostra, calculados no Postgres). O cálculo local (GetMetricas)
// vira reserva para quando a nuvem não responde.
func (a *App) GetMetricasNuvem() (*Metricas, error) {
	token, err := lerTokenConta()
	if err != nil || token == "" {
		return nil, fmt.Errorf("conta não pareada")
	}
	req, err := http.NewRequestWithContext(a.ctx, http.MethodGet, contasync.URLPadrao+"/metricas", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpSessaoConta.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("a nuvem respondeu HTTP %d para /metricas", resp.StatusCode)
	}
	var bruto respostaNuvem
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&bruto); err != nil {
		return nil, err
	}
	m := &Metricas{
		TotalCards:         bruto.TotalCards,
		EntradasSemana:     bruto.EntradasSemana,
		FechadosSemana:     bruto.FechadosSemana,
		TempoMedioDias:     bruto.TempoMedioDias,
		CobrancasAtivas:    bruto.CobrancasAtivas,
		ValorEmAbertoCents: bruto.ValorEmAbertoCents,
		VisitasAgendadas:   bruto.VisitasAgendadas,
		VisitasHoje:        bruto.VisitasHoje,
		PorColuna:          []MetricaColuna{},
	}
	for _, c := range bruto.PorColuna {
		m.PorColuna = append(m.PorColuna, MetricaColuna{Coluna: c.Coluna, Total: c.Total})
	}
	return m, nil
}
