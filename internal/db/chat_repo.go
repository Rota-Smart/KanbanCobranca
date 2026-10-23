package db

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Chat é uma conversa do assistente Groq; as mensagens ficam em
// chat_mensagens e sobrevivem ao fechamento do app.
type Chat struct {
	ID        int64  `json:"id"`
	Titulo    string `json:"titulo"`
	UpdatedAt string `json:"updatedAt"`
}

// ChatMensagem é uma linha do histórico (papel "user" ou "assistant").
type ChatMensagem struct {
	ID        int64  `json:"id"`
	ChatID    int64  `json:"chatId"`
	Papel     string `json:"papel"`
	Conteudo  string `json:"conteudo"`
	AcoesJSON string `json:"acoesJson"` // JSON de []AcaoDoAgente; vazio se sem ações
}

// ChatRepo persiste as conversas do assistente.
type ChatRepo struct{ db *sql.DB }

func NewChatRepo(db *sql.DB) *ChatRepo { return &ChatRepo{db: db} }

// List devolve os chats do mais recente ao mais antigo.
func (r *ChatRepo) List(ctx context.Context) ([]Chat, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, titulo, updated_at FROM chats ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chat
	for rows.Next() {
		var c Chat
		if err := rows.Scan(&c.ID, &c.Titulo, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Create cria um chat e devolve o ID.
func (r *ChatRepo) Create(ctx context.Context, titulo string) (int64, error) {
	if strings.TrimSpace(titulo) == "" {
		titulo = "Nova conversa"
	}
	res, err := r.db.ExecContext(ctx, `INSERT INTO chats (titulo) VALUES (?)`, titulo)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// LimparVazios apaga chats que nunca receberam mensagem — restos de
// conversas abertas e fechadas sem uso, que poluem a lista como "Nova conversa".
func (r *ChatRepo) LimparVazios(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM chats WHERE id NOT IN (SELECT DISTINCT chat_id FROM chat_mensagens)`)
	return err
}

// Delete apaga o chat e (pelo CASCADE) as mensagens.
func (r *ChatRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM chats WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Mensagens devolve o histórico completo do chat em ordem.
func (r *ChatRepo) Mensagens(ctx context.Context, chatID int64) ([]ChatMensagem, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, chat_id, papel, conteudo, COALESCE(acoes_json,'')
		 FROM chat_mensagens WHERE chat_id = ? ORDER BY id`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatMensagem
	for rows.Next() {
		var m ChatMensagem
		if err := rows.Scan(&m.ID, &m.ChatID, &m.Papel, &m.Conteudo, &m.AcoesJSON); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddMensagem grava uma mensagem e atualiza o chat (título automático na
// primeira do usuário + updated_at) numa única transação.
func (r *ChatRepo) AddMensagem(ctx context.Context, chatID int64, papel, conteudo, acoesJSON string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO chat_mensagens (chat_id, papel, conteudo, acoes_json) VALUES (?, ?, ?, ?)`,
		chatID, papel, conteudo, acoesJSON); err != nil {
		return err
	}
	if papel == "user" {
		if titulo := tituloDe(conteudo); titulo != "" {
			var atual string
			if err := tx.QueryRowContext(ctx,
				`SELECT titulo FROM chats WHERE id = ?`, chatID).Scan(&atual); err == nil && atual == "Nova conversa" {
				if _, err := tx.ExecContext(ctx,
					`UPDATE chats SET titulo = ? WHERE id = ?`, titulo, chatID); err != nil {
					return err
				}
			}
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE chats SET updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format("2006-01-02T15:04:05Z"), chatID); err != nil {
		return err
	}
	return tx.Commit()
}

func tituloDe(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) > 40 {
		return string(runes[:40]) + "…"
	}
	return s
}
