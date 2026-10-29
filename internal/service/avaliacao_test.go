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

func avaliadorDeTeste(t *testing.T) (*Avaliador, *db.Cliente360Repo, *db.CobrancaRepo, *db.TicketRepo) {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "teste.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	clientes := db.NewCliente360Repo(database)
	cobrancas := db.NewCobrancaRepo(database)
	tickets := db.NewTicketRepo(database)
	return NovoAvaliador(clientes, cobrancas, func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) }), clientes, cobrancas, tickets
}

func TestAvaliarClienteAntigo(t *testing.T) {
	av, clientes, _, _ := avaliadorDeTeste(t)
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
	av, clientes, _, _ := avaliadorDeTeste(t)
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
			// Caso real do 14535: um ÚNICO boleto futuro divergente é
			// variação da mensalidade (desconto/proporcional) — o
			// dashboard360 não manda categoria, então o padrão decide.
			nome: "único boleto futuro divergente é variação da mensalidade",
			faturas: []rota360.Fatura360{
				{Vencimento: d(time.November, 10), ValorDuplicata: 57.6},
			},
			mensalidade: 10000,
			contem:      "",
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
		{
			// Caso real do cliente 27257: mensalidade real é 99, mas o
			// valorTotal de algum produto diverge (120 aqui); o único boleto
			// futuro é 99 = mensalidade; o proporcional da contratação é
			// único e antigo. Não é renegociação.
			nome: "mensalidade deduzida dos boletos: futuro igual não marca mesmo com produto divergente",
			faturas: []rota360.Fatura360{
				{Vencimento: d(time.March, 5), ValorDuplicata: 62.1},  // proporcional da contratação (único)
				{Vencimento: d(time.August, 5), ValorDuplicata: 99},
				{Vencimento: d(time.September, 5), ValorDuplicata: 99},
				{Vencimento: d(time.October, 5), ValorDuplicata: 99},
				{Vencimento: d(time.November, 10), ValorDuplicata: 99}, // futuro = mensalidade real
			},
			mensalidade: 12000, // produto com valorTotal divergente
			contem:      "",
		},
		{
			// Parcelamento em curso (caso real do 24044): vários boletos
			// futuros e um divergente da referência — marca.
			nome: "futuros divergentes com parcelamento marcam mesmo sem mensalidade",
			faturas: []rota360.Fatura360{
				{Vencimento: d(time.August, 5), ValorDuplicata: 99},
				{Vencimento: d(time.September, 5), ValorDuplicata: 99},
				{Vencimento: d(time.November, 10), ValorDuplicata: 49.5},
				{Vencimento: d(time.December, 10), ValorDuplicata: 49.5},
			},
			mensalidade: 9900,
			contem:      "futuro",
		},
		{
			// Um único futuro divergente sem parcelamento atrás é variação
			// da mensalidade, não renegociação.
			nome: "futuro único diferente do valor recorrente não marca",
			faturas: []rota360.Fatura360{
				{Vencimento: d(time.September, 5), ValorDuplicata: 99},
				{Vencimento: d(time.November, 10), ValorDuplicata: 49.5},
			},
			mensalidade: 9900,
			contem:      "",
		},
		{
			// Selo Mensalidade: mesmo variando o preço, é variação da
			// mensalidade — não marca renegociação (nem pela regra do futuro,
			// nem pela dos dois valores no mês).
			nome: "parcela com selo Mensalidade e valor diferente não é renegociação",
			faturas: []rota360.Fatura360{
				{Vencimento: d(time.September, 5), ValorDuplicata: 99, Categoria: "Mensalidade"},
				{Vencimento: d(time.November, 10), ValorDuplicata: 137.4, Categoria: "Mensalidade"}, // futuro variado
			},
			mensalidade: 9900,
			contem:      "",
		},
		{
			// O selo Mensalidade vira a referência: mensalidade real 89,8
			// (selo); renegociação futura de agrupado com parcelamento
			// (≥2 boletos futuros) e valor diferente marca.
			nome: "referência vem do selo Mensalidade e agrupados divergentes marcam",
			faturas: []rota360.Fatura360{
				{Vencimento: d(time.August, 5), ValorDuplicata: 89.8, Categoria: "Mensalidade"},
				{Vencimento: d(time.September, 5), ValorDuplicata: 89.8, Categoria: "mensalidade"},
				{Vencimento: d(time.November, 10), ValorDuplicata: 135, Categoria: "Boleto Agrupado"},
				{Vencimento: d(time.December, 10), ValorDuplicata: 135, Categoria: "Boleto Agrupado"},
			},
			mensalidade: 12000, // produto divergente; selo manda
			contem:      "futuro",
		},
		{
			// Duas mensalidades em atraso no mesmo mês = variação da mensalidade
			// atrasada, não renegociação. Com o agrupado junto, marca.
			nome: "duas mensalidades em atraso no mesmo mês não marcam; agrupado marca",
			faturas: []rota360.Fatura360{
				{Vencimento: d(time.August, 5), ValorDuplicata: 99, Categoria: "Mensalidade"},
				{Vencimento: d(time.August, 20), ValorDuplicata: 137.4, Categoria: "Mensalidade"},
			},
			contem: "",
		},
		{
			nome: "mensalidade em atraso + agrupado em atraso no mesmo mês marcam",
			faturas: []rota360.Fatura360{
				{Vencimento: d(time.August, 5), ValorDuplicata: 99, Categoria: "Mensalidade"},
				{Vencimento: d(time.August, 20), ValorDuplicata: 135, Categoria: "Boleto agrupado"},
			},
			contem: "atraso",
		},
		{
			// Categoria fora do dicionário conhecido (ex. avulso) NÃO é
			// mensalidade: com parcelamento em curso (≥2 futuros), marca.
			nome: "categoria avulso não é mensalidade e futuros diferentes marcam",
			faturas: []rota360.Fatura360{
				{Vencimento: d(time.September, 5), ValorDuplicata: 99, Categoria: "Mensalidade"},
				{Vencimento: d(time.November, 10), ValorDuplicata: 57.6, Categoria: "avulso"},
				{Vencimento: d(time.December, 10), ValorDuplicata: 57.6, Categoria: "avulso"},
			},
			contem: "futuro",
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
	av, clientes, _, _ := avaliadorDeTeste(t)
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

// O acordo padrão que o PrepararCobranca grava (entrada 0) não é renegociação;
// só acordo com entrada conta. Era o falso positivo do cliente 27257.
func TestAvaliarAcordoLocalNaoRenegociou(t *testing.T) {
	av, clientes, cobrancas, tickets := avaliadorDeTeste(t)
	ctx := context.Background()

	if err := clientes.Upsert(ctx, &db.Cliente360{CodService: "27257", Nome: "W"}); err != nil {
		t.Fatal(err)
	}
	// Ticket necessário pela FK de cobrancas.
	ticketID, err := tickets.Save(ctx, &db.Ticket{NomeCliente: "W", CodService: "27257", ColunaAtual: "Cobrança"})
	if err != nil {
		t.Fatal(err)
	}
	// Acordo padrão do PrepararCobranca: entrada 0, 12x.
	if _, err := cobrancas.SaveAtiva(ctx, &db.Cobranca{
		TicketID: ticketID, TotalAbertoCents: 100000, EntradaCents: 0, NumeroVezes: 12,
		Status: "proposta", VersaoRegra: 1,
	}); err != nil {
		t.Fatal(err)
	}
	avaliacao, err := av.Avaliar(ctx, ticketID, "27257")
	if err != nil {
		t.Fatal(err)
	}
	if avaliacao.Renegociou {
		t.Errorf("acordo padrão (entrada 0) não deveria marcar renegociou: %+v", avaliacao)
	}

	// Acordo salvo no app NUNCA marca renegociação — nem com entrada:
	// renegociação vem só dos dados do 360 (regra do usuário, v1.21.3).
	if _, err := cobrancas.SaveAtiva(ctx, &db.Cobranca{
		TicketID: ticketID, TotalAbertoCents: 100000, EntradaCents: 40000, NumeroVezes: 12,
		Status: "aceita", VersaoRegra: 1,
	}); err != nil {
		t.Fatal(err)
	}
	avaliacao, err = av.Avaliar(ctx, ticketID, "27257")
	if err != nil {
		t.Fatal(err)
	}
	if avaliacao.Renegociou || avaliacao.QtdRenegociacoes != 0 {
		t.Errorf("acordo salvo no app não deveria marcar renegociou: %+v", avaliacao)
	}
}

func TestAvaliarClienteSemCache(t *testing.T) {
	av, _, _, _ := avaliadorDeTeste(t)
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
