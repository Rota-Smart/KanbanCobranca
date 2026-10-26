package main

import "testing"

// TestVinculoAutomaticoTela: o ticket capturado pelo Shift+F2 deve se vincular
// ao card novo do mesmo cliente (por código service) e ignorar clientes
// diferentes; sem captura, ninguém vincula.
func TestVinculoAutomaticoTela(t *testing.T) {
	a := newTestApp(t)

	// Sem captura prévia: card não ganha vínculo.
	card, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	if vs, _ := a.VinculosDoCard(card.ID); len(vs) != 0 {
		t.Fatalf("sem captura não deveria vincular, veio %d", len(vs))
	}

	// Shift+F2 capturou um ticket (uuid) do cliente 24755.
	a.guardarTicketTela(TicketTelaCapturado{
		UUID: "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d", Numero: "12345", CodService: "24755",
	})
	t.Cleanup(func() { a.guardarTicketTela(TicketTelaCapturado{}) })

	card2 := exemploTicket()
	card2.CodService = "24755"
	salvo, err := a.SaveTicket(card2)
	if err != nil {
		t.Fatalf("SaveTicket 2: %v", err)
	}
	vs, err := a.VinculosDoCard(salvo.ID)
	if err != nil {
		t.Fatalf("VinculosDoCard: %v", err)
	}
	if len(vs) != 1 || vs[0].Ticket360ID != "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d" {
		t.Fatalf("vínculo automático falhou: %+v", vs)
	}
	if salvo.Ticket360ID != vs[0].Ticket360ID {
		t.Fatalf("card deveria ter o vínculo como principal: %+v", salvo)
	}

	// Captura de outro cliente não deve vincular a um card diferente.
	card3 := exemploTicket()
	card3.CodService = "99999"
	salvo3, err := a.SaveTicket(card3)
	if err != nil {
		t.Fatalf("SaveTicket 3: %v", err)
	}
	if vs, _ := a.VinculosDoCard(salvo3.ID); len(vs) != 0 {
		t.Fatalf("código diferente não deveria vincular, veio %d", len(vs))
	}
}

func TestDividirEntradaTicket(t *testing.T) {
	casos := []struct {
		entrada, uuid, numero string
	}{
		{"https://360.rotagroup.com.br/helpdesk/tickets/abc-123-def?x=1", "abc-123-def", ""},
		{"360.rotagroup.com.br/helpdesk/tickets/abc-123-def", "abc-123-def", ""},
		{"a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d", "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d", ""},
		{"#12345", "", "12345"},
		{"12345", "", "12345"},
		{"  12345  ", "", "12345"},
		{"", "", ""},
	}
	for _, c := range casos {
		uuid, numero := dividirEntradaTicket(c.entrada)
		if uuid != c.uuid || numero != c.numero {
			t.Errorf("dividirEntradaTicket(%q) = (%q, %q); esperava (%q, %q)",
				c.entrada, uuid, numero, c.uuid, c.numero)
		}
	}
}
