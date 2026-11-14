package main

import (
	"strings"
	"testing"
)

// TestLinhaDoTempo: a linha do tempo junta criação, movimentação, tentativa
// e acordo do card em ordem cronológica (mais recente primeiro), com texto
// legível por tipo. Somente leitura — nada é gravado aqui.
func TestLinhaDoTempo(t *testing.T) {
	a := newTestApp(t)

	if _, err := a.colunas.Add(a.ctx, "Em contato"); err != nil {
		t.Fatalf("criar coluna: %v", err)
	}
	card, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	// Move o card (gera movimentação), registra tentativa e cria acordo.
	if err := a.MoveTicket(card.ID, "Em contato", 0); err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}
	if _, err := a.tentativas.Add(a.ctx, card.ID, "", "sem resposta"); err != nil {
		t.Fatalf("tentativa: %v", err)
	}
	_, err = a.db.Exec(`INSERT INTO cobrancas (ticket_id, total_aberto_cents, entrada_cents, numero_vezes, parcela_base_cents, parcelado_cents, ultima_parcela_cents, final_cents, status, ativa)
		VALUES (?, 120000, 20000, 3, 35000, 105000, 30000, 125000, 'proposta', 1)`, card.ID)
	if err != nil {
		t.Fatalf("cobranca: %v", err)
	}

	ev, err := a.LinhaDoTempo(card.ID)
	if err != nil {
		t.Fatalf("LinhaDoTempo: %v", err)
	}
	if len(ev) != 4 {
		t.Fatalf("esperava 4 eventos (criação, acordo, tentativa, movimento), veio %d: %+v", len(ev), ev)
	}
	// Conteúdo por tipo (a ordem entre eventos do mesmo segundo é de inserção).
	porTipo := map[string]string{}
	for _, e := range ev {
		porTipo[e.Tipo] = e.Texto
	}
	if !strings.Contains(porTipo["acordo"], "Acordo proposta") ||
		!strings.Contains(porTipo["acordo"], "entrada R$ 200,00") ||
		!strings.Contains(porTipo["acordo"], "3x de R$ 350,00") {
		t.Fatalf("evento de acordo inesperado: %+v", porTipo["acordo"])
	}
	if porTipo["tentativa"] != "Tentativa de contato (sem resposta)" {
		t.Fatalf("evento de tentativa inesperado: %+v", porTipo["tentativa"])
	}
	if !strings.Contains(porTipo["movimento"], "Card movido de Fazer contato para Em contato") {
		t.Fatalf("evento de movimento inesperado: %+v", porTipo["movimento"])
	}
	if !strings.Contains(porTipo["criacao"], "Card criado — Maria Souza") {
		t.Fatalf("evento de criação inesperado: %+v", porTipo["criacao"])
	}

	// Observação preenchida entra como evento com o updated_at do card.
	if _, err := a.db.Exec(`UPDATE tickets SET observacoes = 'Cliente pediu ligar depois das 18h' WHERE id = ?`, card.ID); err != nil {
		t.Fatalf("observacoes: %v", err)
	}
	ev, err = a.LinhaDoTempo(card.ID)
	if err != nil {
		t.Fatalf("LinhaDoTempo após observação: %v", err)
	}
	achou := false
	for _, e := range ev {
		if e.Tipo == "observacao" && strings.Contains(e.Texto, "ligar depois das 18h") {
			achou = true
		}
	}
	if !achou {
		t.Fatalf("observação deveria aparecer na linha do tempo: %+v", ev)
	}

	// Ordenação: com datas artificiais distintas, vem do mais novo pro mais velho.
	if _, err := a.db.Exec(`UPDATE movimentacoes SET created_at = '2026-11-14T15:02:00Z' WHERE ticket_id = ?`, card.ID); err != nil {
		t.Fatalf("datar movimentação: %v", err)
	}
	if _, err := a.db.Exec(`UPDATE tickets SET created_at = '2026-11-08T09:00:00Z' WHERE id = ?`, card.ID); err != nil {
		t.Fatalf("datar criação: %v", err)
	}
	ev, err = a.LinhaDoTempo(card.ID)
	if err != nil {
		t.Fatalf("LinhaDoTempo ordenado: %v", err)
	}
	if ev[len(ev)-1].Tipo != "criacao" {
		t.Fatalf("criação (mais antiga) deveria ser o último evento: %+v", ev)
	}
	for i := 1; i < len(ev); i++ {
		if ev[i-1].Data < ev[i].Data {
			t.Fatalf("fora de ordem em %d: %q < %q", i, ev[i-1].Data, ev[i].Data)
		}
	}
}

// Card inexistente responde erro; sem banco, erro claro (padrão dos bindings).
func TestLinhaDoTempoErros(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.LinhaDoTempo(99999); err == nil || !strings.Contains(err.Error(), "não encontrado") {
		t.Fatalf("card inexistente deveria dar erro, veio %v", err)
	}
	vazia := &App{}
	if _, err := vazia.LinhaDoTempo(1); err == nil || !strings.Contains(err.Error(), "banco indisponível") {
		t.Fatalf("deveria dar erro de banco indisponível, veio %v", err)
	}
}

// Formatação de valores em reais e textos de tentativa.
func TestReaisETextoTentativa(t *testing.T) {
	if got := reais(123456); got != "R$ 1.234,56" {
		t.Fatalf("reais(123456) = %q", got)
	}
	if got := reais(50); got != "R$ 0,50" {
		t.Fatalf("reais(50) = %q", got)
	}
	if got := textoTentativa("ligação", ""); got != "Tentativa de contato via ligação" {
		t.Fatalf("textoTentativa canal = %q", got)
	}
	if got := textoTentativa("", "sem resposta"); got != "Tentativa de contato (sem resposta)" {
		t.Fatalf("textoTentativa nota = %q", got)
	}
	if got := textoTentativa("WhatsApp", "falou com a sócia"); got != "Tentativa de contato via WhatsApp — falou com a sócia" {
		t.Fatalf("textoTentativa canal+nota = %q", got)
	}
}
