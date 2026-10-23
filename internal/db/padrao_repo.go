package db

import (
	"context"
	"database/sql"
	"strings"
)

// Limites de um padrão: o texto entra no system prompt de TODA conversa, então
// precisa caber sem estourar o contexto.
const (
	MaxNomePadrao     = 60
	MaxConteudoPadrao = 2000
	MaxPadroes        = 30
)

// Erros de validação de padrões (voltam como mensagem ao agente/usuário).
var (
	ErrPadraoNomeVazio    = errPadrao("o padrão precisa de um nome")
	ErrPadraoConteudoVazio = errPadrao("o conteúdo do padrão está vazio")
	ErrPadraoNaoEncontrado = errPadrao("padrão não encontrado")
	ErrPadroesExcedidos    = errPadrao("limite de 30 padrões atingido — apague um antes de criar outro")
)

type errPadrao string

func (e errPadrao) Error() string { return string(e) }

// Padrao é uma instrução permanente que o usuário ensinou ao assistente.
type Padrao struct {
	Nome     string `json:"nome"`
	Conteudo string `json:"conteudo"`
}

// PadraoRepo persiste os padrões do assistente na tabela padroes.
type PadraoRepo struct{ db *sql.DB }

func NewPadraoRepo(db *sql.DB) *PadraoRepo { return &PadraoRepo{db: db} }

// List devolve os padrões em ordem alfabética de nome.
func (r *PadraoRepo) List(ctx context.Context) ([]Padrao, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT nome, conteudo FROM padroes ORDER BY nome`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Padrao
	for rows.Next() {
		var p Padrao
		if err := rows.Scan(&p.Nome, &p.Conteudo); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Upsert cria ou substitui o padrão pelo nome (reensinar um padrão atualiza).
func (r *PadraoRepo) Upsert(ctx context.Context, nome, conteudo string) error {
	nome, conteudo = strings.TrimSpace(nome), strings.TrimSpace(conteudo)
	if nome == "" {
		return ErrPadraoNomeVazio
	}
	if len([]rune(nome)) > MaxNomePadrao {
		nome = string([]rune(nome)[:MaxNomePadrao])
	}
	if conteudo == "" {
		return ErrPadraoConteudoVazio
	}
	if len([]rune(conteudo)) > MaxConteudoPadrao {
		conteudo = string([]rune(conteudo)[:MaxConteudoPadrao])
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO padroes (nome, conteudo) VALUES (?, ?)
		 ON CONFLICT(nome) DO UPDATE SET conteudo = excluded.conteudo`,
		nome, conteudo)
	return err
}

// Delete apaga o padrão pelo nome.
func (r *PadraoRepo) Delete(ctx context.Context, nome string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM padroes WHERE nome = ?`, strings.TrimSpace(nome))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrPadraoNaoEncontrado
	}
	return nil
}

// Contagem devolve quantos padrões existem (para o limite de MaxPadroes).
func (r *PadraoRepo) Contagem(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM padroes`).Scan(&n)
	return n, err
}
