package main

import (
	"fmt"
	"net/url"
	"strings"

	"rota-smart/internal/db"
	"rota-smart/internal/service"
)

// Agendamento de visita do card: mover o card para a coluna "Visita Agendada"
// (ou pelo menu ⋯ do card) abre o diálogo que grava data/turno/horário e o
// endereço — sugerido do cadastro do cliente no 360, editável — com link do
// Google Maps.

// ColunaVisitaAgendada dispara o diálogo de agendamento ao soltar o card nela.
const ColunaVisitaAgendada = "Visita Agendada"

// SalvarVisita grava o agendamento de visita do card e devolve o card atualizado.
func (a *App) SalvarVisita(ticketID int64, data, turno, hora, endereco string) (db.Ticket, error) {
	data = strings.TrimSpace(data)
	turno = strings.TrimSpace(turno)
	hora = strings.TrimSpace(hora)
	endereco = strings.TrimSpace(endereco)
	if err := service.ValidarVisita(data, turno, hora, endereco); err != nil {
		return db.Ticket{}, friendly("SalvarVisita", err)
	}
	if err := a.tickets.SetVisita(a.ctx, ticketID, data, turno, hora, endereco); err != nil {
		return db.Ticket{}, friendly("SalvarVisita", err)
	}
	t, err := a.tickets.Get(a.ctx, ticketID)
	if err != nil {
		return db.Ticket{}, friendly("SalvarVisita", err)
	}
	return *t, nil
}

// EnderecoSugeridoVisita devolve o endereço para pré-preencher o diálogo:
// cadastro do cliente no 360 (cache local); sem cadastro, o endereço do card.
func (a *App) EnderecoSugeridoVisita(ticketID int64) (string, error) {
	t, err := a.tickets.Get(a.ctx, ticketID)
	if err != nil {
		return "", friendly("EnderecoSugeridoVisita", err)
	}
	if t.CodService != "" && a.clientes != nil {
		if c, err := a.clientes.Get(a.ctx, t.CodService); err == nil && c != nil &&
			strings.TrimSpace(c.Endereco) != "" {
			return strings.TrimSpace(c.Endereco), nil
		}
	}
	return strings.TrimSpace(t.Endereco), nil
}

// AbrirNoMaps abre o endereço no Google Maps (navegador padrão).
func (a *App) AbrirNoMaps(endereco string) error {
	endereco = strings.TrimSpace(endereco)
	if endereco == "" {
		return fmt.Errorf("endereço vazio — informe o endereço da visita")
	}
	link := "https://www.google.com/maps/search/?api=1&query=" + url.QueryEscape(endereco)
	return abrirNoNavegador(link)
}
