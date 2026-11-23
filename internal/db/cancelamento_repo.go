package db

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// Motivos de cancelamento (tags criadas pelo usuário ao fechar o card) e o
// registro por card: qual motivo + resultado (reversão/perda). O registro é
// único por card (ticket_id UNIQUE): re-registrar atualiza.

type MotivoCancelamento struct {
	ID        int64  `json:"id"`
	Nome      string `json:"nome"`
	NomeChave string `json:"nomeChave"`
	CriadoEm  string `json:"criadoEm"`
}

type Cancelamento struct {
	ID        int64  `json:"id"`
	TicketID  int64  `json:"ticketId"`
	Motivo    string `json:"motivo"`
	Resultado string `json:"resultado"` // reversao | perda | em_tratamento
	CriadoEm  string `json:"criadoEm"`
}

type MetricaMotivo struct {
	Motivo     string `json:"motivo"`
	Total      int    `json:"total"`
	Reversao   int    `json:"reversao"`
	Perda      int    `json:"perda"`
	Tratamento int    `json:"tratamento"`
}

type PontoSerie struct {
	Dia      string `json:"dia"`
	Reversao int    `json:"reversao"`
	Perda    int    `json:"perda"`
}

type MetricasCancelamento struct {
	Total      int             `json:"total"`
	Reversao   int             `json:"reversao"`
	Perda      int             `json:"perda"`
	Tratamento int             `json:"tratamento"`
	PorMotivo  []MetricaMotivo `json:"porMotivo"`
	Serie      []PontoSerie    `json:"serie"`
}

type CancelamentoRepo struct {
	db *sql.DB
}

func NewCancelamentoRepo(db *sql.DB) *CancelamentoRepo { return &CancelamentoRepo{db: db} }

var espacos = regexp.MustCompile(`\s+`)

// replAcentosMotivo: caixa/acento não diferenciam tags ("Problema Financeiro"
// = "problema financeiro"); mesmo mapa do service (intencao.go).
var replAcentosMotivo = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a",
	"é", "e", "ê", "e",
	"í", "i",
	"ó", "o", "ô", "o", "õ", "o",
	"ú", "u", "ü", "u",
	"ç", "c",
)

// ChaveMotivo normaliza o nome digitado p/ comparar tags: minúsculo, sem
// acento, sem espaços duplos e no SINGULAR (palavra ≥4 letras terminando em
// "s" perde o "s") — assim "Problemas Financeiros" e "Problema Financeiro"
// viram a mesma tag ("problema financeiro").
func ChaveMotivo(nome string) string {
	texto := replAcentosMotivo.Replace(espacos.ReplaceAllString(strings.ToLower(strings.TrimSpace(nome)), " "))
	palavras := strings.Fields(texto)
	for i, p := range palavras {
		if len([]rune(p)) >= 4 && strings.HasSuffix(p, "s") {
			palavras[i] = strings.TrimSuffix(p, "s")
		}
	}
	return strings.Join(palavras, " ")
}

// DeduplicarMotivos mescla tags que ficaram duplicadas antes da chave
// canônica (singular/caixa/acento): cancelamentos passam pro motivo mais
// antigo do grupo, a chave dele é reescrita e as duplicadas são apagadas.
// Idempotente — roda no boot sem custo quando não há duplicadas.
func (r *CancelamentoRepo) DeduplicarMotivos(ctx context.Context) (int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, nome FROM motivos_cancelamento ORDER BY id`)
	if err != nil {
		return 0, err
	}
	type tag struct {
		id    int64
		chave string
	}
	defer rows.Close()
	var todas []tag
	for rows.Next() {
		var id int64
		var nome string
		if err := rows.Scan(&id, &nome); err != nil {
			return 0, err
		}
		todas = append(todas, tag{id: id, chave: ChaveMotivo(nome)})
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	keep := map[int64]string{} // id canônico -> chave canônica
	porChave := map[string]int64{}
	var duplicados []tag
	for _, t := range todas {
		if _, ok := porChave[t.chave]; ok {
			duplicados = append(duplicados, t)
			continue
		}
		porChave[t.chave] = t.id
		keep[t.id] = t.chave
	}
	if len(duplicados) == 0 {
		return 0, nil
	}
	for _, d := range duplicados {
		if _, err := r.db.ExecContext(ctx,
			`UPDATE cancelamentos SET motivo_id = ? WHERE motivo_id = ?`, porChave[d.chave], d.id); err != nil {
			return 0, err
		}
	}
	// reescreve a chave do canônico p/ futuras inserções casarem
	for id, chave := range keep {
		if _, err := r.db.ExecContext(ctx,
			`UPDATE motivos_cancelamento SET nome_chave = ? WHERE id = ?`, chave, id); err != nil {
			return 0, err
		}
	}
	for _, d := range duplicados {
		if _, err := r.db.ExecContext(ctx,
			`DELETE FROM motivos_cancelamento WHERE id = ?`, d.id); err != nil {
			return 0, err
		}
	}
	return len(duplicados), nil
}

// MotivosListar devolve as tags em ordem alfabética (alimenta o datalist).
func (r *CancelamentoRepo) MotivosListar(ctx context.Context) ([]MotivoCancelamento, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, nome, nome_chave, criado_em FROM motivos_cancelamento ORDER BY nome`)
	if err != nil {
		return nil, fmt.Errorf("listar motivos: %w", err)
	}
	defer rows.Close()
	out := []MotivoCancelamento{}
	for rows.Next() {
		var m MotivoCancelamento
		if err := rows.Scan(&m.ID, &m.Nome, &m.NomeChave, &m.CriadoEm); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MotivoCriarSeNaoExiste devolve a tag existente para o nome (comparando a
// chave) ou cria uma nova com o nome como foi digitado.
func (r *CancelamentoRepo) MotivoCriarSeNaoExiste(ctx context.Context, nome string) (MotivoCancelamento, error) {
	nome = strings.TrimSpace(nome)
	if nome == "" {
		return MotivoCancelamento{}, fmt.Errorf("motivo vazio")
	}
	chave := ChaveMotivo(nome)
	var m MotivoCancelamento
	err := r.db.QueryRowContext(ctx,
		`SELECT id, nome, nome_chave, criado_em FROM motivos_cancelamento WHERE nome_chave = ?`, chave,
	).Scan(&m.ID, &m.Nome, &m.NomeChave, &m.CriadoEm)
	if err == nil {
		return m, nil
	}
	if err != sql.ErrNoRows {
		return MotivoCancelamento{}, fmt.Errorf("buscar motivo: %w", err)
	}
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO motivos_cancelamento (nome, nome_chave) VALUES (?, ?)
		 ON CONFLICT(nome_chave) DO NOTHING`, nome, chave)
	if err != nil {
		return MotivoCancelamento{}, fmt.Errorf("criar motivo: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// perdeu uma corrida: busca a que já existia
		return r.MotivoCriarSeNaoExiste(ctx, nome)
	}
	m.Nome, m.NomeChave = nome, chave
	m.ID, _ = res.LastInsertId()
	return m, nil
}

// CancelarMarcar grava (ou atualiza) o cancelamento do card.
func (r *CancelamentoRepo) CancelarMarcar(ctx context.Context, ticketID, motivoID int64, resultado string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO cancelamentos (ticket_id, motivo_id, resultado) VALUES (?, ?, ?)
		 ON CONFLICT(ticket_id) DO UPDATE SET motivo_id = excluded.motivo_id, resultado = excluded.resultado`,
		ticketID, motivoID, resultado)
	if err != nil {
		return fmt.Errorf("registrar cancelamento: %w", err)
	}
	return nil
}

// CancelarDoTicket devolve o registro do card; nil se nunca fechou com motivo.
func (r *CancelamentoRepo) CancelarDoTicket(ctx context.Context, ticketID int64) (*Cancelamento, error) {
	var c Cancelamento
	err := r.db.QueryRowContext(ctx,
		`SELECT c.id, c.ticket_id, m.nome, c.resultado, c.criado_em
		 FROM cancelamentos c JOIN motivos_cancelamento m ON m.id = c.motivo_id
		 WHERE c.ticket_id = ?`, ticketID,
	).Scan(&c.ID, &c.TicketID, &c.Motivo, &c.Resultado, &c.CriadoEm)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buscar cancelamento do card %d: %w", ticketID, err)
	}
	return &c, nil
}

// CancelarApagar remove o registro — o card saiu de "Ticket Fechado".
func (r *CancelamentoRepo) CancelarApagar(ctx context.Context, ticketID int64) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM cancelamentos WHERE ticket_id = ?`, ticketID); err != nil {
		return fmt.Errorf("apagar cancelamento do card %d: %w", ticketID, err)
	}
	return nil
}

// Metricas agrega os cancelamentos dos últimos dias (0 = todos), com série
// por dia no fuso local e quebra por motivo.
func (r *CancelamentoRepo) Metricas(ctx context.Context, dias int) (*MetricasCancelamento, error) {
	m := &MetricasCancelamento{PorMotivo: []MetricaMotivo{}, Serie: []PontoSerie{}}
	filtro := ""
	args := []any{}
	if dias > 0 {
		filtro = ` WHERE c.criado_em >= strftime('%Y-%m-%dT%H:%M:%SZ','now',?)`
		args = append(args, fmt.Sprintf("-%d days", dias))
	}
	err := r.db.QueryRowContext(ctx,
		`SELECT count(*),
		        COALESCE(SUM(CASE WHEN resultado='reversao' THEN 1 ELSE 0 END),0),
		        COALESCE(SUM(CASE WHEN resultado='perda' THEN 1 ELSE 0 END),0),
		        COALESCE(SUM(CASE WHEN resultado='em_tratamento' THEN 1 ELSE 0 END),0)
		 FROM cancelamentos c`+filtro, args...,
	).Scan(&m.Total, &m.Reversao, &m.Perda, &m.Tratamento)
	if err != nil {
		return nil, fmt.Errorf("métricas de cancelamento: %w", err)
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT m.nome, count(*),
		        COALESCE(SUM(CASE WHEN c.resultado='reversao' THEN 1 ELSE 0 END),0),
		        COALESCE(SUM(CASE WHEN c.resultado='perda' THEN 1 ELSE 0 END),0),
		        COALESCE(SUM(CASE WHEN c.resultado='em_tratamento' THEN 1 ELSE 0 END),0)
		 FROM cancelamentos c JOIN motivos_cancelamento m ON m.id = c.motivo_id`+filtro+`
		 GROUP BY m.id ORDER BY count(*) DESC, m.nome`, args...)
	if err != nil {
		return nil, fmt.Errorf("cancelamentos por motivo: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var x MetricaMotivo
		if err := rows.Scan(&x.Motivo, &x.Total, &x.Reversao, &x.Perda, &x.Tratamento); err != nil {
			return nil, err
		}
		m.PorMotivo = append(m.PorMotivo, x)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = r.db.QueryContext(ctx,
		`SELECT date(c.criado_em, 'localtime') AS dia,
		        COALESCE(SUM(CASE WHEN c.resultado='reversao' THEN 1 ELSE 0 END),0),
		        COALESCE(SUM(CASE WHEN c.resultado='perda' THEN 1 ELSE 0 END),0)
		 FROM cancelamentos c`+filtro+`
		 GROUP BY dia ORDER BY dia`, args...)
	if err != nil {
		return nil, fmt.Errorf("série de cancelamentos: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p PontoSerie
		if err := rows.Scan(&p.Dia, &p.Reversao, &p.Perda); err != nil {
			return nil, err
		}
		m.Serie = append(m.Serie, p)
	}
	return m, rows.Err()
}

// CancelamentosDoMotivo lista os cancelamentos de uma tag (clique no gráfico).
func (r *CancelamentoRepo) CancelamentosDoMotivo(ctx context.Context, nomeChave string) ([]Cancelamento, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT c.id, c.ticket_id, m.nome, c.resultado, c.criado_em
		 FROM cancelamentos c JOIN motivos_cancelamento m ON m.id = c.motivo_id
		 WHERE m.nome_chave = ? ORDER BY c.criado_em DESC`, nomeChave)
	if err != nil {
		return nil, fmt.Errorf("cancelamentos do motivo %s: %w", nomeChave, err)
	}
	defer rows.Close()
	out := []Cancelamento{}
	for rows.Next() {
		var c Cancelamento
		if err := rows.Scan(&c.ID, &c.TicketID, &c.Motivo, &c.Resultado, &c.CriadoEm); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
