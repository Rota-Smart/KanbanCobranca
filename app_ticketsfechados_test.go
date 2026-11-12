package main

import (
	"testing"

	"rota-smart/internal/db"
)

// TestTicketFechado: só status de conclusão do 360 conta como fechado.
func TestTicketFechado(t *testing.T) {
	casos := []struct {
		status string
		want   bool
	}{
		{"", false},
		{"Aberto", false},
		{"Em atendimento", false},
		{"Aguardando cliente", false},
		{"Resolvido", true},
		{"Fechado", true},
		{"Concluído", true},
		{"Cancelado", true},
		{"Encerrado", true},
		{"  fechado  ", true},
	}
	for _, c := range casos {
		if got, _ := ticketFechado(c.status); got != c.want {
			t.Errorf("ticketFechado(%q) = %v, esperava %v", c.status, got, c.want)
		}
	}
}

// TestCardsQueNaoMexer: a verificação de fechados não pode desfazer o que o
// usuário moveu à mão — card já na coluna Ticket Fechado e card que já passou
// por ela alguma vez (mesmo que tenha sido arrastado de volta) ficam de fora.
func TestCardsQueNaoMexer(t *testing.T) {
	ts := []db.Ticket{
		{ID: 1, ColunaAtual: "Fazer contato"},     // ticket aberto: pode mover
		{ID: 2, ColunaAtual: ColunaTicketFechado}, // já está na coluna: ignora
		{ID: 3, ColunaAtual: "Cobrança"},          // puxado de volta à mão: sem histórico, pode mover
		{ID: 5, ColunaAtual: "Aguardando Assinatura"}, // negociação viva: não fecha
		{ID: 6, ColunaAtual: "Aguardando Pagamento"},  // idem
	}
	hist := []db.MovimentoFechado{
		{CardID: 3}, // passou pela coluna antes — usuário puxou de volta, não mexe
		{CardID: 4},
	}
	naoMexer := cardsQueNaoMexer(ts, hist)

	if naoMexer[1] {
		t.Error("card 1 (ticket aberto) não deveria estar protegido")
	}
	if !naoMexer[2] {
		t.Error("card 2 já está na coluna Ticket Fechado e deveria ser ignorado")
	}
	if !naoMexer[3] {
		t.Error("card 3 já passou pela coluna (usuário puxou de volta) e deveria ser ignorado")
	}
	if !naoMexer[4] {
		t.Error("card 4 do histórico deveria ser ignorado mesmo sem estar no board")
	}
	if !naoMexer[5] {
		t.Error("card 5 em Aguardando Assinatura deveria ser protegido da verificação")
	}
	if !naoMexer[6] {
		t.Error("card 6 em Aguardando Pagamento deveria ser protegido da verificação")
	}
	// variação de caixa/espaço também protege
	extra := []db.Ticket{{ID: 7, ColunaAtual: "aguardando assinatura"}}
	if !cardsQueNaoMexer(extra, nil)[7] {
		t.Error("coluna digitada com caixa diferente deveria proteger igual")
	}
	// variações do nome da coluna (quadro dinâmico) protegem igual — o
	// casamento é por palavra-chave, não por nome exato
	variants := []db.Ticket{
		{ID: 8, ColunaAtual: "Aguardando assinatura do contrato"},
		{ID: 9, ColunaAtual: "  Aguardando Assinatura  "},
		{ID: 10, ColunaAtual: "AGUARDANDO PAGAMENTO"},
		{ID: 11, ColunaAtual: "Pagamento em análise"},
	}
	prot := cardsQueNaoMexer(variants, nil)
	for _, id := range []int64{8, 9, 10, 11} {
		if !prot[id] {
			t.Errorf("card %d em variação do nome deveria ser protegido", id)
		}
	}
	livre := []db.Ticket{
		{ID: 12, ColunaAtual: "Negociando"},
		{ID: 13, ColunaAtual: "Pagando"},
	}
	nao := cardsQueNaoMexer(livre, nil)
	if nao[12] || nao[13] {
		t.Error("colunas sem assinatura/pagamento pendente não deveriam ser protegidas")
	}
}

// TestVerificarFechadosSemSessao: sem sessão do 360 a verificação aborta
// antes de mexer em qualquer card.
func TestVerificarFechadosSemSessao(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.VerificarTicketsFechados(); err == nil {
		t.Fatal("sem sessão do 360 deveria dar erro")
	}
}
