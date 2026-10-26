package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Vínculos card ↔ tickets do 360: um card pode ter vários tickets (reclamação
// nova, renegociação, manutenção…). O vínculo "principal" continua nas
// colunas legadas do tickets (ticket360_id/numero) para não quebrar abrir/
// responder via agente; esta tabela guarda a lista completa com o assunto.

type VinculoRepo struct {
	db *sql.DB
}

func NewVinculoRepo(db *sql.DB) *VinculoRepo { return &VinculoRepo{db: db} }

var ErrVinculoNaoEncontrado = errors.New("vínculo não encontrado")

// List devolve os tickets 360 do card, do mais recente para o mais antigo.
func (r *VinculoRepo) List(ctx context.Context, ticketID int64) ([]Vinculo360, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, ticket_id, ticket360_id, ticket360_numero, assunto, criado_em
		 FROM tickets_vinculos WHERE ticket_id = ? ORDER BY id DESC`, ticketID)
	if err != nil {
		return nil, fmt.Errorf("listar vínculos: %w", err)
	}
	defer rows.Close()
	// slice vazio (não nil): nil vira null no JSON da bridge e o filtro do
	// board quebra com "vinculos is not iterable" no frontend.
	out := []Vinculo360{}
	for rows.Next() {
		var v Vinculo360
		if err := rows.Scan(&v.ID, &v.TicketID, &v.Ticket360ID, &v.Ticket360Numero, &v.Assunto, &v.CriadoEm); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListTodos devolve todos os vínculos do banco — alimenta o filtro de cards
// por assunto do ticket no board, sem uma consulta por card.
func (r *VinculoRepo) ListTodos(ctx context.Context) ([]Vinculo360, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, ticket_id, ticket360_id, ticket360_numero, assunto, criado_em
		 FROM tickets_vinculos`)
	if err != nil {
		return nil, fmt.Errorf("listar todos os vínculos: %w", err)
	}
	defer rows.Close()
	// slice vazio (não nil): nil vira null no JSON da bridge e o filtro do
	// board quebra com "vinculos is not iterable" no frontend.
	out := []Vinculo360{}
	for rows.Next() {
		var v Vinculo360
		if err := rows.Scan(&v.ID, &v.TicketID, &v.Ticket360ID, &v.Ticket360Numero, &v.Assunto, &v.CriadoEm); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Upsert registra (ou atualiza o assunto/número de) um vínculo do card.
// Re-vincular o mesmo ticket a outro card move o vínculo — o ticket fica em
// um card só.
func (r *VinculoRepo) Upsert(ctx context.Context, ticketID int64, uuid, numero, assunto string) error {
	if uuid == "" {
		return fmt.Errorf("uuid do ticket vazio")
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO tickets_vinculos (ticket_id, ticket360_id, ticket360_numero, assunto)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(ticket360_id) DO UPDATE SET
		   ticket_id = excluded.ticket_id,
		   ticket360_numero = excluded.ticket360_numero,
		   assunto = CASE WHEN excluded.assunto != '' THEN excluded.assunto ELSE tickets_vinculos.assunto END`,
		ticketID, uuid, numero, assunto)
	if err != nil {
		return fmt.Errorf("registrar vínculo 360: %w", err)
	}
	return nil
}

// Remove apaga um vínculo pelo id da tabela e devolve o que foi removido.
func (r *VinculoRepo) Remove(ctx context.Context, id int64) (*Vinculo360, error) {
	var v Vinculo360
	err := r.db.QueryRowContext(ctx,
		`SELECT id, ticket_id, ticket360_id, ticket360_numero, assunto, criado_em
		 FROM tickets_vinculos WHERE id = ?`, id,
	).Scan(&v.ID, &v.TicketID, &v.Ticket360ID, &v.Ticket360Numero, &v.Assunto, &v.CriadoEm)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrVinculoNaoEncontrado
	}
	if err != nil {
		return nil, fmt.Errorf("buscar vínculo %d: %w", id, err)
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM tickets_vinculos WHERE id = ?`, id); err != nil {
		return nil, fmt.Errorf("remover vínculo %d: %w", id, err)
	}
	return &v, nil
}
