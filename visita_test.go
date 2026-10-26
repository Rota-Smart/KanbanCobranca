package main

import (
	"errors"
	"strings"
	"testing"

	"rota-smart/internal/db"
	"rota-smart/internal/service"
)

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
