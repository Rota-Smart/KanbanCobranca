package db

import (
	"database/sql"
	"fmt"
)

// Migrações versionadas: novas alterações entram como novos itens,
// nunca editando os anteriores.
var migrations = []string{
	migration001,
	migration002,
	migration003,
	migration004,
	migration005,
	migration006,
	migration007,
}

// migration007: padrão permanente "proposta de renegociação" — o formato
// obrigatório das propostas de valores em atraso (pedido do usuário). INSERT
// OR IGNORE: quem já reensinou o padrão pelo chat mantém a própria versão.
const migration007 = `
INSERT OR IGNORE INTO padroes (nome, conteudo) VALUES
    ('proposta de renegociação',
    'Ao montar uma proposta de renegociação de valores em atraso, use SEMPRE este formato (substitua os valores pelos reais do cliente):
"Bom dia, {primeiro nome}! Tudo bem?

Para adiantar o assunto do áudio que enviei, já deixo a proposta aqui para você avaliar:

Referente ao total em aberto, contando com o mês de {mês corrente} (R$ {total em aberto, incluindo a mensalidade do mês corrente}):
• Entrada: R$ {entrada}
• Parcelamento: {n}x de R$ {valor da parcela}

Nesses meses, a parcela ficará em R$ {mensalidade + parcela da renegociação}, retornando aos R$ {mensalidade normal} normais depois."
Ajuste a saudação à hora do dia (Bom dia/Boa tarde/Boa noite); se não houver entrada, omita a linha dela; deixe "Parcelamento" em negrito.');
`

// migration006: vínculo do card do Kanban com um ticket do helpdesk 360 —
// guardamos o uuid (para abrir/responder pela API) e o número exibido.
const migration006 = `
ALTER TABLE tickets ADD COLUMN ticket360_id TEXT NOT NULL DEFAULT '';
ALTER TABLE tickets ADD COLUMN ticket360_numero TEXT NOT NULL DEFAULT '';
`

// migration005: padrões do assistente — instruções permanentes que o usuário
// ensina pelo chat (ex.: estilo de escrita de tickets) e que entram no system
// prompt de toda conversa.
const migration005 = `
CREATE TABLE padroes (
    nome       TEXT PRIMARY KEY,
    conteudo   TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);

INSERT OR IGNORE INTO padroes (nome, conteudo) VALUES
    ('estilo de tickets',
    'Ao melhorar/responder tickets de atendimento, use registro formal e impessoal. Estrutura: 1) parágrafo inicial relatando o contato (ex.: "Em contato telefônico com o cliente, foi confirmada...") e os fatos principais; 2) seção "Status atual:" com bullets; 3) seção "Próximos passos:" com bullets; 4) fechamento do tipo "Permanecemos à disposição e seguimos acompanhando o caso até a normalização definitiva.". Nunca invente dados: use apenas nomes, datas e informações presentes no ticket.');
`

// migration002: integração Rota 360 — cache de clientes, parâmetros de
// negociação e configurações (chave Groq, modelo, sessão do site).
const migration002 = `
CREATE TABLE clientes_360 (
    cod_service    TEXT PRIMARY KEY,
    nome           TEXT NOT NULL,
    cnpj           TEXT,
    telefone       TEXT,
    endereco       TEXT,
    unidade        TEXT,
    saldo_cents    INTEGER NOT NULL DEFAULT 0,
    faturas_json   TEXT,
    score_json     TEXT,
    historico_json TEXT,
    synced_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);

CREATE TABLE parametros_negociacao (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    fonte        TEXT NOT NULL CHECK (fonte IN ('texto','arquivo')),
    nome_arquivo TEXT,
    conteudo     TEXT NOT NULL,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);

CREATE TABLE config (
    chave   TEXT PRIMARY KEY,
    valor   TEXT NOT NULL
);
`

// migration003: colunas dinâmicas do Kanban — o agente de chat pode criar
// novas. A seed replica literalmente service.ColunasValidas (a lista estática
// permanece só como fallback de validação).
const migration003 = `
CREATE TABLE kanban_columns (
    nome       TEXT PRIMARY KEY,
    posicao    INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
INSERT OR IGNORE INTO kanban_columns (nome, posicao) VALUES
    ('Fazer contato', 0),
    ('Contato feito', 1),
    ('Negociando', 2),
    ('Cobrança', 3),
    ('Proposta enviada', 4),
    ('Aguardando resposta', 5),
    ('Acordo aceito', 6),
    ('Pagando', 7),
    ('Quitado', 8),
    ('Inadimplente', 9),
    ('Concluido', 10);
`

// migration004: chats persistentes do assistente Groq — várias conversas,
// cada uma com seu histórico de mensagens.
const migration004 = `
CREATE TABLE chats (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    titulo     TEXT NOT NULL DEFAULT 'Nova conversa',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);

CREATE TABLE chat_mensagens (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id  INTEGER NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    papel    TEXT NOT NULL CHECK (papel IN ('user','assistant')),
    conteudo TEXT NOT NULL,
    acoes_json TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_chat_mensagens_chat ON chat_mensagens(chat_id, id);
`

const migration001 = `
CREATE TABLE tickets (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    nome_cliente  TEXT    NOT NULL,
    tipo_ticket   TEXT    NOT NULL,
    numero_ticket TEXT,
    cod_service   TEXT,
    endereco      TEXT,
    telefone      TEXT,
    coluna_atual  TEXT    NOT NULL DEFAULT 'Fazer contato',
    posicao       INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    updated_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_tickets_coluna_pos ON tickets(coluna_atual, posicao);

CREATE TABLE cobrancas (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    ticket_id             INTEGER NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    data_contrato         TEXT,
    data_vencimento       TEXT,
    total_aberto_cents    INTEGER NOT NULL CHECK (total_aberto_cents > 0),
    entrada_cents         INTEGER NOT NULL DEFAULT 0 CHECK (entrada_cents >= 0),
    numero_vezes          INTEGER NOT NULL DEFAULT 1 CHECK (numero_vezes >= 1),
    parcela_base_cents    INTEGER NOT NULL DEFAULT 0 CHECK (parcela_base_cents >= 0),
    parcelado_cents       INTEGER NOT NULL,
    ultima_parcela_cents  INTEGER NOT NULL,
    final_cents           INTEGER NOT NULL,
    status                TEXT    NOT NULL DEFAULT 'proposta',
    ativa                 INTEGER NOT NULL DEFAULT 1 CHECK (ativa IN (0,1)),
    versao_regra          INTEGER NOT NULL DEFAULT 1,
    created_at            TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    updated_at            TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    CHECK (entrada_cents <= total_aberto_cents)
);
CREATE INDEX idx_cobrancas_ticket ON cobrancas(ticket_id);
CREATE UNIQUE INDEX uq_cobranca_ativa ON cobrancas(ticket_id) WHERE ativa = 1;

CREATE TABLE movimentacoes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ticket_id   INTEGER NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    de_coluna   TEXT,
    para_coluna TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
`

func migrate(db *sql.DB) error {
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return fmt.Errorf("ler user_version: %w", err)
	}
	for i := v; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migração %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
