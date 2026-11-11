package main

import (
	"strings"
	"testing"
	"time"
)

// TestGetMetricas: o painel 📊 conta cards por coluna, entradas e fechados da
// semana, visitas (total e de hoje), acordos ativos com valor e o tempo médio
// até o Ticket Fechado.
func TestGetMetricas(t *testing.T) {
	a := newTestApp(t)

	if _, err := a.colunas.Add(a.ctx, ColunaTicketFechado); err != nil {
		t.Fatalf("criar coluna: %v", err)
	}
	card, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	visita, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket visita: %v", err)
	}
	// A visita_data é gravada pelo fluxo do VisitaModal; aqui basta o registro.
	if _, err := a.db.Exec(`UPDATE tickets SET coluna_atual = ?, visita_data = ? WHERE id = ?`,
		ColunaVisitaAgendada, time.Now().Format("2006-01-02"), visita.ID); err != nil {
		t.Fatalf("marcar visita: %v", err)
	}
	// Acordo ativo de R$ 100,00.
	_, err = a.db.Exec(`INSERT INTO cobrancas (ticket_id, total_aberto_cents, entrada_cents, numero_vezes, parcela_base_cents, parcelado_cents, ultima_parcela_cents, final_cents, status, ativa)
		VALUES (?, 10000, 0, 1, 10000, 10000, 10000, 10000, 'proposta', 1)`, card.ID)
	if err != nil {
		t.Fatalf("cobranca: %v", err)
	}
	// Fechamento de hoje: movimentação para o Ticket Fechado.
	if err := a.MoveTicket(card.ID, ColunaTicketFechado, 0); err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}

	m, err := a.GetMetricas()
	if err != nil {
		t.Fatalf("GetMetricas: %v", err)
	}
	if m.TotalCards != 2 || m.EntradasSemana != 2 || m.FechadosSemana != 1 {
		t.Fatalf("contagens erradas: %+v", m)
	}
	if m.VisitasAgendadas != 1 || m.VisitasHoje != 1 {
		t.Fatalf("visitas erradas: %+v", m)
	}
	if m.CobrancasAtivas != 1 || m.ValorEmAbertoCents != 10000 {
		t.Fatalf("acordos errados: %+v", m)
	}
	if m.TempoMedioDias < 0 || m.TempoMedioDias > 1 {
		t.Fatalf("tempo médio deveria estar entre 0 e 1 dia, veio %v", m.TempoMedioDias)
	}
	achouFechado := false
	for _, c := range m.PorColuna {
		if c.Coluna == ColunaTicketFechado && c.Total == 1 {
			achouFechado = true
		}
	}
	if !achouFechado {
		t.Fatalf("porColuna deveria ter Ticket Fechado=1: %+v", m.PorColuna)
	}
}

// Sem banco, GetMetricas responde erro claro (mesma mensagem dos outros bindings).
func TestGetMetricasSemBanco(t *testing.T) {
	a := &App{}
	_, err := a.GetMetricas()
	if err == nil || !strings.Contains(err.Error(), "banco indisponível") {
		t.Fatalf("deveria dar erro de banco indisponível, veio %v", err)
	}
}
