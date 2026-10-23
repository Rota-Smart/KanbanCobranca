package main

import "testing"

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
