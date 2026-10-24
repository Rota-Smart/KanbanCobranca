package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Agente de chat: o Groq recebe ferramentas (function calling) e age no
// app — busca clientes do 360, adiciona cards, cria colunas, puxa dados.
// A execução das ferramentas fica no ToolExecutor (implementado em main),
// para este pacote não depender do banco.

// 4 rodadas cobrem as tarefas reais (ler ticket → responder → status); cada
// rodada extra reenvia todo o contexto e custa ~2-4k tokens do limite diário.
const MaxIteracoesAgente = 4

// System prompt enxuto: cada token daqui é reenviado em TODA rodada do loop
// (e em toda mensagem do usuário), então economia aqui multiplica.
const systemAgente = `Você é o assistente do app Kanban de Cobrança da Rota Smart (desktop local) e age via ferramentas: buscar/importar clientes do 360, cards e colunas do Kanban, financeiro, orçamentos e tickets do helpdesk (pesquisar, ler, abrir, responder, mudar status). ` +
	`Regras: execute a ação pedida em vez de só descrever; ao adicionar cliente use a coluna indicada (padrão "Fazer contato"); nunca invente códigos, nomes ou valores — consulte as ferramentas, e consulte só os dados que faltarem para a tarefa; para melhorar texto de ticket, reescreva com redação profissional mantendo todos os dados técnicos, códigos e valores, e só envie com responder_ticket_360 se pedido explicitamente; para status, use o nome exato do detalhe_ticket_360. ` +
	`Padrões: quando o usuário pedir para você manter um padrão daqui pra frente (ex.: "mantenha esse estilo nos próximos tickets", "sempre assine assim", "guarde essa regra"), destile o padrão em poucas frases e salve com salvar_padrao (nome curto, ex.: "estilo de tickets"); confirme o que salvou em uma linha. Se ele pedir para mudar um padrão existente, salve por cima com o mesmo nome; se pedir para esquecer, use apagar_padrao. ` +
	`Responda em português do Brasil, curto e direto, confirmando o que fez com os dados obtidos.`

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
			"Busca clientes no 360 por código, nome ou CPF/CNPJ.",
			map[string]any{"query": map[string]any{"type": "string"}}, []string{"query"}),
		tool("importar_cliente",
			"Cria card no Kanban com os dados oficiais do 360.",
			map[string]any{
				"codService": map[string]any{"type": "string"},
				"coluna":     map[string]any{"type": "string"},
			}, []string{"codService", "coluna"}),
		tool("listar_colunas",
			"Lista as colunas do Kanban.",
			map[string]any{}, nil),
		tool("criar_coluna",
			"Cria coluna nova no Kanban.",
			map[string]any{"nome": map[string]any{"type": "string"}}, []string{"nome"}),
		tool("reordenar_colunas",
			"Muda a ordem; 'ordem' = lista COMPLETA (liste antes com listar_colunas).",
			map[string]any{"ordem": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, []string{"ordem"}),
		tool("financeiro_cliente",
			"Saldo devedor e faturas atrasadas do cliente no 360.",
			map[string]any{"codService": map[string]any{"type": "string"}}, []string{"codService"}),
		tool("equipamentos_cliente",
			"Equipamentos do cliente no 360: alarme, câmera (modelo), sensores e produtos cadastrados.",
			map[string]any{"codService": map[string]any{"type": "string"}}, []string{"codService"}),
		tool("atendimentos_cliente",
			"OS ativas e atendimentos recentes (Octadesk) do cliente no 360.",
			map[string]any{"codService": map[string]any{"type": "string"}}, []string{"codService"}),
		tool("pesquisar_tickets",
			"Pesquisa tickets ABERTOS do 360 pelo número do ticket, código service do cliente ou nome do solicitante. Use para achar o número antes de detalhe/abrir/responder.",
			map[string]any{"query": map[string]any{"type": "string"}}, []string{"query"}),
		tool("detalhe_ticket_360",
			"Ticket do 360: título, status, cliente, descrição e interações.",
			map[string]any{"numero": map[string]any{"type": "string"}}, []string{"numero"}),
		tool("abrir_ticket_360",
			"Abre o ticket no navegador.",
			map[string]any{"numero": map[string]any{"type": "string"}}, []string{"numero"}),
		tool("responder_ticket_360",
			"Publica resposta no ticket. Só com pedido explícito do usuário.",
			map[string]any{
				"numero": map[string]any{"type": "string"},
				"texto":  map[string]any{"type": "string"},
			}, []string{"numero", "texto"}),
		tool("alterar_status_ticket_360",
			"Muda o status do ticket (nome exato do detalhe_ticket_360).",
			map[string]any{
				"numero": map[string]any{"type": "string"},
				"status": map[string]any{"type": "string"},
			}, []string{"numero", "status"}),
		tool("listar_tickets",
			"Lista os cards do Kanban.",
			map[string]any{"coluna": map[string]any{"type": "string"}}, nil),
		tool("mover_card",
			"Move o card; 'Cobrança' dispara o fluxo automático.",
			map[string]any{
				"ticketId": map[string]any{"type": "number"},
				"coluna":   map[string]any{"type": "string"},
			}, []string{"ticketId", "coluna"}),
		tool("vincular_ticket",
			"Vincula/desvincula ticket do 360 a um card (número, uuid ou URL).",
			map[string]any{
				"ticketId": map[string]any{"type": "number"},
				"numero":   map[string]any{"type": "string", "description": "número, uuid ou URL; vazio desvincula"},
			}, []string{"ticketId"}),
		tool("avaliar_cliente",
			"Avalia o cliente do card (renegociação, fidelidade, cortesia).",
			map[string]any{"ticketId": map[string]any{"type": "number"}}, []string{"ticketId"}),
		tool("gerar_proposta",
			"Gera a proposta de renegociação do card.",
			map[string]any{"ticketId": map[string]any{"type": "number"}}, []string{"ticketId"}),
		tool("listar_tabela_orcamento",
			"Tipos de sistema, itens e preços da planilha. Use antes de gerar_orcamento.",
			map[string]any{}, nil),
		tool("gerar_orcamento",
			"Gera orçamento: tipo de sistema + quantidades.",
			map[string]any{
				"tipoSistema": map[string]any{"type": "string"},
				"itens":       map[string]any{"type": "object"},
				"ticketId":    map[string]any{"type": "number"},
			}, []string{"tipoSistema", "itens"}),
		tool("salvar_padrao",
			"Salva padrão permanente do usuário (estilo, saudação, regra), destilado em poucas frases.",
			map[string]any{
				"nome":     map[string]any{"type": "string", "description": "nome curto, ex.: 'estilo de tickets'"},
				"conteudo": map[string]any{"type": "string", "description": "o padrão, para TODAS as conversas futuras"},
			}, []string{"nome", "conteudo"}),
		tool("listar_padroes",
			"Lista os padrões permanentes salvos.",
			map[string]any{}, nil),
		tool("apagar_padrao",
			"Apaga um padrão permanente pelo nome.",
			map[string]any{"nome": map[string]any{"type": "string"}}, []string{"nome"}),
	}
}

// SystemComPadroes monta o system prompt com os padrões salvos embutidos —
// eles valem para o loop com ferramentas e para o fast-path sem ferramentas.
func SystemComPadroes(padroes string) string {
	if strings.TrimSpace(padroes) == "" {
		return systemAgente
	}
	return systemAgente + "\n\nPadrões definidos pelo usuário — siga em TODA resposta:\n" + padroes
}

// Heurística do fast-path: responde sem o loop de ferramentas (1 requisição,
// sem os ~600 tokens de schemas). Cobre os dois usos que não precisam de
// ação: reescrita de texto colado na própria mensagem e perguntas sobre o app.
// \b do RE2 é ASCII (falha depois de acento), então as âncoras evitam terminá-
// la em palavra acentuada.
var (
	reVerbosReescrita = regexp.MustCompile(`(?i)(melhor\w*|reescrev\w*|aprimor\w*|corrig\w*|revis\w*|polir|reformul\w*|arrumar|ajustar)`)
	rePerguntaMeta    = regexp.MustCompile(`(?i)^\s*(como (você|vc|o assistente|funcion)|o que (você|vc) (faz|pode|é)|quem (é|te|lhe)|para que (você|vc)? ?serve|quais (ferramentas|comandos)|me ajuda a usar|o que sab\w* fazer)`)
	// Comandos de ação: se o usuário pediu uma dessas, o fast-path não serve.
	reAcaoComando = regexp.MustCompile(`(?i)(busque|buscar|procure|procurar|pesquis\w+|adicion\w+|import\w+|moviment\w+|mova|mover|crie|criar|reorden\w+|orden\w+|respond\w+|envi\w+|altere|alterar|mudar|consulte|consultar|gere|gerar|liste|listar|abra|abrir)`)
	// Substantivos de contexto: indicam que a pergunta depende de dados do app
	// (só descartam o fast-path de perguntas; não bloqueiam reescrita de
	// texto longo, onde costumam aparecer DENTRO do texto citado).
	reContextoApp = regexp.MustCompile(`(?i)(ticket|cliente|card|coluna|kanban|orçament|orcament|proposta|cobran|financeir|fatura|saldo)`)
)

// MensagemSemFerramentas diz se a mensagem pode ser respondida sem ferramentas:
// pergunta sobre o próprio app, ou reescrita de um texto longo colado nela
// (curto demais = provavelmente pede para ler ticket/cliente antes).
func MensagemSemFerramentas(m string) bool {
	if rePerguntaMeta.MatchString(m) && !reContextoApp.MatchString(m) {
		return true
	}
	return reVerbosReescrita.MatchString(m) && len([]rune(m)) >= 250 && !reAcaoComando.MatchString(m)
}

// Limites de contexto: o histórico inteiro é reenviado a cada rodada do loop,
// então cada mensagem velha custa N× o tamanho dela no dia.
const (
	MaxMensagensHistorico = 6
	MaxRunasMensagem      = 400
	MaxRunasToolResult    = 1500
	// A tabela de orçamento é a matéria-prima do gerar_orcamento: cortar no
	// meio (cap global) esconde itens que o modelo precisa nomear. Cap próprio,
	// maior — só ela o usa, e só quando o usuário pede orçamento.
	MaxRunasTabelaOrcamento = 2500
)

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
// padroes é o texto dos padrões salvos pelo usuário (vazio se não houver).
func (g *GroqCliente) Conversar(ctx context.Context, historico []MensagemDeChat, mensagem string, padroes string, exec ToolExecutor, progresso func(AcaoDoAgente)) (*ChatResultado, error) {
	if g.chave() == "" {
		return nil, ErrGroqSemChave
	}
	sistema := SystemComPadroes(padroes)
	// Fast-path: mensagem que não precisa de ação sai em 1 requisição, sem
	// os schemas das tools nem o loop.
	compacto := historicoCompacto(historico)
	if MensagemSemFerramentas(mensagem) {
		msgs := []groqMensagem{{Role: "system", Content: sistema}}
		for _, m := range compacto {
			msgs = append(msgs, groqMensagem{Role: m.Papel, Content: m.Conteudo})
		}
		msgs = append(msgs, groqMensagem{Role: "user", Content: mensagem})
		resp, err := g.chatSemTools(ctx, msgs)
		if err != nil {
			return nil, err
		}
		return &ChatResultado{Resposta: resp, Acoes: []AcaoDoAgente{}}, nil
	}

	msgs := []groqMensagem{{Role: "system", Content: sistema}}
	for _, m := range compacto {
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
			capResultado := MaxRunasToolResult
			if tc.Function.Name == "listar_tabela_orcamento" {
				capResultado = MaxRunasTabelaOrcamento
			}
			msgs = append(msgs, groqMensagem{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    fmt.Sprintf("%s(%s) => %s", tc.Function.Name, argsLimitados, limitarRunas(resultado, capResultado)),
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

// historicoCompacto devolve as últimas MaxMensagensHistorico mensagens do
// histórico, cada uma com no máximo MaxRunasMensagem runes (corte em rune,
// nunca em meio a caractere multibyte).
func historicoCompacto(historico []MensagemDeChat) []MensagemDeChat {
	var out []MensagemDeChat
	for _, m := range historico {
		if m.Papel != "user" && m.Papel != "assistant" {
			continue
		}
		if m.Conteudo == "" {
			continue
		}
		out = append(out, MensagemDeChat{Papel: m.Papel, Conteudo: limitarRunas(m.Conteudo, MaxRunasMensagem)})
	}
	if len(out) > MaxMensagensHistorico {
		out = out[len(out)-MaxMensagensHistorico:]
	}
	return out
}

// limitarRunas corta s em n runes (não em bytes), preservando acentos.
func limitarRunas(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
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
