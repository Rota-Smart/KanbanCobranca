package main

import (
	"strings"
	"testing"
	"time"
)

// TestGerarRelatorio: o texto traz período e os contadores do banco local
// (acordos concluídos, visitas realizadas, fechamentos, tentativas e novos
// cards) — mesmos critérios do painel de métricas.
func TestGerarRelatorio(t *testing.T) {
	a := newTestApp(t)

	if _, err := a.colunas.Add(a.ctx, ColunaTicketFechado); err != nil {
		t.Fatalf("criar coluna: %v", err)
	}
	hoje := time.Now().Format("2006-01-02")

	// Card novo hoje, com acordo e visita realizada hoje…
	card, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	_, err = a.db.Exec(`INSERT INTO cobrancas (ticket_id, total_aberto_cents, entrada_cents, numero_vezes, parcela_base_cents, parcelado_cents, ultima_parcela_cents, final_cents, status, ativa)
		VALUES (?, 10000, 0, 1, 10000, 10000, 10000, 10000, 'proposta', 1)`, card.ID)
	if err != nil {
		t.Fatalf("cobranca: %v", err)
	}
	if _, err := a.tentativas.Add(a.ctx, card.ID, "ligação", "sem resposta"); err != nil {
		t.Fatalf("tentativa: %v", err)
	}
	if err := a.MoveTicket(card.ID, ColunaTicketFechado, 0); err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}
	agora := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	if _, err := a.db.Exec(`UPDATE tickets SET visita_realizada_em = ? WHERE id = ?`, agora, card.ID); err != nil {
		t.Fatalf("marcar visita realizada: %v", err)
	}

	// …e um card antigo (fora do período de hoje) que não deve entrar.
	antigo, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket antigo: %v", err)
	}
	if _, err := a.db.Exec(`UPDATE tickets SET created_at = '2020-01-01T10:00:00Z' WHERE id = ?`, antigo.ID); err != nil {
		t.Fatalf("envelhecer card: %v", err)
	}

	txt, err := a.GerarRelatorio("", "") // vazio = últimos 7 dias
	if err != nil {
		t.Fatalf("GerarRelatorio: %v", err)
	}
	if !strings.Contains(txt, "Acordos fechados: 1") ||
		!strings.Contains(txt, "Visitas realizadas: 1") ||
		!strings.Contains(txt, "Tickets encerrados") ||
		!strings.Contains(txt, ": 1") ||
		!strings.Contains(txt, "Tentativas de contato: 1") ||
		!strings.Contains(txt, "Novos cards criados: 1") {
		t.Fatalf("texto inesperado:\n%s", txt)
	}
	if strings.Contains(txt, ": 2") {
		t.Fatalf("card antigo não deveria entrar em nenhum contador:\n%s", txt)
	}
	if !strings.Contains(txt, hoje) {
		t.Fatalf("período deveria terminar hoje (%s):\n%s", hoje, txt)
	}

	// Datas explícitas estreitas: só hoje.
	txt, err = a.GerarRelatorio(hoje, hoje)
	if err != nil {
		t.Fatalf("GerarRelatorio com datas: %v", err)
	}
	if !strings.Contains(txt, "Novos cards criados: 1") {
		t.Fatalf("esperava 1 card novo hoje:\n%s", txt)
	}

	// Data inválida responde erro claro.
	if _, err := a.GerarRelatorio("ontem", hoje); err == nil {
		t.Fatalf("data inválida deveria dar erro")
	}
}

// Sem banco, GerarRelatorio responde erro claro (mesma mensagem dos outros bindings).
func TestGerarRelatorioSemBanco(t *testing.T) {
	a := &App{}
	if _, err := a.GerarRelatorio("", ""); err == nil ||
		!strings.Contains(err.Error(), "banco indisponível") {
		t.Fatalf("deveria dar erro de banco indisponível, veio %v", err)
	}
}
