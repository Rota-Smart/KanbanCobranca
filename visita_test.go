package main

import (
	"errors"
	"strings"
	"testing"

	"rota-smart/internal/db"
	"rota-smart/internal/service"
)

// ticketPorID acha o card na lista do board (não há Get por ID no binding).
func ticketPorID(t *testing.T, a *App, id int64) (db.Ticket, error) {
	t.Helper()
	ts, err := a.GetTickets()
	if err != nil {
		return db.Ticket{}, err
	}
	for _, x := range ts {
		if x.ID == id {
			return x, nil
		}
	}
	return db.Ticket{}, db.ErrTicketNaoEncontrado
}

func TestSalvarVisita(t *testing.T) {
	a := newTestApp(t)
	tk, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}

	salvo, err := a.SalvarVisita(tk.ID, "2026-11-12", "Tarde", "14:30", "Rua das Flores, 123 - Centro")
	if err != nil {
		t.Fatalf("SalvarVisita: %v", err)
	}
	if salvo.VisitaData != "2026-11-12" || salvo.VisitaTurno != "Tarde" ||
		salvo.VisitaHora != "14:30" || salvo.VisitaEndereco != "Rua das Flores, 123 - Centro" {
		t.Fatalf("visita não gravada: %+v", salvo)
	}

	// limpar: tudo vazio zera o agendamento
	limpo, err := a.SalvarVisita(tk.ID, "", "", "", "")
	if err != nil {
		t.Fatalf("limpar visita: %v", err)
	}
	if limpo.VisitaData != "" || limpo.VisitaTurno != "" || limpo.VisitaHora != "" || limpo.VisitaEndereco != "" {
		t.Fatalf("visita deveria estar limpa: %+v", limpo)
	}
}

func TestSalvarVisitaValidacao(t *testing.T) {
	a := newTestApp(t)
	tk, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}

	casos := []struct {
		nome              string
		data, turno, hora string
		erroEsperado      error
	}{
		{"data fora do formato", "12/11/2026", "", "", service.ErrDataInvalida},
		{"hora fora do formato", "2026-11-12", "", "1430", service.ErrHoraInvalida},
		{"turno inexistente", "2026-11-12", "Madrugada", "", service.ErrTurnoInvalido},
		{"endereço longo", "2026-11-12", "", "", service.ErrTextoMuitoLongo},
	}
	for _, c := range casos {
		endereco := ""
		if c.nome == "endereço longo" {
			endereco = strings.Repeat("x", 300)
		}
		_, err := a.SalvarVisita(tk.ID, c.data, c.turno, c.hora, endereco)
		if !errors.Is(err, c.erroEsperado) {
			t.Errorf("%s: esperava %v, veio %v", c.nome, c.erroEsperado, err)
		}
	}

	if _, err := a.SalvarVisita(99999, "2026-11-12", "", "", ""); !errors.Is(err, db.ErrTicketNaoEncontrado) {
		t.Errorf("ticket inexistente: esperava ErrTicketNaoEncontrado, veio %v", err)
	}
}

func TestEnderecoSugeridoVisita(t *testing.T) {
	a := newTestApp(t)
	tk, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	// sem cadastro do 360 no banco de teste: cai no endereço do card
	endereco, err := a.EnderecoSugeridoVisita(tk.ID)
	if err != nil {
		t.Fatalf("EnderecoSugeridoVisita: %v", err)
	}
	if endereco != tk.Endereco {
		t.Fatalf("esperava %q, veio %q", tk.Endereco, endereco)
	}
}

func TestValidarVisitaTudoVazio(t *testing.T) {
	if err := service.ValidarVisita("", "", "", ""); err != nil {
		t.Fatalf("tudo vazio deveria ser válido (limpa), veio %v", err)
	}
}

func TestMoverParaVisitaRealizadaMarca(t *testing.T) {
	a := newTestApp(t)
	tk, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	if _, err := a.SalvarVisita(tk.ID, "2026-11-12", "Tarde", "14:30", "Rua das Flores, 123"); err != nil {
		t.Fatalf("SalvarVisita: %v", err)
	}

	// mover o card com visita agendada para a coluna "Visita Realizada"
	// marca a visita; mover de volta não limpa.
	if err := a.MoveTicket(tk.ID, "Visita Realizada", 0); err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}
	t1, err := ticketPorID(t, a, tk.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if t1.VisitaRealizada == "" {
		t.Fatalf("visita deveria estar marcada como realizada: %+v", t1)
	}
	marca := t1.VisitaRealizada
	if err := a.MoveTicket(tk.ID, "Cobrança", 0); err != nil {
		t.Fatalf("MoveTicket de volta: %v", err)
	}
	t2, _ := ticketPorID(t, a, tk.ID)
	if t2.VisitaRealizada != marca {
		t.Fatalf("marca não deveria mudar ao sair da coluna: %+v", t2)
	}

	// re-agendar limpa a marca
	if _, err := a.SalvarVisita(tk.ID, "2026-12-01", "Manhã", "", ""); err != nil {
		t.Fatalf("re-agendar: %v", err)
	}
	t3, _ := ticketPorID(t, a, tk.ID)
	if t3.VisitaRealizada != "" {
		t.Fatalf("re-agendar deveria limpar a marca: %+v", t3)
	}

	// card sem visita agendada: mover não marca nem falha
	tk2, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket 2: %v", err)
	}
	if err := a.MoveTicket(tk2.ID, "Visita Realizada", 0); err != nil {
		t.Fatalf("MoveTicket sem visita: %v", err)
	}
	t4, _ := ticketPorID(t, a, tk2.ID)
	if t4.VisitaRealizada != "" {
		t.Fatalf("card sem visita agendada não deveria ser marcado: %+v", t4)
	}
}

func TestMarcarVisitaRealizadaSemVisita(t *testing.T) {
	a := newTestApp(t)
	tk, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	if _, err := a.MarcarVisitaRealizada(tk.ID); err == nil ||
		!strings.Contains(err.Error(), "não tem visita agendada") {
		t.Fatalf("esperava erro de visita não agendada, veio %v", err)
	}
}

func TestParseComandoRealizada(t *testing.T) {
	casos := []struct {
		msg  string
		ok   bool
		card string
	}{
		{"/realizada", true, ""},
		{"/realizada João", true, "João"},
		{"/realizado João da Silva.", true, "João da Silva"},
		{"/move João para Cobrança", false, ""},
		{"visita realizada", false, ""},
	}
	for _, c := range casos {
		cmd, ok := service.ParseComandoRealizada(c.msg)
		if ok != c.ok {
			t.Errorf("%q: esperava ok=%v", c.msg, c.ok)
			continue
		}
		if ok && cmd.Card != c.card {
			t.Errorf("%q: esperava card %q, veio %q", c.msg, c.card, cmd.Card)
		}
	}
}
