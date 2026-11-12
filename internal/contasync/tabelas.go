package contasync

import (
	"fmt"
	"strconv"
)

type tipoCampo int

const (
	campoTexto tipoCampo = iota
	campoInt
	campoBool
	campoTempo
)

type campo struct {
	nome string
	tipo tipoCampo
}

type specTabela struct {
	nome         string
	pk           string
	pkInt        bool
	campos       []campo
	carimboLocal string // expressão SQL do horário da linha, usada só na primeira subida
}

// ordemUpsert respeita chave estrangeira: coluna e card antes de acordo, visita e vínculo.
var ordemUpsert = []string{
	"kanban_columns",
	"tickets",
	"cobrancas",
	"movimentacoes",
	"tentativas_contato",
	"tickets_vinculos",
	"comissoes",
}

var tabelas = map[string]specTabela{
	"kanban_columns": {
		nome: "kanban_columns", pk: "nome",
		carimboLocal: "COALESCE(NULLIF(created_at,''), '1970-01-01T00:00:00Z')",
		campos: []campo{
			{"posicao", campoInt},
			{"created_at", campoTempo},
		},
	},
	"tickets": {
		nome: "tickets", pk: "id", pkInt: true,
		carimboLocal: "COALESCE(NULLIF(updated_at,''), NULLIF(created_at,''), '1970-01-01T00:00:00Z')",
		campos: []campo{
			{"nome_cliente", campoTexto},
			{"tipo_ticket", campoTexto},
			{"numero_ticket", campoTexto},
			{"cod_service", campoTexto},
			{"endereco", campoTexto},
			{"telefone", campoTexto},
			{"coluna_atual", campoTexto},
			{"posicao", campoInt},
			{"ticket360_id", campoTexto},
			{"ticket360_numero", campoTexto},
			{"observacoes", campoTexto},
			{"visita_data", campoTexto},
			{"visita_turno", campoTexto},
			{"visita_hora", campoTexto},
			{"visita_endereco", campoTexto},
			{"visita_realizada_em", campoTexto},
			{"visita_origem", campoTexto},
			{"created_at", campoTempo},
			{"updated_at", campoTempo},
		},
	},
	"cobrancas": {
		nome: "cobrancas", pk: "id", pkInt: true,
		carimboLocal: "COALESCE(NULLIF(updated_at,''), NULLIF(created_at,''), '1970-01-01T00:00:00Z')",
		campos: []campo{
			{"ticket_id", campoInt},
			{"data_contrato", campoTexto},
			{"data_vencimento", campoTexto},
			{"total_aberto_cents", campoInt},
			{"entrada_cents", campoInt},
			{"numero_vezes", campoInt},
			{"parcela_base_cents", campoInt},
			{"parcelado_cents", campoInt},
			{"ultima_parcela_cents", campoInt},
			{"final_cents", campoInt},
			{"status", campoTexto},
			{"ativa", campoBool},
			{"versao_regra", campoInt},
			{"created_at", campoTempo},
			{"updated_at", campoTempo},
		},
	},
	"movimentacoes": {
		nome: "movimentacoes", pk: "id", pkInt: true,
		carimboLocal: "COALESCE(NULLIF(created_at,''), '1970-01-01T00:00:00Z')",
		campos: []campo{
			{"ticket_id", campoInt},
			{"de_coluna", campoTexto},
			{"para_coluna", campoTexto},
			{"created_at", campoTempo},
		},
	},
	"tentativas_contato": {
		nome: "tentativas_contato", pk: "id", pkInt: true,
		carimboLocal: "COALESCE(NULLIF(created_at,''), '1970-01-01T00:00:00Z')",
		campos: []campo{
			{"ticket_id", campoInt},
			{"canal", campoTexto},
			{"nota", campoTexto},
			{"created_at", campoTempo},
		},
	},
	"tickets_vinculos": {
		nome: "tickets_vinculos", pk: "id", pkInt: true,
		carimboLocal: "COALESCE(NULLIF(criado_em,''), '1970-01-01T00:00:00Z')",
		campos: []campo{
			{"ticket_id", campoInt},
			{"ticket360_id", campoTexto},
			{"ticket360_numero", campoTexto},
			{"assunto", campoTexto},
			{"criado_em", campoTempo},
		},
	},
	"comissoes": {
		nome: "comissoes", pk: "id", pkInt: true,
		carimboLocal: "COALESCE(NULLIF(updated_at,''), NULLIF(created_at,''), '1970-01-01T00:00:00Z')",
		campos: []campo{
			{"ticket_id", campoInt},
			{"valor_servico_cents", campoInt},
			{"valor_parcela_cents", campoInt},
			{"comissao_cents", campoInt},
			{"observacoes", campoTexto},
			{"data_registro", campoTexto},
			{"recebido", campoBool},
			{"recebido_em", campoTexto},
			{"created_at", campoTempo},
			{"updated_at", campoTempo},
		},
	},
}

func specDe(tabela string) (specTabela, bool) {
	s, ok := tabelas[tabela]
	return s, ok
}

func idArg(spec specTabela, id string) (any, error) {
	if !spec.pkInt {
		if id == "" {
			return nil, fmt.Errorf("id vazio")
		}
		return id, nil
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return nil, fmt.Errorf("id inválido")
	}
	return n, nil
}

func comoInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	case bool:
		if n {
			return 1
		}
		return 0
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	default:
		return 0
	}
}

func comoTexto(v any) string {
	switch s := v.(type) {
	case nil:
		return ""
	case string:
		return s
	case float64:
		return strconv.FormatInt(int64(s), 10)
	case int64:
		return strconv.FormatInt(s, 10)
	case bool:
		if s {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprint(s)
	}
}

func valorColuna(c campo, v any, carimbo string) any {
	switch c.tipo {
	case campoInt, campoBool:
		return comoInt(v)
	case campoTempo:
		s := comoTexto(v)
		if s == "" {
			s = carimbo
		}
		return s
	default:
		return comoTexto(v)
	}
}
