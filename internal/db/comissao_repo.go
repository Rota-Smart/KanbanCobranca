package db

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Percentuais do comissionamento da coluna Upgrade: 5% do valor do serviço
// e 75% do valor da parcela do upgrade.
const (
	ComissaoServicoPct = 5
	ComissaoParcelaPct = 75
)

// ComissaoDe calcula a comissão em centavos: 5% do serviço + 75% da parcela.
func ComissaoDe(servicoCents, parcelaCents int64) int64 {
	return servicoCents*ComissaoServicoPct/100 + parcelaCents*ComissaoParcelaPct/100
}

// ComissaoRepo grava o comissionamento dos cards da coluna Upgrade.
type ComissaoRepo struct{ db *sql.DB }

func NewComissaoRepo(db *sql.DB) *ComissaoRepo { return &ComissaoRepo{db: db} }

// scanner cobre *sql.Row e *sql.Rows no mesmo scan.
type scanner interface{ Scan(dest ...any) error }

const colunasComissao = `id, ticket_id, valor_servico_cents, valor_parcela_cents, comissao_cents,
    observacoes, data_registro, recebido, recebido_em, created_at, updated_at`

func scanComissao(s scanner) (*Comissao, error) {
	var c Comissao
	err := s.Scan(&c.ID, &c.TicketID, &c.ValorServicoCents, &c.ValorParcelaCents, &c.ComissaoCents,
		&c.Observacoes, &c.DataRegistro, &c.Recebido, &c.RecebidoEm, &c.CreatedAt, &c.UpdatedAt)
	return &c, err
}

// Salvar grava (upsert por card) o comissionamento, recalculando a comissão.
// Ajusta created_at/updated_at em UTC; devolve o registro final.
func (r *ComissaoRepo) Salvar(ctx context.Context, ticketID int64, servicoCents, parcelaCents int64, observacoes, dataRegistro string) (*Comissao, error) {
	obs, data := strings.TrimSpace(observacoes), strings.TrimSpace(dataRegistro)
	agora := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.ExecContext(ctx, `
INSERT INTO comissoes (ticket_id, valor_servico_cents, valor_parcela_cents, comissao_cents,
    observacoes, data_registro, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(ticket_id) DO UPDATE SET
    valor_servico_cents = excluded.valor_servico_cents,
    valor_parcela_cents = excluded.valor_parcela_cents,
    comissao_cents      = excluded.comissao_cents,
    observacoes         = excluded.observacoes,
    data_registro       = excluded.data_registro,
    updated_at          = excluded.updated_at`,
		ticketID, servicoCents, parcelaCents, ComissaoDe(servicoCents, parcelaCents), obs, data, agora)
	if err != nil {
		return nil, err
	}
	return r.PorTicket(ctx, ticketID)
}

// PorTicket devolve a comissão do card; sql.ErrNoRows se não há registro.
func (r *ComissaoRepo) PorTicket(ctx context.Context, ticketID int64) (*Comissao, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+colunasComissao+` FROM comissoes WHERE ticket_id = ?`, ticketID)
	return scanComissao(row)
}

// MarcarRecebida marca a comissão do card como recebida hoje.
func (r *ComissaoRepo) MarcarRecebida(ctx context.Context, ticketID int64) (*Comissao, error) {
	agora := time.Now().UTC().Format(time.RFC3339)
	res, err := r.db.ExecContext(ctx,
		`UPDATE comissoes SET recebido = 1, recebido_em = ?, updated_at = ? WHERE ticket_id = ?`,
		agora[:10], agora, ticketID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, sql.ErrNoRows
	}
	return r.PorTicket(ctx, ticketID)
}

// Listar devolve todas as comissões: pendentes primeiro (mais recentes),
// depois as recebidas pela data em que entraram no histórico.
func (r *ComissaoRepo) Listar(ctx context.Context) ([]Comissao, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+colunasComissao+` FROM comissoes
        ORDER BY recebido, COALESCE(NULLIF(data_registro, ''), created_at) DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Comissao{}
	for rows.Next() {
		c, err := scanComissao(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// ApagarDoCard remove o registro quando o card sai do fluxo (uso interno).
func (r *ComissaoRepo) ApagarDoCard(ctx context.Context, ticketID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM comissoes WHERE ticket_id = ?`, ticketID)
	return err
}
