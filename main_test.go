package main

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"rota-smart/internal/db"
	"rota-smart/internal/rota360"
	"rota-smart/internal/service"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return NewApp(database, service.NewPropostaService(
		func() time.Time { return time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC) },
		func() bool { return false },
	))
}

func exemploTicket() db.Ticket {
	return db.Ticket{
		NomeCliente: "Maria Souza",
		TipoTicket:  "Manutencao",
		ColunaAtual: "Fazer contato",
	}
}

func TestSaveTicketGetTicketsEMove(t *testing.T) {
	a := newTestApp(t)

	salvo, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	if salvo.ID == 0 || salvo.Posicao != 0 || salvo.ColunaAtual != "Fazer contato" {
		t.Fatalf("ticket novo inesperado: %+v", salvo)
	}

	outro, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket 2: %v", err)
	}
	if outro.Posicao != 1 {
		t.Fatalf("card novo deveria ir para o fim da coluna, posicao=%d", outro.Posicao)
	}

	tickets, err := a.GetTickets()
	if err != nil || len(tickets) != 2 {
		t.Fatalf("GetTickets: %v, n=%d", err, len(tickets))
	}

	if err := a.MoveTicket(salvo.ID, "Negociando", 0); err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}
	movido, err := a.tickets.Get(a.ctx, salvo.ID)
	if err != nil || movido.ColunaAtual != "Negociando" || movido.Posicao != 0 {
		t.Fatalf("card não moveu: %+v, err=%v", movido, err)
	}
}

func TestSaveTicketValidacoes(t *testing.T) {
	a := newTestApp(t)

	casos := []struct {
		nome string
		t    db.Ticket
	}{
		{"coluna inválida", db.Ticket{NomeCliente: "A", TipoTicket: "Outro", ColunaAtual: "Lixeira"}},
		{"tipo inválido", db.Ticket{NomeCliente: "A", TipoTicket: "Folga", ColunaAtual: "Fazer contato"}},
		{"nome longo", db.Ticket{NomeCliente: string(make([]byte, 121)), TipoTicket: "Outro", ColunaAtual: "Fazer contato"}},
	}
	for _, c := range casos {
		if _, err := a.SaveTicket(c.t); err == nil {
			t.Errorf("%s: esperava erro", c.nome)
		}
	}
}

func TestMoveTicketValidaColuna(t *testing.T) {
	a := newTestApp(t)
	salvo, _ := a.SaveTicket(exemploTicket())
	if err := a.MoveTicket(salvo.ID, "Nao existe", 0); err != service.ErrColunaInvalida {
		t.Fatalf("esperava ErrColunaInvalida, veio %v", err)
	}
	if err := a.MoveTicket(999, "Negociando", 0); err == nil {
		t.Fatal("ticket inexistente deveria dar erro")
	}
}

func TestSaveCobrancaRecalculaDerivados(t *testing.T) {
	a := newTestApp(t)
	tk, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}

	c, err := a.SaveCobranca(db.Cobranca{
		TicketID:         tk.ID,
		TotalAbertoCents: 58400,
		EntradaCents:     14000,
		NumeroVezes:      12,
		ParcelaBaseCents: 10400,
		// campos derivados falsos vindos do frontend devem ser sobrescritos
		ParceladoCents:     1,
		UltimaParcelaCents: 1,
		FinalCents:         1,
	})
	if err != nil {
		t.Fatalf("SaveCobranca: %v", err)
	}
	if c.ParceladoCents != 3700 || c.UltimaParcelaCents != 3700 || c.FinalCents != 14100 {
		t.Fatalf("recálculo errado: %+v", c)
	}
	if !c.Ativa || c.Status != "proposta" || c.VersaoRegra != service.VersaoRegra {
		t.Fatalf("metadados errados: %+v", c)
	}

	ativa, err := a.GetCobrancaAtiva(tk.ID)
	if err != nil || ativa == nil || ativa.ID != c.ID {
		t.Fatalf("GetCobrancaAtiva: %+v, err=%v", ativa, err)
	}

	hist, err := a.GetHistoricoCobrancas(tk.ID)
	if err != nil || len(hist) != 1 {
		t.Fatalf("GetHistoricoCobrancas: n=%d, err=%v", len(hist), err)
	}
}

func TestSaveCobrancaValidaEntrada(t *testing.T) {
	a := newTestApp(t)
	tk, _ := a.SaveTicket(exemploTicket())

	if _, err := a.SaveCobranca(db.Cobranca{TicketID: tk.ID, TotalAbertoCents: 0, NumeroVezes: 1}); err == nil {
		t.Fatal("total zero deveria falhar")
	}
	if _, err := a.SaveCobranca(db.Cobranca{TicketID: tk.ID, TotalAbertoCents: 100, EntradaCents: 200, NumeroVezes: 1}); err == nil {
		t.Fatal("entrada maior que o total deveria falhar")
	}
	if _, err := a.SaveCobranca(db.Cobranca{TicketID: 9999, TotalAbertoCents: 100, NumeroVezes: 1}); err == nil {
		t.Fatal("ticket inexistente deveria falhar")
	}
	if _, err := a.SaveCobranca(db.Cobranca{TicketID: tk.ID, TotalAbertoCents: 100, NumeroVezes: 1, DataVencimento: "31/12/2026"}); err == nil {
		t.Fatal("data inválida deveria falhar")
	}
}

func TestUpdateStatusCobranca(t *testing.T) {
	a := newTestApp(t)
	tk, _ := a.SaveTicket(exemploTicket())
	c, err := a.SaveCobranca(db.Cobranca{TicketID: tk.ID, TotalAbertoCents: 10000, NumeroVezes: 2})
	if err != nil {
		t.Fatalf("SaveCobranca: %v", err)
	}

	if err := a.UpdateStatusCobranca(c.ID, "aceita"); err != nil {
		t.Fatalf("UpdateStatusCobranca: %v", err)
	}
	if err := a.UpdateStatusCobranca(c.ID, "pago"); err != service.ErrStatusInvalido {
		t.Fatalf("esperava ErrStatusInvalido, veio %v", err)
	}
}

func TestCalculateDebtECasosDoPlano(t *testing.T) {
	a := newTestApp(t)

	res, err := a.CalculateDebt(service.ReparcelamentoInput{TotalAbertoCents: 58400, EntradaCents: 14000, NumeroVezes: 12, ParcelaBaseCents: 10400})
	if err != nil {
		t.Fatalf("CalculateDebt: %v", err)
	}
	if res.SaldoCents != 44400 || res.ParceladoCents != 3700 || res.FinalCents != 14100 {
		t.Fatalf("caso 584/140/12x: %+v", res)
	}

	// 100,00 em 3x -> 34 + 34 + 32 (parcela em real inteiro, última menor)
	res, err = a.CalculateDebt(service.ReparcelamentoInput{TotalAbertoCents: 10000, NumeroVezes: 3})
	if err != nil {
		t.Fatalf("CalculateDebt 3x: %v", err)
	}
	soma := res.ParceladoCents*2 + res.UltimaParcelaCents
	if res.ParceladoCents != 3400 || res.UltimaParcelaCents != 3200 || soma != 10000 {
		t.Fatalf("caso 100/3x: %+v", res)
	}

	if _, err := a.CalculateDebt(service.ReparcelamentoInput{}); err == nil {
		t.Fatal("total zero deveria falhar")
	}
}

func TestGenerateProposalTemplate(t *testing.T) {
	a := newTestApp(t)
	tk, _ := a.SaveTicket(exemploTicket())

	prop, err := a.GenerateProposal(tk.ID, service.ReparcelamentoInput{TotalAbertoCents: 58400, EntradaCents: 14000, NumeroVezes: 12, ParcelaBaseCents: 10400}, false)
	if err != nil {
		t.Fatalf("GenerateProposal: %v", err)
	}
	if prop.Origem != "template" {
		t.Fatalf("origem: %s", prop.Origem)
	}
	for _, trecho := range []string{"Bom dia, Maria!", "R$ 584,00", "R$ 140,00", "*12*x de R$ 37,00", "R$ 141,00"} {
		if !contains(prop.Texto, trecho) {
			t.Errorf("proposta sem %q:\n%s", trecho, prop.Texto)
		}
	}
	if a.HasAI() {
		t.Error("sem GROQ_API_KEY, HasAI deveria ser false")
	}
}

func TestGetColunas(t *testing.T) {
	a := newTestApp(t)
	cols := a.GetColunas()
	if len(cols) != 12 || cols[0] != "Fazer contato" || cols[2] != "Visita Agendada" || cols[4] != "Cobrança" {
		t.Fatalf("colunas: %v", cols)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestGerarRenegociacaoSemSessao(t *testing.T) {
	a := newTestApp(t)
	tk, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	if _, err := a.GerarRenegociacao(tk.ID); !errors.Is(err, rota360.ErrSemSessao) {
		t.Fatalf("esperava ErrSemSessao, veio %v", err)
	}
}

func TestCortesiasUsadas(t *testing.T) {
	casos := []struct {
		nome string
		obs  string
		want int
	}{
		{"sem menção", "AMPLIAÇÃO CÂMERA WIFI EM 28.01.2026\r\nSISTEMA INSTALADO EM 20.08.2025", 0},
		{"sem número usa as 3", "concedida cortesia conforme combinado - bruna", 3},
		{"duas cortesias", "liberado 2 cortesias no mes - vivian", 2},
		{"cortesia 1x", "CORTESIA 1X APROVADA - marcielly", 1},
		{"cortesia de 1 mes", "cortesia de 1 mes diluida em 3x - taiane", 1},
		{"maior entre linhas", "usou 1 cortesia\r\ndepois usou 3 cortesias", 3},
		{"data não vira número", "cortesia em 03/2026 cancelada", 3},
	}
	for _, c := range casos {
		if got := cortesiasUsadas(c.obs); got != c.want {
			t.Errorf("%s: cortesiasUsadas=%d, esperava %d", c.nome, got, c.want)
		}
	}
}

func TestAvaliarCortesia(t *testing.T) {
	agora := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	assin := agora.AddDate(0, -14, 0)

	if n, motivo := avaliarCortesia(agora, assin, ""); n != 3 || motivo == "" {
		t.Errorf("1 ano + nunca usou: restantes=%d motivo=%q", n, motivo)
	}
	if n, _ := avaliarCortesia(agora, assin, "usou 2 cortesias - bruna"); n != 1 {
		t.Errorf("usou 2: restantes=%d, esperava 1", n)
	}
	if n, _ := avaliarCortesia(agora, assin, "cortesia concedida"); n != 0 {
		t.Errorf("mencionada sem número: restantes=%d, esperava 0", n)
	}
	if n, _ := avaliarCortesia(agora, agora.AddDate(0, -6, 0), ""); n != 0 {
		t.Errorf("contrato novo: restantes=%d, esperava 0", n)
	}
	if n, _ := avaliarCortesia(agora, time.Time{}, ""); n != 0 {
		t.Errorf("sem data: restantes=%d, esperava 0", n)
	}
}

func TestGenerateProposalAVista(t *testing.T) {
	a := newTestApp(t)
	tk, _ := a.SaveTicket(exemploTicket())

	// entrada quita o total: vira oferta à vista, sem linha de parcelamento
	prop, err := a.GenerateProposal(tk.ID, service.ReparcelamentoInput{
		TotalAbertoCents: 50200, EntradaCents: 50200, NumeroVezes: 1, ParcelaBaseCents: 33500,
	}, false)
	if err != nil {
		t.Fatalf("GenerateProposal à vista: %v", err)
	}
	for _, trecho := range []string{"Pagamento à vista: R$ 502,00", "mensalidade volta ao normal de R$ 335,00"} {
		if !contains(prop.Texto, trecho) {
			t.Errorf("proposta à vista sem %q:\n%s", trecho, prop.Texto)
		}
	}
	if contains(prop.Texto, "Parcelamento") {
		t.Errorf("proposta à vista não deveria ter parcelamento:\n%s", prop.Texto)
	}
}
