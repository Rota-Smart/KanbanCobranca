package main

import (
	"strings"
	"testing"

	"rota-smart/internal/rota360"
)

func codPtr(v int64) *int64 { return &v }

// TestCrmWatchGarantirCard: transição "Não atendido" → "Tentativa de contato"
// cria o card do cliente na coluna do vigia; na segunda vez não duplica, e
// card em outra coluna é movido.
func TestCrmWatchGarantirCard(t *testing.T) {
	a := newTestApp(t)

	neg := rota360.NegociacaoCRM{
		ID:          "neg-1",
		EtapaID:     "etapa-tentativa",
		EtapaNome:   "Tentativa de contato",
		Titulo:      "Ticket #123 · Outro",
		ClienteNome: "23808 - CLIENTE DE TESTE",
		CodCliente:  codPtr(23808),
	}

	msg, err := a.crmWatchGarantirCard(neg)
	if err != nil {
		t.Fatalf("garantir card: %v", err)
	}
	if !strings.Contains(msg, "criado") {
		t.Fatalf("esperava criação, veio %q", msg)
	}
	card, err := a.tickets.GetPorCodService(a.ctx, "23808")
	if err != nil || card == nil {
		t.Fatalf("card não criado: %+v err=%v", card, err)
	}
	if card.ColunaAtual != "Fazer contato" {
		t.Fatalf("card deveria estar em Fazer contato, veio %q", card.ColunaAtual)
	}

	// de novo: card já na coluna certa — nada a fazer
	msg, err = a.crmWatchGarantirCard(neg)
	if err != nil || msg != "" {
		t.Fatalf("segunda chamada deveria ser no-op, veio %q err=%v", msg, err)
	}

	// card movido para outra coluna: vigia traz de volta
	if _, err := a.colunas.Add(a.ctx, "Em contato"); err != nil {
		t.Fatalf("criar coluna Em contato: %v", err)
	}
	if err := a.MoveTicket(card.ID, "Em contato", 0); err != nil {
		t.Fatalf("mover para Em contato: %v", err)
	}
	msg, err = a.crmWatchGarantirCard(neg)
	if err != nil || !strings.Contains(msg, "movido") {
		t.Fatalf("esperava movimento, veio %q err=%v", msg, err)
	}
	card, _ = a.tickets.GetPorCodService(a.ctx, "23808")
	if card.ColunaAtual != "Fazer contato" {
		t.Fatalf("card deveria voltar para Fazer contato, veio %q", card.ColunaAtual)
	}
}

func TestCrmEtapaChave(t *testing.T) {
	if crmEtapaChave("  Não   atendido ") != "nao atendido" {
		t.Fatalf("não atendido: %q", crmEtapaChave("  Não   atendido "))
	}
	if crmEtapaChave("TENTATIVA de Contato") != "tentativa de contato" {
		t.Fatalf("tentativa: %q", crmEtapaChave("TENTATIVA de Contato"))
	}
}
