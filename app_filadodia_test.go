package main

import (
	"testing"
	"time"
)

// Cenários da fila do dia com carimbos controlados (relativos a agora).
//
// Card A: criado há 10 dias em "Fazer contato", sem movimento -> PARADO
// Card B: movido há 1 dia -> não é parado (dias < 5)
// Card C: criado há 40 dias em "Ticket Fechado" -> fora (coluna finalizada)
func TestFilaDoDiaParados(t *testing.T) {
	a := newTestApp(t)

	haDias := func(d int) string {
		return time.Now().UTC().Add(-time.Duration(d) * 24 * time.Hour).Format(formatoCarimbo)
	}
	criar := func(cliente, coluna, criadoEm string) int64 {
		t.Helper()
		card, err := a.SaveTicket(exemploTicket())
		if err != nil {
			t.Fatalf("SaveTicket: %v", err)
		}
		if _, err := a.db.Exec(`UPDATE tickets SET nome_cliente = ?, coluna_atual = ?, created_at = ? WHERE id = ?`,
			cliente, coluna, criadoEm, card.ID); err != nil {
			t.Fatalf("ajustar ticket: %v", err)
		}
		return card.ID
	}

	criar("Card A", "Fazer contato", haDias(10))
	criar("Card B", "Em contato", haDias(10))
	idC := criar("Card C", "Ticket Fechado", haDias(40))
	// Card B teve movimento recente; Card A nenhum.
	if _, err := a.db.Exec(`INSERT INTO movimentacoes (ticket_id, de_coluna, para_coluna, created_at)
		VALUES ((SELECT id FROM tickets WHERE nome_cliente = 'Card B'), 'Fazer contato', 'Em contato', ?)`,
		haDias(1)); err != nil {
		t.Fatalf("movimentacao: %v", err)
	}

	fila, err := a.FilaDoDia(5)
	if err != nil {
		t.Fatalf("FilaDoDia: %v", err)
	}
	if len(fila.Parados) != 1 {
		t.Fatalf("esperava 1 card parado (Card A), veio %d: %+v", len(fila.Parados), fila.Parados)
	}
	if fila.Parados[0].Cliente != "Card A" || fila.Parados[0].Coluna != "Fazer contato" {
		t.Fatalf("parado errado: %+v", fila.Parados[0])
	}

	// Ticket Fechado nunca vira parado, nem com diasParado enorme.
	fila, err = a.FilaDoDia(100)
	if err != nil {
		t.Fatalf("FilaDoDia 100 dias: %v", err)
	}
	for _, p := range fila.Parados {
		if p.TicketID == idC {
			t.Fatalf("Ticket Fechado entrou na fila: %+v", p)
		}
	}

	// Configuração zerada cai no padrão (5): Card B (1 dia) continua fora.
	if fila.Parados != nil {
		for _, p := range fila.Parados {
			if p.Cliente == "Card B" {
				t.Fatalf("Card B movido há 1 dia não deveria aparecer: %+v", p)
			}
		}
	}
}

// Acordos: acende com <= 3 dias ou vencido; sem data_vencimento usa
// criação + 30 dias (estimativa comentada no binding). Concluído não acende.
func TestFilaDoDiaAcordos(t *testing.T) {
	a := newTestApp(t)

	haDias := func(d int) string {
		return time.Now().UTC().Add(-time.Duration(d) * 24 * time.Hour).Format(formatoCarimbo)
	}
	criar := func() int64 {
		t.Helper()
		card, err := a.SaveTicket(exemploTicket())
		if err != nil {
			t.Fatalf("SaveTicket: %v", err)
		}
		return card.ID
	}

	// vence em 2 dias (data_vencimento explícita)
	idVence := criar()
	em2 := time.Now().UTC().AddDate(0, 0, 2).Format("2006-01-02")
	if _, err := a.db.Exec(`INSERT INTO cobrancas (ticket_id, total_aberto_cents, entrada_cents, numero_vezes, parcela_base_cents, parcelado_cents, ultima_parcela_cents, final_cents, status, ativa, data_vencimento)
		VALUES (?, 100000, 0, 3, 34000, 100000, 32000, 102000, 'aceita', 1, ?)`, idVence, em2); err != nil {
		t.Fatalf("cobranca 1: %v", err)
	}

	// criado há 40 dias, sem data -> estimativa criação+30 = vencido há 10 dias
	idVelho := criar()
	if _, err := a.db.Exec(`INSERT INTO cobrancas (ticket_id, total_aberto_cents, entrada_cents, numero_vezes, parcela_base_cents, parcelado_cents, ultima_parcela_cents, final_cents, status, ativa, created_at, updated_at)
		VALUES (?, 100000, 0, 3, 34000, 100000, 32000, 102000, 'aceita', 1, ?, ?)`, idVelho, haDias(40), haDias(40)); err != nil {
		t.Fatalf("cobranca 2: %v", err)
	}

	// criado há 10 dias -> vence em ~20 dias, não acende
	idNovo := criar()
	if _, err := a.db.Exec(`INSERT INTO cobrancas (ticket_id, total_aberto_cents, entrada_cents, numero_vezes, parcela_base_cents, parcelado_cents, ultima_parcela_cents, final_cents, status, ativa, created_at, updated_at)
		VALUES (?, 100000, 0, 3, 34000, 100000, 32000, 102000, 'aceita', 1, ?, ?)`, idNovo, haDias(10), haDias(10)); err != nil {
		t.Fatalf("cobranca 3: %v", err)
	}

	// concluída vencendo hoje -> não acende
	idOk := criar()
	hoje := time.Now().UTC().Format("2006-01-02")
	if _, err := a.db.Exec(`INSERT INTO cobrancas (ticket_id, total_aberto_cents, entrada_cents, numero_vezes, parcela_base_cents, parcelado_cents, ultima_parcela_cents, final_cents, status, ativa, data_vencimento)
		VALUES (?, 100000, 0, 1, 100000, 100000, 100000, 100000, 'concluida', 0, ?)`, idOk, hoje); err != nil {
		t.Fatalf("cobranca 4: %v", err)
	}

	fila, err := a.FilaDoDia(5)
	if err != nil {
		t.Fatalf("FilaDoDia: %v", err)
	}
	if len(fila.Acordos) != 2 {
		t.Fatalf("esperava 2 acordos na fila, veio %d: %+v", len(fila.Acordos), fila.Acordos)
	}
	vistos := map[int64]bool{}
	for _, ac := range fila.Acordos {
		vistos[ac.TicketID] = true
	}
	if !vistos[idVence] || !vistos[idVelho] {
		t.Fatalf("faltou acordo: %+v (vence=%v velho=%v)", fila.Acordos, vistos[idVence], vistos[idVelho])
	}
}

// Orçamentos: enviado há > 3 dias em coluna ativa acende; recente, card que
// virou acordo ativo ou que está em coluna finalizada não acende.
func TestFilaDoDiaOrcamentos(t *testing.T) {
	a := newTestApp(t)

	haDias := func(d int) string {
		return time.Now().UTC().Add(-time.Duration(d) * 24 * time.Hour).Format(formatoCarimbo)
	}
	criar := func(coluna, geradoEm string) int64 {
		t.Helper()
		card, err := a.SaveTicket(exemploTicket())
		if err != nil {
			t.Fatalf("SaveTicket: %v", err)
		}
		if _, err := a.db.Exec(`UPDATE tickets SET coluna_atual = ?, orcamento_gerado_em = ? WHERE id = ?`,
			coluna, geradoEm, card.ID); err != nil {
			t.Fatalf("ajustar ticket: %v", err)
		}
		return card.ID
	}

	criar("Orçamento", haDias(6))  // acende
	criar("Orçamento", haDias(1))  // recente, fora
	idAcordo := criar("Negociando", haDias(9)) // virou acordo: fora
	if _, err := a.db.Exec(`INSERT INTO cobrancas (ticket_id, total_aberto_cents, entrada_cents, numero_vezes, parcela_base_cents, parcelado_cents, ultima_parcela_cents, final_cents, status, ativa)
		VALUES (?, 100000, 0, 2, 50000, 100000, 50000, 100000, 'aceita', 1)`, idAcordo); err != nil {
		t.Fatalf("cobranca: %v", err)
	}
	criar("Visita Realizada", haDias(9)) // coluna finalizada: fora

	fila, err := a.FilaDoDia(5)
	if err != nil {
		t.Fatalf("FilaDoDia: %v", err)
	}
	if len(fila.Orcamentos) != 1 {
		t.Fatalf("esperava 1 orçamento sem resposta, veio %d: %+v", len(fila.Orcamentos), fila.Orcamentos)
	}
	if fila.Orcamentos[0].Coluna != "Orçamento" || fila.Orcamentos[0].TicketID == idAcordo {
		t.Fatalf("orçamento errado: %+v", fila.Orcamentos[0])
	}
}

// Sem banco, responde erro claro (mesma mensagem dos outros bindings).
func TestFilaDoDiaSemBanco(t *testing.T) {
	a := &App{}
	if _, err := a.FilaDoDia(5); err == nil {
		t.Fatalf("deveria dar erro de banco indisponível")
	}
}
