package contasync

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"math/big"
	"strings"
	"time"
)

type executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Item é uma alteração local ainda não confirmada pela nuvem.
type Item struct {
	ID           int64
	Tabela       string
	RegistroID   string
	Op           string
	AtualizadoEm string
	Versao       int64
	Tentativas   int
}

// Suprimir executa fn sem alimentar a fila. Serve para gravar o que veio da nuvem.
func Suprimir(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE sync_suprimir SET ativo = 1 WHERE id = 1`); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sync_suprimir SET ativo = 0 WHERE id = 1`); err != nil {
		return err
	}
	return tx.Commit()
}

func MetaGet(ctx context.Context, db executor, chave string) (string, error) {
	var v string
	err := db.QueryRowContext(ctx, `SELECT valor FROM sync_meta WHERE chave = ?`, chave).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func MetaSet(ctx context.Context, db executor, chave, valor string) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO sync_meta (chave, valor) VALUES (?, ?)
		 ON CONFLICT(chave) DO UPDATE SET valor = excluded.valor`, chave, valor)
	return err
}

func ContarPendentes(ctx context.Context, db executor) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sync_fila`).Scan(&n)
	return n, err
}

func cursor(ctx context.Context, db executor) (string, error) {
	var desde string
	err := db.QueryRowContext(ctx, `SELECT desde FROM sync_cursor WHERE id = 1`).Scan(&desde)
	if err == sql.ErrNoRows {
		return "1970-01-01T00:00:00Z", nil
	}
	return desde, err
}

func gravarCursor(ctx context.Context, db executor, desde string) error {
	_, err := db.ExecContext(ctx, `UPDATE sync_cursor SET desde = ? WHERE id = 1`, desde)
	return err
}

func pendentes(ctx context.Context, db executor, agora time.Time, limite int) ([]Item, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, tabela, registro_id, op, atualizado_em, versao, tentativas
		 FROM sync_fila WHERE proxima_em <= ? ORDER BY id LIMIT ?`,
		carimboAgora(agora), limite)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Tabela, &it.RegistroID, &it.Op, &it.AtualizadoEm, &it.Versao, &it.Tentativas); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func adiar(ctx context.Context, db executor, itens []Item, quando time.Time) error {
	if len(itens) == 0 {
		return nil
	}
	marca := carimboAgora(quando)
	for _, it := range itens {
		if _, err := db.ExecContext(ctx,
			`UPDATE sync_fila SET tentativas = tentativas + 1, proxima_em = ? WHERE id = ? AND versao = ?`,
			marca, it.ID, it.Versao); err != nil {
			return err
		}
	}
	return nil
}

// enfileirarBase coloca na fila o que já existia antes do pareamento,
// sem mexer em alteração que o gatilho já registrou.
func enfileirarBase(ctx context.Context, db *sql.DB) error {
	ja, err := MetaGet(ctx, db, "baseline")
	if err != nil {
		return err
	}
	if ja == "1" {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, nome := range ordemUpsert {
		spec := tabelas[nome]
		q := fmt.Sprintf(`
			INSERT INTO sync_fila (tabela, registro_id, op, atualizado_em, versao, tentativas, proxima_em)
			SELECT '%s', %s, 'upsert', %s, 1, 0, strftime('%%Y-%%m-%%dT%%H:%%M:%%fZ','now')
			FROM %s
			WHERE NOT EXISTS (
				SELECT 1 FROM sync_estado e WHERE e.tabela = '%s' AND e.registro_id = %s
			)
			ON CONFLICT(tabela, registro_id) DO NOTHING`,
			spec.nome, exprIDSQL(spec, spec.nome), spec.carimboLocal,
			spec.nome, spec.nome, exprIDSQL(spec, spec.nome))
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("baseline %s: %w", spec.nome, err)
		}
	}
	if err := MetaSet(ctx, tx, "baseline", "1"); err != nil {
		return err
	}
	return tx.Commit()
}

func exprIDSQL(spec specTabela, alias string) string {
	if spec.pkInt {
		return fmt.Sprintf("CAST(%s.id AS TEXT)", alias)
	}
	return alias + "." + spec.pk
}

func montar(ctx context.Context, db executor, it Item) (Alteracao, error) {
	alt := Alteracao{
		Tabela: it.Tabela, ID: it.RegistroID,
		Apagado: it.Op == "delete", AtualizadoEm: it.AtualizadoEm, Versao: it.Versao,
	}
	if alt.Apagado {
		return alt, nil
	}
	dados, ok, err := lerDados(ctx, db, it.Tabela, it.RegistroID)
	if err != nil {
		return alt, err
	}
	if !ok {
		alt.Apagado = true
		alt.Dados = nil
		return alt, nil
	}
	alt.Dados = dados
	return alt, nil
}

func lerDados(ctx context.Context, db executor, tabela, id string) (map[string]any, bool, error) {
	spec, ok := specDe(tabela)
	if !ok {
		return nil, false, fmt.Errorf("tabela desconhecida")
	}
	pk, err := idArg(spec, id)
	if err != nil {
		return nil, false, err
	}
	cols := make([]string, len(spec.campos))
	for i, c := range spec.campos {
		if c.tipo == campoTexto || c.tipo == campoTempo {
			cols[i] = fmt.Sprintf("COALESCE(%s, '')", c.nome)
		} else {
			cols[i] = fmt.Sprintf("COALESCE(%s, 0)", c.nome)
		}
	}
	q := fmt.Sprintf("SELECT %s FROM %s WHERE %s = ?", strings.Join(cols, ", "), spec.nome, spec.pk)
	dest := make([]any, len(spec.campos))
	ptrs := make([]any, len(spec.campos))
	for i, c := range spec.campos {
		if c.tipo == campoInt || c.tipo == campoBool {
			dest[i] = new(int64)
		} else {
			dest[i] = new(string)
		}
		ptrs[i] = dest[i]
	}
	err = db.QueryRowContext(ctx, q, pk).Scan(ptrs...)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	out := make(map[string]any, len(spec.campos))
	for i, c := range spec.campos {
		switch p := dest[i].(type) {
		case *int64:
			out[c.nome] = *p
		case *string:
			out[c.nome] = *p
		}
	}
	return out, true, nil
}

func gravarAlteracao(ctx context.Context, tx *sql.Tx, alt Alteracao) error {
	spec, ok := specDe(alt.Tabela)
	if !ok {
		return fmt.Errorf("tabela desconhecida")
	}
	pk, err := idArg(spec, alt.ID)
	if err != nil {
		return err
	}
	if alt.Apagado {
		_, err = tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE %s = ?", spec.nome, spec.pk), pk)
		return err
	}
	if alt.Tabela == "cobrancas" && comoInt(alt.Dados["ativa"]) != 0 {
		if _, err := tx.ExecContext(ctx,
			`UPDATE cobrancas SET ativa = 0 WHERE ticket_id = ? AND id <> ? AND ativa = 1`,
			comoInt(alt.Dados["ticket_id"]), pk); err != nil {
			return err
		}
	}
	cols := make([]string, 0, len(spec.campos)+1)
	args := make([]any, 0, len(spec.campos)+1)
	cols = append(cols, spec.pk)
	args = append(args, pk)
	for _, c := range spec.campos {
		cols = append(cols, c.nome)
		args = append(args, valorColuna(c, alt.Dados[c.nome], alt.AtualizadoEm))
	}
	ph := make([]string, len(cols))
	sets := make([]string, len(spec.campos))
	for i := range cols {
		ph[i] = "?"
	}
	for i, c := range spec.campos {
		sets[i] = c.nome + " = excluded." + c.nome
	}
	q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT(%s) DO UPDATE SET %s",
		spec.nome, strings.Join(cols, ", "), strings.Join(ph, ", "), spec.pk, strings.Join(sets, ", "))
	_, err = tx.ExecContext(ctx, q, args...)
	return err
}

func registrarHistorico(ctx context.Context, tx executor, tabela, id, motivo, detalhe string) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sync_historico (tabela, registro_id, motivo, detalhe) VALUES (?, ?, ?, ?)`,
		tabela, id, motivo, detalhe); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx,
		`DELETE FROM sync_historico WHERE id < (
			SELECT id FROM sync_historico ORDER BY id DESC LIMIT 1 OFFSET 500)`)
	return err
}

func gravarEstado(ctx context.Context, tx executor, alt Alteracao) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO sync_estado (tabela, registro_id, atualizado_em, versao) VALUES (?, ?, ?, ?)
		 ON CONFLICT(tabela, registro_id) DO UPDATE SET
		   atualizado_em = excluded.atualizado_em,
		   versao = excluded.versao`,
		alt.Tabela, alt.ID, alt.AtualizadoEm, alt.Versao)
	return err
}

func removerFilaSe(ctx context.Context, tx executor, tabela, id string, versao int64) error {
	_, err := tx.ExecContext(ctx,
		`DELETE FROM sync_fila WHERE tabela = ? AND registro_id = ? AND versao = ?`,
		tabela, id, versao)
	return err
}

func lerFila(ctx context.Context, db executor, tabela, id string) (Item, bool, error) {
	var it Item
	err := db.QueryRowContext(ctx,
		`SELECT id, tabela, registro_id, op, atualizado_em, versao, tentativas
		 FROM sync_fila WHERE tabela = ? AND registro_id = ?`, tabela, id).
		Scan(&it.ID, &it.Tabela, &it.RegistroID, &it.Op, &it.AtualizadoEm, &it.Versao, &it.Tentativas)
	if err == sql.ErrNoRows {
		return Item{}, false, nil
	}
	return it, err == nil, err
}

func lerEstado(ctx context.Context, db executor, tabela, id string) (string, int64, bool, error) {
	var em string
	var ver int64
	err := db.QueryRowContext(ctx,
		`SELECT atualizado_em, versao FROM sync_estado WHERE tabela = ? AND registro_id = ?`,
		tabela, id).Scan(&em, &ver)
	if err == sql.ErrNoRows {
		return "", 0, false, nil
	}
	return em, ver, err == nil, err
}

const faixaStride = 1_000_000

// reservarFaixa afasta o próximo id deste PC dos ids dos outros.
// Sem isso, dois computadores criariam o card 501 ao mesmo tempo.
func reservarFaixa(ctx context.Context, db *sql.DB) error {
	slotTxt, err := MetaGet(ctx, db, "faixa_slot")
	if err != nil {
		return err
	}
	var slot int64
	if slotTxt == "" {
		n, err := rand.Int(rand.Reader, big.NewInt(faixaStride-2))
		if err != nil {
			return err
		}
		slot = n.Int64() + 1 // 1 .. stride-2
		if err := MetaSet(ctx, db, "faixa_slot", fmt.Sprintf("%d", slot)); err != nil {
			return err
		}
	} else {
		slot, err = parseInt(slotTxt)
		if err != nil || slot <= 0 {
			slot = 1
		}
	}
	for _, nome := range ordemUpsert {
		spec := tabelas[nome]
		if !spec.pkInt {
			continue
		}
		var maxID int64
		if err := db.QueryRowContext(ctx,
			fmt.Sprintf("SELECT COALESCE(MAX(id), 0) FROM %s", spec.nome)).Scan(&maxID); err != nil {
			return err
		}
		cand := proximoDaFaixa(maxID, slot)
		res, err := db.ExecContext(ctx,
			`UPDATE sqlite_sequence SET seq = CASE WHEN seq > ? THEN seq ELSE ? END WHERE name = ?`,
			cand-1, cand-1, spec.nome)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			if _, err := db.ExecContext(ctx,
				`INSERT INTO sqlite_sequence (name, seq) VALUES (?, ?)`, spec.nome, cand-1); err != nil {
				return err
			}
		}
	}
	return nil
}

func proximoDaFaixa(maxID, slot int64) int64 {
	if slot <= 0 {
		slot = 1
	}
	slot = slot % faixaStride
	if slot == 0 {
		slot = 1
	}
	base := maxID + 1
	rem := base % faixaStride
	delta := (slot - rem) % faixaStride
	if delta < 0 {
		delta += faixaStride
	}
	cand := base + delta
	if cand <= maxID {
		cand += faixaStride
	}
	return cand
}

func parseInt(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscan(s, &n)
	return n, err
}
