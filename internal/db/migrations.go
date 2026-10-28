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
	migration008,
	migration009,
	migration010,
	migration011,
	migration012,
}

// migration012: estatísticas do assistente para a pré-busca aprender —
// para cada classe de consulta (financeiro, os, equipamentos...), quantas
// vezes o modelo acabou chamando cada ferramenta mesmo com o contexto
// injetado. Fatias chamadas com frequência (>= 3) passam a vir junto na
// pré-busca daquela classe, sem o usuário precisar pedir.
const migration012 = `
CREATE TABLE IF NOT EXISTS agente_stats (
    classe        TEXT NOT NULL,
    ferramenta    TEXT NOT NULL,
    contagem      INTEGER NOT NULL DEFAULT 0,
    atualizado_em TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    PRIMARY KEY (classe, ferramenta)
);
`

// migration011: agendamento de visita do card — data, turno, horário e
// endereço (sugerido do cadastro do cliente no 360, editável) e a coluna
// "Visita Agendada" logo após "Contato feito".
const migration011 = `
ALTER TABLE tickets ADD COLUMN visita_data     TEXT NOT NULL DEFAULT '';
ALTER TABLE tickets ADD COLUMN visita_turno    TEXT NOT NULL DEFAULT '';
ALTER TABLE tickets ADD COLUMN visita_hora     TEXT NOT NULL DEFAULT '';
ALTER TABLE tickets ADD COLUMN visita_endereco TEXT NOT NULL DEFAULT '';
UPDATE kanban_columns SET posicao = posicao + 1 WHERE posicao >= 2;
INSERT OR IGNORE INTO kanban_columns (nome, posicao) VALUES ('Visita Agendada', 2);
`

// migration010: observações internas do card (não vão para o cliente) e
// vínculos múltiplos de tickets do 360 — um card pode ter vários tickets,
// cada um com o assunto para exibir na aba Cliente. Importa os vínculos
// únicos que já existiam nas colunas legadas do tickets.
const migration010 = `
ALTER TABLE tickets ADD COLUMN observacoes TEXT NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS tickets_vinculos (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    ticket_id        INTEGER NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    ticket360_id     TEXT    NOT NULL UNIQUE,
    ticket360_numero TEXT    NOT NULL DEFAULT '',
    assunto          TEXT    NOT NULL DEFAULT '',
    criado_em        TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
INSERT OR IGNORE INTO tickets_vinculos (ticket_id, ticket360_id, ticket360_numero, assunto)
    SELECT id, ticket360_id, ticket360_numero, '' FROM tickets WHERE ticket360_id != '';
`

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

// migration008: adapta o padrão "proposta de renegociação" às condições
// realmente praticadas (Política de Reversão de Cancelamento e Bonificações
// v7, conferidas com a planilha de cobrança do setor) — o formato da mensagem
// continua o mesmo; entra o bloco de regras que o assistente deve obedecer.
// UPDATE explícito (pedido do usuário): a versão ensinada pelo chat é
// substituída pela canônica.
const migration008 = `
UPDATE padroes SET conteudo =
    'Ao montar uma proposta de renegociação de valores em atraso, use SEMPRE este formato (substitua os valores pelos reais do cliente):
"Bom dia, {primeiro nome}! Tudo bem?

Para adiantar o assunto do áudio que enviei, já deixo a proposta aqui para você avaliar:

Referente ao total em aberto, contando com o mês de {mês corrente} (R$ {total em aberto, incluindo a mensalidade do mês corrente}):
• Entrada: R$ {entrada}
• Parcelamento: {n}x de R$ {valor da parcela}

Nesses meses, a parcela ficará em R$ {mensalidade + parcela da renegociação}, retornando aos R$ {mensalidade normal} normais depois."
Ajuste a saudação à hora do dia (Bom dia/Boa tarde/Boa noite); se não houver entrada, omita a linha dela; deixe "Parcelamento" em negrito.

CONDIÇÕES VIGENTES (Política de Reversão de Cancelamento e Bonificações v7 — obedeça em qualquer oferta/negociação):
• Elegibilidade: cliente com 01 ano (ou mais) de contrato e adimplente; bonificação limitada a 01 ocorrência a cada 12 meses; ofertas não cumulativas entre si (única exceção: a cortesia).
• Reajuste: pode-se ofertar 50% de desconto ou isenção total no valor do reajuste.
• Desconto/bonificação por período: máx. 20% na mensalidade — até 03 meses (pedidos comuns) ou até 06 meses (reversão de cancelamento, ticket acima de R$ 120).
• Cortesia: até 03 mensalidades de cortesia, que podem ser diluídas em até 12x; é cumulativa com as demais ofertas e exige renovação da vigência contratual por no mínimo 12 meses.
• Câmera Wifi grátis (IM3/IM5/IM7, somente se o cliente não tiver câmera grátis no contrato) ou redução de até R$ 40/mês na Câmera Wifi paga — ambos exigem renovação de 12 meses.
• Troca de serviço sem custos rescisórios exige renovação de 24 ou 36 meses.
• Todo acordo deve ser formalizado no ticket; qualquer alteração contratual depende de contrato assinado pelo cliente.'
WHERE nome = 'proposta de renegociação';
`

// migration009: cache dos contratos do cliente (Consulta Clientes → aba
// Contratos no 360) — fonte da mensalidade exata, dia de vencimento e
// vigência usados na proposta de renegociação.
const migration009 = `
ALTER TABLE clientes_360 ADD COLUMN contratos_json TEXT NOT NULL DEFAULT '';
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
