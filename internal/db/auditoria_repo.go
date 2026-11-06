package db

import (
	"context"
	"database/sql"
	"time"
)

// Origens de uma troca do assistente, na coluna chat_auditoria.origem.
const (
	OrigemLocalComando = "local_comando" // /comandos e intenções rígidas, zero IA
	OrigemLocalSemIA   = "local_sem_ia"  // respostas de banco local sem IA (ex.: board sem chave)
	OrigemIA           = "ia"            // passou pelo agente (Groq/DeepInfra)
)

// FerramentaAuditada é uma ferramenta que o agente chamou na troca.
type FerramentaAuditada struct {
	Nome string `json:"nome"`
	Args string `json:"args,omitempty"`
	Erro string `json:"erro,omitempty"`
}

// AuditoriaChat é uma linha do log append-only do assistente.
type AuditoriaChat struct {
	ID               int64  `json:"id"`
	CreatedAt        string `json:"createdAt"`
	ChatID           int64  `json:"chatId"`
	Origem           string `json:"origem"`
	Comando          string `json:"comando"`
	Mensagem         string `json:"mensagem"`
	Resposta         string `json:"resposta"`
	Ferramentas      string `json:"ferramentas"` // JSON de []FerramentaAuditada
	PromptTokens     int    `json:"promptTokens"`
	CompletionTokens int    `json:"completionTokens"`
	Rodadas          int    `json:"rodadas"`
	DuracaoMs        int64  `json:"duracaoMs"`
}

// AuditoriaRepo grava o histórico de auditoria do assistente. Append-only:
// nenhum método apaga — o log sobrevive ao Delete do chat.
type AuditoriaRepo struct{ db *sql.DB }

func NewAuditoriaRepo(db *sql.DB) *AuditoriaRepo { return &AuditoriaRepo{db: db} }

// Registrar insere uma linha. Falha não derruba o chat: quem chama ignora
// o erro (log é acessório).
func (r *AuditoriaRepo) Registrar(ctx context.Context, a AuditoriaChat) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO chat_auditoria
		(chat_id, origem, comando, mensagem, resposta, ferramentas,
		 prompt_tokens, completion_tokens, rodadas, duracao_ms)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		a.ChatID, a.Origem, a.Comando, a.Mensagem, a.Resposta, a.Ferramentas,
		a.PromptTokens, a.CompletionTokens, a.Rodadas, a.DuracaoMs)
	return err
}

// Listar devolve todo o log, do mais antigo ao mais recente.
func (r *AuditoriaRepo) Listar(ctx context.Context) ([]AuditoriaChat, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, created_at, chat_id, origem, comando,
		mensagem, resposta, ferramentas, prompt_tokens, completion_tokens, rodadas, duracao_ms
		FROM chat_auditoria ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditoriaChat{}
	for rows.Next() {
		var a AuditoriaChat
		if err := rows.Scan(&a.ID, &a.CreatedAt, &a.ChatID, &a.Origem, &a.Comando,
			&a.Mensagem, &a.Resposta, &a.Ferramentas, &a.PromptTokens,
			&a.CompletionTokens, &a.Rodadas, &a.DuracaoMs); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Resumo é a estatística consolidada do log, para o topo da exportação.
type Resumo struct {
	PeriodoInicio    string         `json:"periodoInicio"`
	PeriodoFim       string         `json:"periodoFim"`
	Trocas           int            `json:"trocas"`
	PorOrigem        map[string]int `json:"porOrigem"`
	PromptTokens     int            `json:"promptTokens"`
	CompletionTokens int            `json:"completionTokens"`
}

// Resumo totaliza o log por origem — a métrica central de "quanto já roda
// sem IA" e de onde a IA ainda é acionada.
func (r *AuditoriaRepo) Resumo(ctx context.Context) (Resumo, error) {
	linhas, err := r.Listar(ctx)
	if err != nil {
		return Resumo{}, err
	}
	res := Resumo{PorOrigem: map[string]int{}}
	for _, l := range linhas {
		res.Trocas++
		res.PorOrigem[l.Origem]++
		res.PromptTokens += l.PromptTokens
		res.CompletionTokens += l.CompletionTokens
	}
	if len(linhas) > 0 {
		res.PeriodoInicio = formatarDataAuditoria(linhas[0].CreatedAt)
		res.PeriodoFim = formatarDataAuditoria(linhas[len(linhas)-1].CreatedAt)
	}
	return res, nil
}

func formatarDataAuditoria(s string) string {
	t, err := time.Parse("2006-01-02T15:04:05.999Z", s)
	if err != nil {
		return s
	}
	return t.Format("2006-01-02 15:04")
}
