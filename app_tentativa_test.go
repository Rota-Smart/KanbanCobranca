package main

import (
	"testing"
)

// Tentativas de contato: registrar incrementa o contador no card e guarda a
// data da última; remover decrementa; funciona em qualquer coluna.
func TestTentativasContato(t *testing.T) {
	a := newTestApp(t)
	tk, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}

	salvo, err := a.RegistrarTentativaContato(tk.ID, "ligação", "")
	if err != nil {
		t.Fatalf("RegistrarTentativaContato: %v", err)
	}
	if salvo.Tentativas != 1 || salvo.UltimaTentativa == "" {
		t.Fatalf("esperava 1 tentativa com data, veio %+v", salvo)
	}

	if _, err := a.RegistrarTentativaContato(tk.ID, "WhatsApp", "sem resposta"); err != nil {
		t.Fatalf("2ª tentativa: %v", err)
	}
	atual, err := ticketPorID(t, a, tk.ID)
	if err != nil {
		t.Fatalf("ticketPorID: %v", err)
	}
	if atual.Tentativas != 2 {
		t.Fatalf("esperava 2 tentativas no board, veio %d", atual.Tentativas)
	}

	lista, err := a.ListarTentativasContato(tk.ID)
	if err != nil {
		t.Fatalf("ListarTentativasContato: %v", err)
	}
	if len(lista) != 2 || lista[0].Canal != "WhatsApp" {
		t.Fatalf("esperava 2 registros (mais recente 1º), veio %+v", lista)
	}

	atual, err = a.RemoverTentativaContato(lista[0].ID)
	if err != nil {
		t.Fatalf("RemoverTentativaContato: %v", err)
	}
	if atual.Tentativas != 1 {
		t.Fatalf("esperava 1 tentativa após remover, veio %d", atual.Tentativas)
	}

	// id inexistente volta com erro claro
	if _, err := a.RemoverTentativaContato(99999); err == nil {
		t.Fatal("remover tentativa inexistente deveria falhar")
	}
}
