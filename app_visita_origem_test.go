package main

import (
	"testing"

	"rota-smart/internal/db"
)

// Cancelar a visita devolve o card pra coluna de origem (gravada quando ele
// entrou na "Visita Agendada") e limpa o agendamento — card cancelado não
// fica parado na coluna sem data.
func TestCancelarVisitaDevolvePraOrigem(t *testing.T) {
	a := newTestApp(t)

	salvo, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	if err := a.MoveTicket(salvo.ID, ColunaVisitaAgendada, 0); err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}

	t1, err := a.SalvarVisita(salvo.ID, "2026-11-13", "Tarde", "", "Rua A, 123")
	if err != nil {
		t.Fatalf("SalvarVisita: %v", err)
	}
	if t1.VisitaOrigem != "Fazer contato" {
		t.Fatalf("origem não gravou: %+v", t1)
	}

	t2, err := a.CancelarVisita(salvo.ID)
	if err != nil {
		t.Fatalf("CancelarVisita: %v", err)
	}
	if t2.ColunaAtual != "Fazer contato" {
		t.Fatalf("card deveria voltar pra origem, ficou em %q", t2.ColunaAtual)
	}
	if t2.VisitaData != "" || t2.VisitaOrigem != "" {
		t.Fatalf("agendamento/origem deveriam limpar: %+v", t2)
	}
}

// Mover o card pra fora da "Visita Agendada" limpa a origem; card criado
// direto na coluna (sem origem) cancela sem mover.
func TestMoveForaLimpaOrigemECancelaSemOrigem(t *testing.T) {
	a := newTestApp(t)

	salvo, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	if err := a.MoveTicket(salvo.ID, ColunaVisitaAgendada, 0); err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}
	if err := a.MoveTicket(salvo.ID, "Cobrança", 0); err != nil {
		t.Fatalf("MoveTicket 2: %v", err)
	}
	movido, err := a.tickets.Get(a.ctx, salvo.ID)
	if err != nil || movido.VisitaOrigem != "" {
		t.Fatalf("origem deveria limpar ao sair da coluna: %+v, err=%v", movido, err)
	}

	novo, err := a.SaveTicket(db.Ticket{NomeCliente: "José Lima", TipoTicket: "Manutencao", ColunaAtual: ColunaVisitaAgendada})
	if err != nil {
		t.Fatalf("SaveTicket 2: %v", err)
	}
	t3, err := a.CancelarVisita(novo.ID)
	if err != nil {
		t.Fatalf("CancelarVisita 2: %v", err)
	}
	if t3.ColunaAtual != ColunaVisitaAgendada {
		t.Fatalf("sem origem, card deveria ficar na coluna, foi pra %q", t3.ColunaAtual)
	}
}
