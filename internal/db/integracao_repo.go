package db

import (
	"context"
	"database/sql"
	"time"
)

// Cliente360Repo guarda o cache local dos clientes buscados no 360.
type Cliente360Repo struct{ db *sql.DB }

func NewCliente360Repo(db *sql.DB) *Cliente360Repo { return &Cliente360Repo{db: db} }

// Upsert grava (ou atualiza) o snapshot do cliente no cache.
func (r *Cliente360Repo) Upsert(ctx context.Context, c *Cliente360) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO clientes_360 (cod_service, nome, cnpj, telefone, endereco, unidade,
    saldo_cents, faturas_json, score_json, historico_json, synced_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ','now'))
ON CONFLICT(cod_service) DO UPDATE SET
    nome = excluded.nome, cnpj = excluded.cnpj, telefone = excluded.telefone,
    endereco = excluded.endereco, unidade = excluded.unidade,
    saldo_cents = excluded.saldo_cents, faturas_json = excluded.faturas_json,
    score_json = excluded.score_json, historico_json = excluded.historico_json,
    synced_at = strftime('%Y-%m-%dT%H:%M:%SZ','now')`,
		c.CodService, c.Nome, c.CNPJ, c.Telefone, c.Endereco, c.Unidade,
		c.SaldoCents, c.FaturasJSON, c.ScoreJSON, c.HistoricoJSON)
	return err
}

// Get devolve o cliente em cache ou nil se não houver.
func (r *Cliente360Repo) Get(ctx context.Context, codService string) (*Cliente360, error) {
	c := &Cliente360{}
	err := r.db.QueryRowContext(ctx, `
SELECT cod_service, nome, COALESCE(cnpj,''), COALESCE(telefone,''), COALESCE(endereco,''),
       COALESCE(unidade,''), saldo_cents, COALESCE(faturas_json,''),
       COALESCE(score_json,''), COALESCE(historico_json,''), synced_at
FROM clientes_360 WHERE cod_service = ?`, codService).Scan(
		&c.CodService, &c.Nome, &c.CNPJ, &c.Telefone, &c.Endereco,
		&c.Unidade, &c.SaldoCents, &c.FaturasJSON, &c.ScoreJSON,
		&c.HistoricoJSON, &c.SyncedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// HistoricoRenegociacoes devolve o número de cobranças anteriores do ticket
// (acordos já criados = renegociação anterior). Usado pela avaliação.
func (r *Cliente360Repo) ContaRenegociacoesTicket(ctx context.Context, ticketID int64) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM cobrancas WHERE ticket_id = ?`, ticketID).Scan(&n)
	return n, err
}

// IdadePrimeiraCobranca dias desde a primeira cobrança registrada do ticket.
func (r *Cliente360Repo) IdadePrimeiraCobranca(ctx context.Context, ticketID int64) (int, error) {
	var criadaEm string
	err := r.db.QueryRowContext(ctx,
		`SELECT MIN(created_at) FROM cobrancas WHERE ticket_id = ?`, ticketID).Scan(&criadaEm)
	if err == sql.ErrNoRows || criadaEm == "" {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	t, err := time.Parse("2006-01-02T15:04:05Z", criadaEm)
	if err != nil {
		return 0, nil
	}
	return int(time.Since(t).Hours() / 24), nil
}

// ConfigRepo persiste pares chave/valor (chave Groq, modelo, sessão 360).
type ConfigRepo struct{ db *sql.DB }

func NewConfigRepo(db *sql.DB) *ConfigRepo { return &ConfigRepo{db: db} }

func (r *ConfigRepo) Set(ctx context.Context, chave, valor string) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO config (chave, valor) VALUES (?, ?)
ON CONFLICT(chave) DO UPDATE SET valor = excluded.valor`, chave, valor)
	return err
}

func (r *ConfigRepo) Get(ctx context.Context, chave string) (string, error) {
	var v string
	err := r.db.QueryRowContext(ctx, `SELECT valor FROM config WHERE chave = ?`, chave).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// ParametroRepo armazena os parâmetros de negociação (texto ou arquivo).
type ParametroRepo struct{ db *sql.DB }

func NewParametroRepo(db *sql.DB) *ParametroRepo { return &ParametroRepo{db: db} }

func (r *ParametroRepo) Save(ctx context.Context, p *ParametroNegociacao) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
INSERT INTO parametros_negociacao (fonte, nome_arquivo, conteudo) VALUES (?, ?, ?)`,
		p.Fonte, p.NomeArquivo, p.Conteudo)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (r *ParametroRepo) List(ctx context.Context) ([]ParametroNegociacao, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, fonte, COALESCE(nome_arquivo,''), conteudo, created_at
FROM parametros_negociacao ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ParametroNegociacao
	for rows.Next() {
		var p ParametroNegociacao
		if err := rows.Scan(&p.ID, &p.Fonte, &p.NomeArquivo, &p.Conteudo, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *ParametroRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM parametros_negociacao WHERE id = ?`, id)
	return err
}

// TodosParametros concatena os parâmetros ativos para enviar ao prompt da IA.
func (r *ParametroRepo) TodosParametros(ctx context.Context) (string, error) {
	lista, err := r.List(ctx)
	if err != nil || len(lista) == 0 {
		return "", err
	}
	out := ""
	for _, p := range lista {
		out += p.Conteudo + "\n"
	}
	return out, nil
}
