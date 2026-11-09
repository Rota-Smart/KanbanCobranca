// Package nuvem: envia o banco SQLite local para o Postgres na nuvem (Supabase).
// É a parte do app do botão "Enviar dados para a nuvem" — a Etapa 1 do plano
// docs/plano-rotasuite-web-360.md. Idempotente: cada envio sobrescreve linha a
// linha na nuvem preservando os ids; quem envia por último "vence" (a
// sincronização de campo, da Etapa 4, assume depois).
package nuvem

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

// tabelas na ordem de dependência (referenciadas por FK primeiro).
var tabelas = []string{
	"kanban_columns",
	"tickets",
	"cobrancas",
	"movimentacoes",
	"tickets_vinculos",
	"tentativas_contato",
	"comissoes",
	"clientes_360",
	"parametros_negociacao",
	"config",
	"padroes",
	"chats",
	"chat_mensagens",
	"chat_auditoria",
	"agente_stats",
}

// colunas de sincronização: na nuvem são gerenciadas por gatilho/policy e ficam
// de fora da carga.
var colunasSync = map[string]bool{
	"usuario_id": true, "atualizado_em": true, "versao": true, "apagado": true,
}

// colunas booleanas na nuvem que eram 0/1 no SQLite.
var colunasBool = map[string]bool{"ativa": true, "recebido": true}

// Resumo descreve o resultado do envio, tabela a tabela.
type Resumo struct {
	Tabelas  map[string]int `json:"tabelas"`
	Total    int            `json:"total"`
	DuracaoMs int64         `json:"duracaoMs"`
}

// Enviar copia todas as tabelas do SQLite para o Postgres (DSN da nuvem).
func Enviar(ctx context.Context, local *sql.DB, dsn string) (Resumo, error) {
	início := time.Now()
	nuvemDB, err := sql.Open("postgres", dsn)
	if err != nil {
		return Resumo{}, fmt.Errorf("abrir nuvem: %w", err)
	}
	defer nuvemDB.Close()
	nuvemDB.SetMaxOpenConns(3)
	pingCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := nuvemDB.PingContext(pingCtx); err != nil {
		return Resumo{}, fmt.Errorf("conectar à nuvem: %w", err)
	}

	res := Resumo{Tabelas: map[string]int{}}
	for _, t := range tabelas {
		n, err := enviarTabela(ctx, local, nuvemDB, t)
		if err != nil {
			return res, fmt.Errorf("%s: %w", t, err)
		}
		res.Tabelas[t] = n
		res.Total += n
	}
	res.DuracaoMs = time.Since(início).Milliseconds()
	return res, nil
}

func enviarTabela(ctx context.Context, local, destino *sql.DB, tabela string) (int, error) {
	// estrutura da tabela local
	info, err := local.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, tabela))
	if err != nil {
		return 0, err
	}
	type campo struct {
		nome string
		pk   bool
	}
	var campos []campo
	for info.Next() {
		var cid, pk int
		var nome, tipo string
		var notNull, dflt sql.NullString
		if err := info.Scan(&cid, &nome, &tipo, &notNull, &dflt, &pk); err != nil {
			info.Close()
			return 0, err
		}
		campos = append(campos, campo{nome: nome, pk: pk > 0})
	}
	if err := info.Err(); err != nil {
		info.Close()
		return 0, err
	}
	info.Close()
	if len(campos) == 0 {
		return 0, fmt.Errorf("tabela não existe no banco local")
	}

	// a nuvem tem colunas extras (ex.: contratos_json em clientes_360): insert
	// só nas colunas que existem nos dois lados
	existem, err := colunasNuvem(ctx, destino, tabela)
	if err != nil {
		return 0, err
	}
	var cols []string
	var pks []string
	for _, c := range campos {
		if colunasSync[c.nome] {
			continue
		}
		if !existem[c.nome] {
			continue
		}
		cols = append(cols, c.nome)
		if c.pk {
			pks = append(pks, c.nome)
		}
	}
	if len(cols) == 0 {
		return 0, fmt.Errorf("nenhuma coluna em comum com a nuvem")
	}
	sort.Strings(pks) // determinístico
	if len(pks) == 0 {
		return 0, fmt.Errorf("sem chave primária")
	}

	rows, err := local.QueryContext(ctx, `SELECT `+strings.Join(cols, ", ")+` FROM `+tabela)
	if err != nil {
		return 0, err
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	total := 0
	defer rows.Close()

	atualiza := ""
	for _, c := range cols {
		éPK := false
		for _, p := range pks {
			if p == c {
				éPK = true
				break
			}
		}
		if !éPK {
			atualiza += fmt.Sprintf("%s = excluded.%s, ", c, c)
		}
	}
	atualiza = strings.TrimSuffix(atualiza, ", ")
	conflito := "DO NOTHING"
	if atualiza != "" {
		conflito = "DO UPDATE SET " + atualiza
	}

	// lotes de várias linhas por INSERT: uma ida à nuvem a cada lote, não por linha
	const porLote = 200
	var lote []any
	nLote := 0
	enviar := func() error {
		if nLote == 0 {
			return nil
		}
		var grupos []string
		for l := 0; l < nLote; l++ {
			fenos := make([]string, len(cols))
			for i := range cols {
				fenos[i] = fmt.Sprintf("$%d", l*len(cols)+i+1)
			}
			grupos = append(grupos, "("+strings.Join(fenos, ", ")+")")
		}
		q := fmt.Sprintf(`INSERT INTO %s (%s) VALUES %s ON CONFLICT (%s) %s`,
			tabela, strings.Join(cols, ", "), strings.Join(grupos, ", "), strings.Join(pks, ", "), conflito)
		if _, err := destino.ExecContext(ctx, q, lote...); err != nil {
			return err
		}
		total += nLote
		lote, nLote = lote[:0], 0
		return nil
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return total, err
		}
		for i, c := range cols {
			lote = append(lote, converter(c, vals[i]))
		}
		nLote++
		if nLote == porLote {
			if err := enviar(); err != nil {
				return total, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return total, err
	}
	return total, enviar()
}

func colunasNuvem(ctx context.Context, destino *sql.DB, tabela string) (map[string]bool, error) {
	rows, err := destino.QueryContext(ctx,
		`SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name=$1`, tabela)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var nome string
		if err := rows.Scan(&nome); err != nil {
			return nil, err
		}
		out[nome] = true
	}
	return out, rows.Err()
}

func converter(coluna string, v any) any {
	if v == nil {
		return nil
	}
	if colunasBool[coluna] {
		if n, ok := v.(int64); ok {
			return n != 0
		}
	}
	// O Postgres recusa o caractere nulo (0x00) em texto; dados vindos do 360
	// às vezes o trazem.
	switch t := v.(type) {
	case string:
		return strings.ReplaceAll(t, "\x00", "")
	case []byte:
		return strings.ReplaceAll(string(t), "\x00", "")
	}
	return v
}

// TestarConexao confere se o DSN abre e enxerga o esquema do RotaSuite.
func TestarConexao(ctx context.Context, dsn string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name='tickets'`,
	).Scan(&n); err != nil {
		return fmt.Errorf("conexão falhou: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("conectou, mas o banco da nuvem não tem a tabela tickets — rode a Etapa 1 antes")
	}
	return nil
}
