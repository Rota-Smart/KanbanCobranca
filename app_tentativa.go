package main

import (
	"rota-smart/internal/db"
)

// Tentativas de contato do card: contam em qualquer coluna do board — cada
// registro guarda data-hora automática, canal opcional (ligação, WhatsApp,
// e-mail…) e nota livre. O assistente usa a tool registrar_tentativa_contato,
// que cai aqui.

// RegistrarTentativaContato grava uma tentativa e devolve o card atualizado
// (contador e data da última tentativa).
func (a *App) RegistrarTentativaContato(ticketID int64, canal, nota string) (db.Ticket, error) {
	if _, err := a.tickets.Get(a.ctx, ticketID); err != nil {
		return db.Ticket{}, friendly("RegistrarTentativaContato", err)
	}
	if _, err := a.tentativas.Add(a.ctx, ticketID, canal, nota); err != nil {
		return db.Ticket{}, friendly("RegistrarTentativaContato", err)
	}
	t, err := a.tickets.Get(a.ctx, ticketID)
	if err != nil {
		return db.Ticket{}, friendly("RegistrarTentativaContato", err)
	}
	return *t, nil
}

// ListarTentativasContato devolve as tentativas do card, da mais recente
// para a mais antiga.
func (a *App) ListarTentativasContato(ticketID int64) ([]db.TentativaContato, error) {
	if _, err := a.tickets.Get(a.ctx, ticketID); err != nil {
		return nil, friendly("ListarTentativasContato", err)
	}
	out, err := a.tentativas.List(a.ctx, ticketID)
	if err != nil {
		return nil, friendly("ListarTentativasContato", err)
	}
	return out, nil
}

// RemoverTentativaContato apaga um registro de tentativa (desfazer marcação
// errada) e devolve o card atualizado.
func (a *App) RemoverTentativaContato(tentativaID int64) (db.Ticket, error) {
	reg, err := a.tentativas.Get(a.ctx, tentativaID)
	if err != nil {
		return db.Ticket{}, friendly("RemoverTentativaContato", err)
	}
	if err := a.tentativas.Delete(a.ctx, tentativaID); err != nil {
		return db.Ticket{}, friendly("RemoverTentativaContato", err)
	}
	t, err := a.tickets.Get(a.ctx, reg.TicketID)
	if err != nil {
		return db.Ticket{}, friendly("RemoverTentativaContato", err)
	}
	return *t, nil
}
