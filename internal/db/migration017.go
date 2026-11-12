package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// A Etapa 4 acrescenta a fila de sincronização e os gatilhos que a alimentam.
// O token da conta não entra aqui: ele fica no Credential Manager.
func init() {
	migrations = append(migrations, migracao017())
}

func migracao017() string {
	b := `
CREATE TABLE sync_suprimir (
    id    INTEGER PRIMARY KEY CHECK (id = 1),
    ativo INTEGER NOT NULL DEFAULT 0
);
INSERT INTO sync_suprimir (id, ativo) VALUES (1, 0);

CREATE TABLE sync_meta (
    chave TEXT PRIMARY KEY,
    valor TEXT NOT NULL
);

CREATE TABLE sync_estado (
    tabela        TEXT    NOT NULL,
    registro_id   TEXT    NOT NULL,
    atualizado_em TEXT    NOT NULL,
    versao        INTEGER NOT NULL,
    PRIMARY KEY (tabela, registro_id)
);

CREATE TABLE sync_fila (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    tabela        TEXT    NOT NULL,
    registro_id   TEXT    NOT NULL,
    op            TEXT    NOT NULL,
    atualizado_em TEXT    NOT NULL,
    versao        INTEGER NOT NULL,
    tentativas    INTEGER NOT NULL DEFAULT 0,
    proxima_em    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (tabela, registro_id)
);

CREATE TABLE sync_historico (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    tabela     TEXT NOT NULL,
    registro_id TEXT NOT NULL,
    motivo     TEXT NOT NULL,
    detalhe    TEXT NOT NULL DEFAULT '',
    criado_em  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE sync_cursor (
    id    INTEGER PRIMARY KEY CHECK (id = 1),
    desde TEXT    NOT NULL
);
INSERT INTO sync_cursor (id, desde) VALUES (1, '1970-01-01T00:00:00Z');
`
	for _, t := range tabelasSync {
		b += gatilhosSync(t.nome, t.pk)
	}
	return b
}

var tabelasSync = []struct{ nome, pk string }{
	{"kanban_columns", "nome"},
	{"tickets", "id"},
	{"cobrancas", "id"},
	{"movimentacoes", "id"},
	{"tentativas_contato", "id"},
	{"tickets_vinculos", "id"},
	{"comissoes", "id"},
}

// RepararGatilhosSync recria os gatilhos da fila que estiverem faltando.
func RepararGatilhosSync(ctx context.Context, d *sql.DB) error {
	for _, t := range tabelasSync {
		sqlGatilhos := strings.ReplaceAll(gatilhosSync(t.nome, t.pk), "CREATE TRIGGER ", "CREATE TRIGGER IF NOT EXISTS ")
		if _, err := d.ExecContext(ctx, sqlGatilhos); err != nil {
			return fmt.Errorf("gatilhos %s: %w", t.nome, err)
		}
	}
	return nil
}

func exprRegistro(pk, linha string) string {
	if pk == "id" {
		return fmt.Sprintf("CAST(%s.id AS TEXT)", linha)
	}
	return linha + "." + pk
}

// carimboLinha: para tabelas com updated_at, a fila é carimbada com o
// horário REAL da mudança da linha, não com o horário do enfileiramento.
// Com o carimbo "agora", uma linha só reenfileirada (ex.: o reajuste de
// posição dos outros cards ao mover um) subia com horário fresco e a cópia
// velha dela vencia na nuvem uma mudança mais recente — o card voltava de
// coluna. Nas demais tabelas (insert-only ou posição de colunas) o horário
// do gatilho continua sendo usado.
var carimboLinha = map[string]string{
	"tickets":   "COALESCE(NULLIF(NEW.updated_at,''), NEW.created_at, strftime('%Y-%m-%dT%H:%M:%fZ','now'))",
	"cobrancas": "COALESCE(NULLIF(NEW.updated_at,''), NEW.created_at, strftime('%Y-%m-%dT%H:%M:%fZ','now'))",
	"comissoes": "COALESCE(NULLIF(NEW.updated_at,''), NEW.created_at, strftime('%Y-%m-%dT%H:%M:%fZ','now'))",
}

// gatilhosSync grava uma linha na fila a cada inserção, alteração ou exclusão.
// Enquanto sync_suprimir.ativo = 1 (aplicando o que veio da nuvem), não grava —
// senão o app reenviaria o que acabou de receber.
func gatilhosSync(tabela, pk string) string {
	novo := exprRegistro(pk, "NEW")
	velho := exprRegistro(pk, "OLD")
	carimbo, real := carimboLinha[tabela]
	if !real {
		carimbo = "strftime('%%Y-%%m-%%dT%%H:%%M:%%fZ','now')"
	}
	upsert := fmt.Sprintf(`
CREATE TRIGGER %s_sync_ins AFTER INSERT ON %s
WHEN (SELECT ativo FROM sync_suprimir WHERE id = 1) = 0
BEGIN
    INSERT INTO sync_fila (tabela, registro_id, op, atualizado_em, versao, tentativas, proxima_em)
    VALUES ('%s', %s, 'upsert', %s,
        COALESCE((SELECT versao FROM sync_estado WHERE tabela = '%s' AND registro_id = %s), 0) + 1,
        0, strftime('%%Y-%%m-%%dT%%H:%%M:%%fZ','now'))
    ON CONFLICT(tabela, registro_id) DO UPDATE SET
        op = 'upsert',
        atualizado_em = excluded.atualizado_em,
        versao = CASE WHEN sync_fila.versao >= excluded.versao THEN sync_fila.versao + 1 ELSE excluded.versao END,
        tentativas = 0,
        proxima_em = excluded.proxima_em;
END;

CREATE TRIGGER %s_sync_upd AFTER UPDATE ON %s
WHEN (SELECT ativo FROM sync_suprimir WHERE id = 1) = 0
BEGIN
    INSERT INTO sync_fila (tabela, registro_id, op, atualizado_em, versao, tentativas, proxima_em)
    VALUES ('%s', %s, 'upsert', %s,
        COALESCE((SELECT versao FROM sync_estado WHERE tabela = '%s' AND registro_id = %s), 0) + 1,
        0, strftime('%%Y-%%m-%%dT%%H:%%M:%%fZ','now'))
    ON CONFLICT(tabela, registro_id) DO UPDATE SET
        op = 'upsert',
        atualizado_em = excluded.atualizado_em,
        versao = CASE WHEN sync_fila.versao >= excluded.versao THEN sync_fila.versao + 1 ELSE excluded.versao END,
        tentativas = 0,
        proxima_em = excluded.proxima_em;
END;

CREATE TRIGGER %s_sync_del BEFORE DELETE ON %s
WHEN (SELECT ativo FROM sync_suprimir WHERE id = 1) = 0
BEGIN
    INSERT INTO sync_fila (tabela, registro_id, op, atualizado_em, versao, tentativas, proxima_em)
    VALUES ('%s', %s, 'delete', strftime('%%Y-%%m-%%dT%%H:%%M:%%fZ','now'),
        COALESCE((SELECT versao FROM sync_fila WHERE tabela = '%s' AND registro_id = %s),
                 (SELECT versao FROM sync_estado WHERE tabela = '%s' AND registro_id = %s), 0) + 1,
        0, strftime('%%Y-%%m-%%dT%%H:%%M:%%fZ','now'))
    ON CONFLICT(tabela, registro_id) DO UPDATE SET
        op = 'delete',
        atualizado_em = excluded.atualizado_em,
        versao = CASE WHEN sync_fila.versao >= excluded.versao THEN sync_fila.versao + 1 ELSE excluded.versao END,
        tentativas = 0,
        proxima_em = excluded.proxima_em;
END;
`,
		tabela, tabela,
		tabela, novo, carimbo, tabela, novo,
		tabela, tabela,
		tabela, novo, carimbo, tabela, novo,
		tabela, tabela,
		tabela, velho, tabela, velho, tabela, velho,
	)
	if tabela == "kanban_columns" {
		upsert += `
CREATE TRIGGER kanban_columns_sync_rename AFTER UPDATE ON kanban_columns
WHEN (SELECT ativo FROM sync_suprimir WHERE id = 1) = 0 AND OLD.nome <> NEW.nome
BEGIN
    INSERT INTO sync_fila (tabela, registro_id, op, atualizado_em, versao, tentativas, proxima_em)
    VALUES ('kanban_columns', OLD.nome, 'delete', strftime('%Y-%m-%dT%H:%M:%fZ','now'),
        COALESCE((SELECT versao FROM sync_fila WHERE tabela = 'kanban_columns' AND registro_id = OLD.nome),
                 (SELECT versao FROM sync_estado WHERE tabela = 'kanban_columns' AND registro_id = OLD.nome), 0) + 1,
        0, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
    ON CONFLICT(tabela, registro_id) DO UPDATE SET
        op = 'delete',
        atualizado_em = excluded.atualizado_em,
        versao = CASE WHEN sync_fila.versao >= excluded.versao THEN sync_fila.versao + 1 ELSE excluded.versao END,
        tentativas = 0,
        proxima_em = excluded.proxima_em;
END;
`
	}
	return upsert
}
