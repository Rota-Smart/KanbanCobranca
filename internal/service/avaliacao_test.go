package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rota-smart/internal/db"
	"rota-smart/internal/rota360"
)

func avaliadorDeTeste(t *testing.T) (*Avaliador, *db.Cliente360Repo) {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "teste.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	clientes := db.NewCliente360Repo(database)
	cobrancas := db.NewCobrancaRepo(database)
	return NovoAvaliador(clientes, cobrancas, func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) }), clientes
}

func TestAvaliarClienteAntigo(t *testing.T) {
	av, clientes := avaliadorDeTeste(t)
	ctx := context.Background()

	hist := map[string]any{
		"sources": map[string]any{
			"octadesk": map[string]any{
				"data": map[string]any{
					"tickets_recentes": []any{
						map[string]any{"summary": "Renegociação — X (confissão de dívida)", "status": map[string]any{"name": "Resolvido"}},
						map[string]any{"summary": "Cobrança — X (1 título, 61 dias)", "status": map[string]any{"name": "Em andamento"}},
					},
				},
			},
			"produtos": map[string]any{
				"data": []any{
					map[string]any{"descricao": "Central", "valorTotal": 120.0, "primeiraMovimentacao": "2024-01-15T00:00:00Z"},
				},
			},
		},
	}
	histJSON, _ := json.Marshal(hist)
	if err := clientes.Upsert(ctx, &db.Cliente360{CodService: "123", Nome: "X", SaldoCents: 5000, HistoricoJSON: string(histJSON)}); err != nil {
		t.Fatal(err)
	}

	avaliacao, err := av.Avaliar(ctx, 1, "123")
	if err != nil {
		t.Fatal(err)
	}
	// Título de ticket não é mais fonte de renegociação (falso positivo);
	// sem acordos no app e sem boletos, Renegociou fica falso.
	if avaliacao.Renegociou {
		t.Errorf("título de ticket não deveria marcar renegociou: %+v", avaliacao)
	}
	if !avaliacao.Fiel || !avaliacao.ElegivelCortesia {
		t.Errorf("esperava fiel+cortesia, veio %+v", avaliacao)
	}
	if avaliacao.MesesDeCasa < 12 {
		t.Errorf("mesesDeCasa=%d, esperava >= 12", avaliacao.MesesDeCasa)
	}
	if avaliacao.MensalidadeCents != 12000 {
		t.Errorf("mensalidade=%d, esperava 12000", avaliacao.MensalidadeCents)
	}
	if len(avaliacao.Observacoes) == 0 {
		t.Error("esperava observações")
	}
	if !strings.Contains(avaliacao.ResumoUmaLinha(), "renegociou=false") {
		t.Errorf("resumo errado: %s", avaliacao.ResumoUmaLinha())
	}
}

// Ticket de cobrança comum (o próprio da fila atual) NÃO é prova de
// renegociação — era a fonte de falso positivo da regra antiga.
func TestAvaliarCobrancaSimplesNaoRenegociou(t *testing.T) {
	av, clientes := avaliadorDeTeste(t)
	ctx := context.Background()

	hist := map[string]any{
		"sources": map[string]any{
			"octadesk": map[string]any{
				"data": map[string]any{
					"tickets_recentes": []any{
						map[string]any{"summary": "Cobrança — Y (2 títulos, 30 dias)", "status": map[string]any{"name": "Em andamento"}},
					},
				},
			},
			"produtos": map[string]any{
				"data": []any{
					map[string]any{"descricao": "Central", "valorTotal": 100.0, "primeiraMovimentacao": "2024-01-15T00:00:00Z"},
				},
			},
		},
	}
	histJSON, _ := json.Marshal(hist)
	if err := clientes.Upsert(ctx, &db.Cliente360{CodService: "123", Nome: "Y", HistoricoJSON: string(histJSON)}); err != nil {
		t.Fatal(err)
	}

	avaliacao, err := av.Avaliar(ctx, 1, "123")
	if err != nil {
		t.Fatal(err)
	}
	if avaliacao.Renegociou {
		t.Errorf("cobrança comum não deveria marcar renegociou: %+v", avaliacao)
	}
}

// Regras de boletos da Política v7 (agora fixo do avaliador = 2026-10-22):
// renegociação em andamento só pelos padrões em aberto; pago não marca.
func TestRenegociacaoPorBoletos(t *testing.T) {
	agora := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	d := func(m time.Month, dia int) time.Time { return time.Date(2026, m, dia, 0, 0, 0, 0, time.UTC) }

	casos := []struct {
		nome        string
		faturas     []rota360.Fatura360
		mensalidade int64
		contem      string // "" = não é renegociação
	}{
		{
			nome: "dois boletos em atraso no mesmo mês",
			faturas: []rota360.Fatura360{
				{Vencimento: d(time.August, 5), ValorDuplicata: 100},
				{Vencimento: d(time.August, 15), ValorDuplicata: 57.6},
			},
			contem: "atraso",
		},
		{
			nome:        "boleto futuro com valor diferente da mensalidade",
			faturas:     []rota360.Fatura360{{Vencimento: d(time.November, 10), ValorDuplicata: 57.6}},
			mensalidade: 10000,
			contem:      "futuro",
		},
		{
			nome: "mês atual e anterior com dois valores",
			faturas: []rota360.Fatura360{
				{Vencimento: d(time.October, 10), ValorDuplicata: 100},
				{Vencimento: d(time.October, 25), ValorDuplicata: 57.6}, // ainda não venceu
				{Vencimento: d(time.September, 5), ValorDuplicata: 100},
				{Vencimento: d(time.September, 15), ValorDuplicata: 57.6},
			},
			contem: "mês",
		},
		{
			nome: "cobrança pontual: um mês antigo com valor extra (mensalidade quitada)",
			faturas: []rota360.Fatura360{
				{Vencimento: d(time.June, 5), ValorDuplicata: 100}, // só a mensalidade em aberto
			},
			mensalidade: 10000,
			contem:      "",
		},
		{
			nome: "futuro igual à mensalidade não é renegociação",
			faturas: []rota360.Fatura360{
				{Vencimento: d(time.November, 10), ValorDuplicata: 100},
			},
			mensalidade: 10000,
			contem:      "",
		},
	}
	for _, c := range casos {
		got := renegociacaoPorBoletos(c.faturas, c.mensalidade, agora)
		if c.contem == "" && got != "" {
			t.Errorf("%s: esperava vazio, veio %q", c.nome, got)
		}
		if c.contem != "" && !strings.Contains(got, c.contem) {
			t.Errorf("%s: esperava motivo com %q, veio %q", c.nome, c.contem, got)
		}
	}
}

// Padrão de dois valores por mês só em boletos que chegaram do 360 como
// em aberto marca renegociação pela avaliação completa (mensalidade vem
// dos produtos).
func TestAvaliarBoletosRenegociacaoEmAndamento(t *testing.T) {
	av, clientes := avaliadorDeTeste(t)
	ctx := context.Background()

	faturas := []map[string]any{
		{"Vencimento": "2026-10-10T00:00:00Z", "ValorDuplicata": 100.0},
		{"Vencimento": "2026-10-25T00:00:00Z", "ValorDuplicata": 57.6},
	}
	fatJSON, _ := json.Marshal(faturas)

	hist := map[string]any{
		"sources": map[string]any{
			"octadesk": map[string]any{"data": map[string]any{"tickets_recentes": []any{}}},
			"produtos": map[string]any{"data": []any{}},
		},
	}
	histJSON, _ := json.Marshal(hist)
	if err := clientes.Upsert(ctx, &db.Cliente360{
		CodService: "123", Nome: "Z",
		HistoricoJSON: string(histJSON),
		FaturasJSON:   string(fatJSON),
	}); err != nil {
		t.Fatal(err)
	}

	avaliacao, err := av.Avaliar(ctx, 1, "123")
	if err != nil {
		t.Fatal(err)
	}
	if !avaliacao.Renegociou {
		t.Errorf("esperava renegociou=true pelo padrão de boletos: %+v", avaliacao)
	}
}

func TestAvaliarClienteSemCache(t *testing.T) {
	av, _ := avaliadorDeTeste(t)
	avaliacao, err := av.Avaliar(context.Background(), 1, "999")
	if err != nil {
		t.Fatal(err)
	}
	if avaliacao.Renegociou || avaliacao.Fiel {
		t.Errorf("sem dados não deveria afirmar nada: %+v", avaliacao)
	}
	for _, o := range avaliacao.Observacoes {
		if !strings.Contains(o, "não sincronizado") {
			t.Errorf("observação inesperada: %s", o)
		}
	}
}
