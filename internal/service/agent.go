package service

import (
	"context"
	"fmt"
	"strings"
)

// Agente de chat: o Groq recebe ferramentas (function calling) e age no
// app — busca clientes do 360, adiciona cards, cria colunas, puxa dados.
// A execução das ferramentas fica no ToolExecutor (implementado em main),
// para este pacote não depender do banco.

const MaxIteracoesAgente = 6

const systemAgente = `Você é o assistente do app Kanban de Cobrança da Rota Smart, um app desktop local. ` +
	`Você age de verdade no app usando as ferramentas disponíveis: buscar clientes no portal 360, adicionar clientes ao Kanban, criar colunas novas, mover cards, consultar financeiro e gerar propostas, além de atuar em tickets do helpdesk do 360. ` +
	`Sobre tickets do helpdesk você pode: ler o detalhe (detalhe_ticket_360), abrir o ticket no navegador na aba do 360 que o usuário já tem aberta (abrir_ticket_360), registrar a sua resposta no ticket (responder_ticket_360) e alterar o status (alterar_status_ticket_360). ` +
	`Quando o usuário pedir para melhorar/aprimorar o texto de um ticket, reescreva com redação profissional, clara e cordial — mantendo todos os dados técnicos, códigos, valores e fatos originais — e só envie com responder_ticket_360 se o usuário pediu explicitamente para enviar; se ele pediu para "adicionar X e enviar", junte X ao texto melhorado e envie de uma vez. ` +
	`Para alterar status, use o nome do status exatamente como o detalhe_ticket_360 mostra (campo Status); se não souber os valores válidos, leia o detalhe do ticket antes. ` +
	`Regras: (1) prefira executar a ação pedida em vez de só descrever; (2) ao adicionar um cliente, use a coluna que o usuário indicou (padrão: "Fazer contato"); (3) nunca invente códigos service, nomes ou valores — consulte com as ferramentas; (4) responda em português do Brasil, curto e direto, confirmando o que fez e citando os dados que obteve.`

// Ferramentas disponíveis para o agente (formato tools da API OpenAI).
func AgentTools() []map[string]any {
	tool := func(nome, desc string, props map[string]any, obrigatorios []string) map[string]any {
		params := map[string]any{"type": "object", "properties": props}
		if len(obrigatorios) > 0 {
			params["required"] = obrigatorios
		}
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        nome,
				"description": desc,
				"parameters":  params,
			},
		}
	}
	return []map[string]any{
		tool("buscar_cliente_360",
			"Busca clientes no portal 360 por código service, nome, CPF/CNPJ. Devolve códigos e nomes.",
			map[string]any{"query": map[string]any{"type": "string", "description": "Código ou nome (ou parte) a procurar"}}, []string{"query"}),
		tool("importar_cliente",
			"Cria um card no Kanban com os dados oficiais do 360 (nome, telefone, endereço).",
			map[string]any{
				"codService": map[string]any{"type": "string", "description": "Código service do cliente"},
				"coluna":     map[string]any{"type": "string", "description": "Coluna de destino do card"},
			}, []string{"codService", "coluna"}),
		tool("listar_colunas",
			"Lista as colunas atuais do Kanban.",
			map[string]any{}, nil),
		tool("criar_coluna",
			"Cria uma coluna nova no Kanban (máx. 40 caracteres).",
			map[string]any{"nome": map[string]any{"type": "string"}}, []string{"nome"}),
		tool("financeiro_cliente",
			"Consulta os dados do cliente no 360 pelo código service: financeiro (saldo devedor, faturas atrasadas com multa/mora), ordens de serviço (OS) ativas, produtos/equipamentos cadastrados (alarme, câmera, sensores, controles, modelos) e atendimentos recentes (Octadesk).",
			map[string]any{"codService": map[string]any{"type": "string"}}, []string{"codService"}),
		tool("detalhe_ticket_360",
			"Busca o ticket do helpdesk 360 pelo número: devolve título, status, cliente, descrição e todas as interações (o que foi dito e feito, inclusive o que o técnico registrou).",
			map[string]any{"numero": map[string]any{"type": "string", "description": "Número do ticket (ex.: 67220)"}}, []string{"numero"}),
		tool("abrir_ticket_360",
			"Abre o ticket do helpdesk 360 no navegador, na aba que o usuário já tem aberta no portal 360 (a aba navega para o ticket).",
			map[string]any{"numero": map[string]any{"type": "string", "description": "Número do ticket (ex.: 67220)"}}, []string{"numero"}),
		tool("responder_ticket_360",
			"Registra a sua resposta (texto final, já redigido e revisado) como interação pública no ticket do helpdesk 360. Só chame quando o usuário pedir explicitamente para enviar/responder.",
			map[string]any{
				"numero": map[string]any{"type": "string", "description": "Número do ticket"},
				"texto":  map[string]any{"type": "string", "description": "Texto completo da resposta a publicar"},
			}, []string{"numero", "texto"}),
		tool("alterar_status_ticket_360",
			"Altera o status do ticket do helpdesk 360. Use o nome do status exatamente como detalhe_ticket_360 mostra no campo Status.",
			map[string]any{
				"numero": map[string]any{"type": "string", "description": "Número do ticket"},
				"status": map[string]any{"type": "string", "description": "Nome exato do status (como no detalhe do ticket)"},
			}, []string{"numero", "status"}),
		tool("listar_tickets",
			"Lista os cards do Kanban (id, nome, coluna), opcionalmente de uma coluna só.",
			map[string]any{"coluna": map[string]any{"type": "string", "description": "Filtra por coluna (opcional)"}}, nil),
		tool("mover_card",
			"Move um card para outra coluna (vai para o fim da coluna). Mover para a coluna 'Cobrança' dispara o fluxo automático.",
			map[string]any{
				"ticketId": map[string]any{"type": "number"},
				"coluna":   map[string]any{"type": "string"},
			}, []string{"ticketId", "coluna"}),
		tool("avaliar_cliente",
			"Avalia o cliente de um card: renegociação anterior, fidelidade/tempo de casa, elegibilidade a cortesia.",
			map[string]any{"ticketId": map[string]any{"type": "number"}}, []string{"ticketId"}),
		tool("gerar_proposta",
			"Gera a proposta de renegociação do card pelo template padrão (usa o acordo ativo; sem acordo, usa o saldo do 360 em 12x).",
			map[string]any{"ticketId": map[string]any{"type": "number"}}, []string{"ticketId"}),
		tool("listar_tabela_orcamento",
			"Lista a planilha-calculadora comercial: tipos de sistema, itens com preços (instalação/mensal) e faixas de mensalidade. Use antes de gerar um orçamento.",
			map[string]any{}, nil),
		tool("gerar_orcamento",
			"Gera o orçamento comercial calculando a planilha: escolha o tipo de sistema e a quantidade de cada item (câmeras, sensores, controles…), e devolve o texto da proposta com taxa de instalação e mensalidade.",
			map[string]any{
				"tipoSistema": map[string]any{"type": "string", "description": "Aba da planilha (ex.: ALARME, CAMERAS WIFI, CAMERAS DVR, RASTREAMENTO)"},
				"itens":       map[string]any{"type": "object", "description": "Mapa nome do item → quantidade (ex.: {\"Câmera WIFI Interna\": 3})"},
				"ticketId":    map[string]any{"type": "number", "description": "Card do Kanban para personalizar com o nome do cliente (opcional)"},
			}, []string{"tipoSistema", "itens"}),
	}
}

// AcaoDoAgente registra uma ferramenta executada, para exibir ao usuário.
type AcaoDoAgente struct {
	Ferramenta string `json:"ferramenta"`
	Resumo     string `json:"resumo"`
}

// ChatResultado é a resposta do agente ao frontend.
type ChatResultado struct {
	Resposta string         `json:"resposta"`
	Acoes    []AcaoDoAgente `json:"acoes"`
}

// ToolExecutor executa uma ferramenta pelo nome com argumentos JSON.
// Devolve (resultadoParaIA, resumoParaUsuario, erro).
type ToolExecutor interface {
	ExecutarAgente(ctx context.Context, ferramenta, argsJSON string) (string, string, error)
}

// MensagemDeChat é uma mensagem do histórico vinda do frontend.
type MensagemDeChat struct {
	Papel    string `json:"papel"` // "user" | "assistant"
	Conteudo string `json:"conteudo"`
}

// Conversar roda o loop de function calling: mensagem do usuário → chamadas
// de ferramenta (até MaxIteracoesAgente rodadas) → resposta final.
// progresso é chamado a cada ferramenta executada (para a UI).
func (g *GroqCliente) Conversar(ctx context.Context, historico []MensagemDeChat, mensagem string, exec ToolExecutor, progresso func(AcaoDoAgente)) (*ChatResultado, error) {
	if g.chave() == "" {
		return nil, ErrGroqSemChave
	}
	msgs := []groqMensagem{{Role: "system", Content: systemAgente}}
	for _, m := range historico {
		if m.Papel != "user" && m.Papel != "assistant" {
			continue
		}
		msgs = append(msgs, groqMensagem{Role: m.Papel, Content: m.Conteudo})
	}
	msgs = append(msgs, groqMensagem{Role: "user", Content: mensagem})

	acoes := []AcaoDoAgente{}
	for rodada := 0; rodada < MaxIteracoesAgente; rodada++ {
		resp, err := g.chatComTools(ctx, msgs)
		if err != nil {
			return nil, err
		}
		if len(resp.ToolCalls) == 0 {
			return &ChatResultado{Resposta: strings.TrimSpace(resp.Content), Acoes: acoes}, nil
		}
		msgs = append(msgs, groqMensagem{Role: "assistant", Content: resp.Content, ToolCalls: resp.ToolCalls})
		for _, tc := range resp.ToolCalls {
			resultado, resumo, err := exec.ExecutarAgente(ctx, tc.Function.Name, tc.Function.Arguments)
			if err != nil {
				resultado = "ERRO: " + err.Error()
				resumo = tc.Function.Name + " falhou: " + err.Error()
			}
			if progresso != nil {
				progresso(AcaoDoAgente{Ferramenta: tc.Function.Name, Resumo: resumo})
			}
			acoes = append(acoes, AcaoDoAgente{Ferramenta: tc.Function.Name, Resumo: resumo})
			argsLimitados := tc.Function.Arguments
			if len(argsLimitados) > 200 {
				argsLimitados = argsLimitados[:200]
			}
			msgs = append(msgs, groqMensagem{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    fmt.Sprintf("%s(%s) => %s", tc.Function.Name, argsLimitados, limitar(resultado, 3000)),
			})
		}
	}
	// Esgotou as rodadas: pede o resumo final sem ferramentas.
	msgs = append(msgs, groqMensagem{Role: "user", Content: "Resuma o que foi feito sem usar mais ferramentas."})
	resp, err := g.chatSemTools(ctx, msgs)
	if err != nil {
		return nil, err
	}
	return &ChatResultado{Resposta: strings.TrimSpace(resp), Acoes: acoes}, nil
}

func limitar(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

type groqToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
