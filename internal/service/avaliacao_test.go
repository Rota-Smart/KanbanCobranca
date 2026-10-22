package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rota-smart/internal/db"
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
						map[string]any{"summary": "Cobrança — X (1 título, 61 dias)", "status": map[string]any{"name": "Resolvido"}},
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
	if !avaliacao.Renegociou {
		t.Error("esperava renegociou=true")
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
	if !strings.Contains(avaliacao.ResumoUmaLinha(), "renegociou=true") {
		t.Errorf("resumo errado: %s", avaliacao.ResumoUmaLinha())
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
