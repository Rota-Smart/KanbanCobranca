package main

import (
	"testing"

	"rota-smart/internal/db"
	"rota-smart/internal/rota360"
	"rota-smart/internal/service"
)

func TestParseComandoVincular(t *testing.T) {
	casos := map[string]bool{
		"/vincular":        true,
		"/vincular.":       true,
		"/vincule":         true,
		"/vincula":         true,
		"  /vincular  ":    true,
		"/vincular agora":  true, // ignora o resto da mensagem
		"/move 1 para X":   false,
		"vincular tudo":    false, // sem a barra não é comando
		"":                 false,
	}
	for msg, quer := range casos {
		if got := service.ParseComandoVincular(msg); got != quer {
			t.Errorf("ParseComandoVincular(%q) = %v, queria %v", msg, got, quer)
		}
	}
}

func TestEscolherTicketParaCard(t *testing.T) {
	fila := []rota360.Ticket360{
		{ID: "a", Numero: "101", CodigoService: "999", Solicitante: "Outra Empresa", CriadoEm: "2026-10-20T10:00:00Z"},
		{ID: "b", Numero: "102", CodigoService: "24755", Solicitante: "João da Silva", CriadoEm: "2026-10-01T10:00:00Z"},
		{ID: "c", Numero: "103", CodigoService: "24755", Solicitante: "João da Silva", CriadoEm: "2026-10-15T10:00:00Z"},
		{ID: "d", Numero: "104", CodigoService: "", Solicitante: "Maria Souza Ltda", CriadoEm: "2026-10-10T10:00:00Z"},
	}

	// Código service vence, e dentro dele o mais recente.
	card := db.Ticket{NomeCliente: "João da Silva", CodService: "24755"}
	tk := escolherTicketParaCard(card, fila)
	if tk == nil || tk.ID != "c" {
		t.Fatalf("queria ticket c (cod 24755 mais recente), veio %+v", tk)
	}

	// Sem código no card: casa pelo nome contra o solicitante.
	card.CodService = ""
	tk = escolherTicketParaCard(card, fila)
	if tk == nil || tk.ID != "c" {
		t.Fatalf("sem código queria ticket c, veio %+v", tk)
	}

	// Nome de card contido no solicitante ("Maria Souza" ⊂ "Maria Souza Ltda").
	card = db.Ticket{NomeCliente: "Maria Souza", CodService: ""}
	tk = escolherTicketParaCard(card, fila)
	if tk == nil || tk.ID != "d" {
		t.Fatalf("queria ticket d por nome, veio %+v", tk)
	}

	// Nome curto demais não casa por contenção (falso positivo).
	card = db.Ticket{NomeCliente: "Cia", CodService: ""}
	if tk := escolherTicketParaCard(card, fila); tk != nil {
		t.Fatalf("nome curto não deveria casar, veio %+v", tk)
	}

	// Nada casa: nil.
	card = db.Ticket{NomeCliente: "Zezinho sem ticket", CodService: ""}
	if tk := escolherTicketParaCard(card, fila); tk != nil {
		t.Fatalf("não deveria casar nada, veio %+v", tk)
	}
}
