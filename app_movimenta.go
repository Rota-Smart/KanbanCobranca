package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"rota-smart/internal/db"
	"rota-smart/internal/rota360"
	"rota-smart/internal/service"
)

// MovimentarTicket360 é a ação manual (sem IA, sem chat): dado um ticket do
// 360 — número ("12345"), uuid ou URL — e uma coluna do Kanban, busca o
// ticket e o cliente no 360 e coloca o card na coluna. Se o cliente já tem
// card, move o card existente em vez de duplicar; senão importa um novo.
// Usa exatamente as mesmas funções do import manual e do vínculo de ticket.
func (a *App) MovimentarTicket360(entrada, coluna string) (db.Ticket, error) {
	entrada = strings.TrimSpace(entrada)
	coluna = strings.TrimSpace(coluna)
	if !service.ColunaValida(coluna) {
		return db.Ticket{}, service.ErrColunaInvalida
	}
	uuid, numero := dividirEntradaTicket(entrada)
	if uuid == "" && numero == "" {
		return db.Ticket{}, fmt.Errorf("informe o nº do ticket, o uuid ou a URL do ticket no 360")
	}
	c := a.rotaAtual()
	if c == nil {
		return db.Ticket{}, rota360.ErrSemSessao
	}
	ctx, cancel := context.WithTimeout(a.ctx, 45*time.Second)
	defer cancel()

	if uuid == "" {
		item, err := a.resolverTicket360(ctx, numero)
		if err != nil {
			return db.Ticket{}, fmt.Errorf("ticket #%s não está nas filas em aberto/pendentes do 360", numero)
		}
		uuid = item.ID
	}
	det, err := c.DetalheTicket(ctx, uuid)
	if err != nil {
		return db.Ticket{}, friendly("MovimentarTicket360", err)
	}
	if det.CodService == "" {
		return db.Ticket{}, fmt.Errorf("o ticket #%s não tem código service no 360 — importe o cliente pela busca", det.Numero)
	}

	// Cliente já tem card? Move em vez de duplicar; senão, importa novo.
	existente, err := a.tickets.GetPorCodService(a.ctx, det.CodService)
	if err == nil && existente != nil && existente.ID > 0 {
		if existente.ColunaAtual != coluna {
			if err := a.MoveTicket(existente.ID, coluna, a.contagemColuna(coluna)); err != nil {
				return db.Ticket{}, friendly("MovimentarTicket360", err)
			}
		}
		t, _, err := a.vincularTicket360(existente.ID, uuid)
		if err != nil {
			return db.Ticket{}, friendly("MovimentarTicket360", err)
		}
		return t, nil
	}

	criado, err := a.ImportarCliente360(det.CodService, coluna)
	if err != nil {
		return db.Ticket{}, err
	}
	t, _, err := a.vincularTicket360(criado.ID, uuid)
	if err != nil {
		return criado, friendly("MovimentarTicket360", err)
	}
	return t, nil
}
