package db

import (
	"context"
	"database/sql"
)

// AgenteStatsRepo grava o histórico de uso do assistente por classe de
// consulta — é o "aprendizado" da pré-busca: se o modelo pede sempre a
// mesma ferramenta extra numa classe, ela passa a vir pré-coletada.

type AgenteStatsRepo struct{ db *sql.DB }

func NewAgenteStatsRepo(db *sql.DB) *AgenteStatsRepo { return &AgenteStatsRepo{db: db} }

// Incrementar soma 1 ao uso da ferramenta dentro da classe.
func (r *AgenteStatsRepo) Incrementar(ctx context.Context, classe, ferramenta string) error {
	if classe == "" || ferramenta == "" {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
INSERT INTO agente_stats (classe, ferramenta, contagem) VALUES (?, ?, 1)
ON CONFLICT(classe, ferramenta) DO UPDATE SET
    contagem = contagem + 1,
    atualizado_em = strftime('%Y-%m-%dT%H:%M:%SZ','now')`,
		classe, ferramenta)
	return err
}

// TopFerramentas devolve as ferramentas mais chamadas dentro da classe,
// com contagem >= min (ordem decrescente). Vazio/erro = nada aprendido.
func (r *AgenteStatsRepo) TopFerramentas(ctx context.Context, classe string, min int) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT ferramenta FROM agente_stats WHERE classe = ? AND contagem >= ?
ORDER BY contagem DESC, ferramenta`, classe, min)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
