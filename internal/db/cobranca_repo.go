package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type CobrancaRepo struct {
	db *sql.DB
}

func NewCobrancaRepo(db *sql.DB) *CobrancaRepo { return &CobrancaRepo{db: db} }

var ErrCobrancaNaoEncontrada = errors.New("cobrança não encontrada")

const cobrancaCols = `id, ticket_id, data_contrato, data_vencimento, total_aberto_cents, entrada_cents, numero_vezes, parcela_base_cents, parcelado_cents, ultima_parcela_cents, final_cents, status, ativa, versao_regra`

// SaveAtiva desativa a cobrança ativa anterior do ticket e insere a nova
// como ativa, em uma única transação. O índice único parcial uq_cobranca_ativa
// garante que nunca haja duas ativas para o mesmo ticket.
func (r *CobrancaRepo) SaveAtiva(ctx context.Context, c *Cobranca) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("iniciar transação: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`UPDATE cobrancas SET ativa = 0, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now')
		 WHERE ticket_id = ? AND ativa = 1`, c.TicketID); err != nil {
		return 0, fmt.Errorf("desativar cobrança anterior: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO cobrancas (ticket_id, data_contrato, data_vencimento, total_aberto_cents, entrada_cents, numero_vezes, parcela_base_cents, parcelado_cents, ultima_parcela_cents, final_cents, status, ativa, versao_regra)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`,
		c.TicketID, c.DataContrato, c.DataVencimento, c.TotalAbertoCents, c.EntradaCents,
		c.NumeroVezes, c.ParcelaBaseCents, c.ParceladoCents, c.UltimaParcelaCents,
		c.FinalCents, c.Status, c.VersaoRegra)
	if err != nil {
		return 0, fmt.Errorf("inserir cobrança: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("obter id da cobrança: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("confirmar transação: %w", err)
	}
	return id, nil
}

// GetAtiva devolve a cobrança ativa do ticket, ou nil se ainda não houver acordo.
func (r *CobrancaRepo) GetAtiva(ctx context.Context, ticketID int64) (*Cobranca, error) {
	var c Cobranca
	err := r.db.QueryRowContext(ctx,
		`SELECT `+cobrancaCols+` FROM cobrancas WHERE ticket_id = ? AND ativa = 1`, ticketID,
	).Scan(&c.ID, &c.TicketID, &c.DataContrato, &c.DataVencimento, &c.TotalAbertoCents,
		&c.EntradaCents, &c.NumeroVezes, &c.ParcelaBaseCents, &c.ParceladoCents,
		&c.UltimaParcelaCents, &c.FinalCents, &c.Status, &c.Ativa, &c.VersaoRegra)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buscar cobrança ativa do ticket %d: %w", ticketID, err)
	}
	return &c, nil
}

// History devolve o histórico de acordos do ticket, mais recente primeiro.
func (r *CobrancaRepo) History(ctx context.Context, ticketID int64) ([]Cobranca, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+cobrancaCols+` FROM cobrancas WHERE ticket_id = ? ORDER BY id DESC`, ticketID)
	if err != nil {
		return nil, fmt.Errorf("listar histórico do ticket %d: %w", ticketID, err)
	}
	defer rows.Close()
	out := []Cobranca{}
	for rows.Next() {
		var c Cobranca
		if err := rows.Scan(&c.ID, &c.TicketID, &c.DataContrato, &c.DataVencimento, &c.TotalAbertoCents,
			&c.EntradaCents, &c.NumeroVezes, &c.ParcelaBaseCents, &c.ParceladoCents,
			&c.UltimaParcelaCents, &c.FinalCents, &c.Status, &c.Ativa, &c.VersaoRegra); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateStatus altera o status do acordo (proposta, aceita, quebrada, cancelada).
func (r *CobrancaRepo) UpdateStatus(ctx context.Context, id int64, status string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE cobrancas SET status = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id = ?`,
		status, id)
	if err != nil {
		return fmt.Errorf("atualizar status da cobrança %d: %w", id, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrCobrancaNaoEncontrada
	}
	return nil
}

// UpdateTotal substitui o total em aberto da cobrança ativa e as parcelas
// derivadas já recalculadas (botão "atualizar valor salvo" após os juros
// rodarem no 360).
func (r *CobrancaRepo) UpdateTotal(ctx context.Context, id, totalCents, parcelado, ultima, final int64) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE cobrancas SET total_aberto_cents = ?, parcelado_cents = ?, ultima_parcela_cents = ?, final_cents = ?,
		 updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id = ?`,
		totalCents, parcelado, ultima, final, id)
	if err != nil {
		return fmt.Errorf("atualizar total da cobrança %d: %w", id, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrCobrancaNaoEncontrada
	}
	return nil
}
