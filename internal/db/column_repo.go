package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ErrColunaNaoVazia: não se apaga coluna com cards dentro.
var ErrColunaNaoVazia = errors.New("a coluna tem cards — mova-os antes de remover")

// ColumnRepo gerencia as colunas dinâmicas do Kanban.
type ColumnRepo struct{ db *sql.DB }

func NewColumnRepo(db *sql.DB) *ColumnRepo { return &ColumnRepo{db: db} }

// List devolve os nomes na ordem do board; vazio nunca (a migração semeia).
func (r *ColumnRepo) List(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT nome FROM kanban_columns ORDER BY posicao, nome`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Add cria a coluna no fim do board. Nome é aparado e limitado a 40 chars.
func (r *ColumnRepo) Add(ctx context.Context, nome string) (string, error) {
	nome = strings.TrimSpace(nome)
	if nome == "" || len(nome) > 40 {
		return "", errors.New("nome de coluna inválido (1 a 40 caracteres)")
	}
	var max int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(posicao), -1) FROM kanban_columns`).Scan(&max); err != nil {
		return "", err
	}
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO kanban_columns (nome, posicao) VALUES (?, ?)`, nome, max+1); err != nil {
		return "", err
	}
	return nome, nil
}

// Existe informa se a coluna está registrada.
func (r *ColumnRepo) Existe(ctx context.Context, nome string) (bool, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM kanban_columns WHERE nome = ?`, nome).Scan(&n)
	return n > 0, err
}

// Remove apaga a coluna; recusa se houver tickets nela.
func (r *ColumnRepo) Remove(ctx context.Context, nome string) error {
	var n int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tickets WHERE coluna_atual = ?`, nome).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrColunaNaoVazia
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM kanban_columns WHERE nome = ?`, nome)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
