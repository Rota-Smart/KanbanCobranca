package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// TentativaContato é um registro de tentativa de contato com o cliente do
// card: data-hora automática (CriadoEm), canal opcional (ligação, WhatsApp,
// e-mail…) e nota livre. O contador do card vem da contagem de linhas.
type TentativaContato struct {
	ID        int64  `json:"id"`
	TicketID  int64  `json:"ticketId"`
	Canal     string `json:"canal"`
	Nota      string `json:"nota"`
	CreatedAt string `json:"createdAt"`
}

type TentativaRepo struct {
	db *sql.DB
}

func NewTentativaRepo(db *sql.DB) *TentativaRepo { return &TentativaRepo{db: db} }

var ErrTentativaNaoEncontrada = errors.New("tentativa não encontrada")

// Add grava uma tentativa de contato do card.
func (r *TentativaRepo) Add(ctx context.Context, ticketID int64, canal, nota string) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO tentativas_contato (ticket_id, canal, nota) VALUES (?, ?, ?)`,
		ticketID, canal, nota)
	if err != nil {
		return 0, fmt.Errorf("registrar tentativa de contato: %w", err)
	}
	return res.LastInsertId()
}

// Delete remove um registro de tentativa (desfazer marcação errada).
func (r *TentativaRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM tentativas_contato WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("apagar tentativa de contato: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrTentativaNaoEncontrada
	}
	return nil
}

// Get devolve uma tentativa pelo id (para descobrir o card antes de remover).
func (r *TentativaRepo) Get(ctx context.Context, id int64) (TentativaContato, error) {
	var t TentativaContato
	err := r.db.QueryRowContext(ctx,
		`SELECT id, ticket_id, canal, nota, created_at FROM tentativas_contato WHERE id = ?`, id).
		Scan(&t.ID, &t.TicketID, &t.Canal, &t.Nota, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return TentativaContato{}, ErrTentativaNaoEncontrada
	}
	if err != nil {
		return TentativaContato{}, fmt.Errorf("buscar tentativa de contato: %w", err)
	}
	return t, nil
}

// List devolve as tentativas do card, da mais recente para a mais antiga.
// Slice vazio (não nil): nil vira null na bridge do Wails e derruba o frontend.
func (r *TentativaRepo) List(ctx context.Context, ticketID int64) ([]TentativaContato, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, ticket_id, canal, nota, created_at FROM tentativas_contato
		 WHERE ticket_id = ? ORDER BY created_at DESC, id DESC`, ticketID)
	if err != nil {
		return nil, fmt.Errorf("listar tentativas de contato: %w", err)
	}
	defer rows.Close()
	out := []TentativaContato{}
	for rows.Next() {
		var t TentativaContato
		if err := rows.Scan(&t.ID, &t.TicketID, &t.Canal, &t.Nota, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
