package service

import (
	"regexp"
	"strings"
)

// Roteamento local de consultas: antes de gastar tokens com o loop de
// ferramentas, o backend classifica o que a mensagem pede, identifica o
// alvo (cliente/ticket) e pré-coleta os dados do 360 em UMA requisição
// (o dashboard360 já traz financeiro + OS + produtos + Octadesk juntos).
// A fatia escolhida entra no system prompt e as tools de leitura
// correspondentes saem do request — o modelo só monta a resposta.

type ClasseConsulta string

const (
	ConsultaNenhuma      ClasseConsulta = ""
	ConsultaFinanceiro   ClasseConsulta = "financeiro"
	ConsultaOS           ClasseConsulta = "os"
	ConsultaEquipamentos ClasseConsulta = "equipamentos"
	// ConsultaCliente: mais de uma fatia do cliente pedida na mesma mensagem
	// (ou pergunta genérica sobre o cliente) — traz as três.
	ConsultaCliente ClasseConsulta = "cliente"
	ConsultaTicket  ClasseConsulta = "ticket"
	// ConsultaBoard: pedido agregado sobre o quadro ("todos os clientes da
	// coluna X", "liste os cards em Cobrança") — o backend responde com dados
	// LOCAIS do banco, sem tocar no 360.
	ConsultaBoard ClasseConsulta = "board"

	// Classes de AÇÃO (não consulta): a mensagem pede uma operação e só
	// precisa das tools daquela operação — as outras 20+ saem do request
	// (cada schema de tool custa tokens em TODA rodada do loop). O conjunto
	// de cada classe é generoso de propósito: melhor mandar uma tool a mais
	// do que o modelo tentar chamar uma que não existe.
	AcaoCard      ClasseConsulta = "acao_card"
	AcaoTicket    ClasseConsulta = "acao_ticket"
	AcaoOrcamento ClasseConsulta = "acao_orcamento"
	AcaoComissao  ClasseConsulta = "acao_comissao"
	AcaoPadroes   ClasseConsulta = "acao_padroes"
	AcaoImportar  ClasseConsulta = "acao_importar"
)

// Regexes de ação: exigem verbo de operação + objeto da classe — mensagem
// ambígua (sem verbo claro) NÃO é classificada e vai com o conjunto completo
// de tools. Rodam SÓ depois das classes de consulta (uma pergunta de dados
// com "ticket" não pode virar ação).
var (
	// "salve esse padrão", "esqueça o padrão X", "quais são os meus padrões",
	// "mude o prompt da proposta".
	reAcaoPadroes = regexp.MustCompile(`(?i)\b(salv\w+|guard\w+|esque[çc]\w+|apag\w+|remov\w+|exclu\w+|delet\w+|elimin\w+|liste|listar|quais|veja|ver|mostr\w+|mud\w+|edit\w+)\b[^.?!]{0,30}\b(padr[ãaõo]+(?:es)?|prompt d[oa] proposta)\b`)
	// "registre a comissão", "comissão recebida do card 5", "quanto já recebi de comissão".
	reAcaoComissao = regexp.MustCompile(`(?i)\b(registr\w+|lan[çc]\w+|apont\w+|recebid[oa]|recebemos|recebi|quanto|liste|listar)\b[^.?!]*\bcomiss\w*|\bcomiss\w*`)
	// "gere um orçamento", "quanto fica a tabela de preços".
	reAcaoOrcamento = regexp.MustCompile(`(?i)\b(ger\w+|mont\w+|faz\w+|crie|criar|consult\w+|veja|ver|liste|listar|pre[çc]o)\b[^.?!]*\b(or[çc]ament\w*|tabela de pre[çc]os?)\b`)
	// "responda o ticket 123", "abra o ticket", "mude o status do ticket",
	// "feche o ticket 45".  (Pergunta de leitura cai na ConsultaTicket antes.)
	reAcaoTicket = regexp.MustCompile(`(?i)\b(respond\w+|abr\w+|fech\w+|mud\w+|alter\w+|troqu\w+|resolv\w+|encerr\w+|pesquis\w+|localiz\w+|encontre|veja|ver)\b[^.?!]*\btickets?\b|\btickets?\b[^.?!]*\b(respond\w+|abr\w+|fech\w+|status|resolv\w+|encerr\w+)\b`)
	// Verbos de ESCRITA sobre ticket — sem "responderam/mudou" (leitura no
	// passado). Usada para não deixar "mude o status do ticket 123" virar
	// consulta de leitura só porque tem número.
	reAcaoTicketForte = regexp.MustCompile(`(?i)\b(respond(?:er|a|e)\b|abr(?:a|ir|e)\b|fech(?:a|ar|e)\b|mud(?:a|ar|e)\b|alter(?:a|ar|e)\b|troqu(?:e|ar)\b|resolv(?:a|er|e)\b|encerr(?:a|ar|e)\b)[^.?!]{0,40}\btickets?\b`)
	// "mova o card do João", "apague o card 5", "anote no card", "agende uma
	// visita", "vincule o ticket 123 ao card", "verifique os tickets fechados".
	reAcaoCard = regexp.MustCompile(`(?i)\b(mov\w+|arrast\w+|pass\w+|lev\w+|mand\w+|mud\w+|coloc\w+|coloq\w+|ponh\w+|p[õo]e|deix\w+|mex\w+|apag\w+|exclu\w+|remov\w+|delet\w+|elimin\w+|anot\w+|observ\w+|vincul\w+|desvincul\w+|agend\w+|avalie|avaliar|ger\w+)\b[^.?!]*\b(card|visita|observa\w*|ticket fechado|proposta)\b|\b(card|visita)\b[^.?!]*\b(para|pro|pra|em|na|no|apag\w+|exclu\w+)\b`)
	// "adicione o cliente X", "importe o cod 1234", "cadastre o cliente".
	reAcaoImportar = regexp.MustCompile(`(?i)\b(adicione|adicionar|adc|importe|importar|cadastre|cadastrar|puxe|puxar|crie um card|criar um card)\b`)
)

// ferramentasDaAcao mapeia cada classe de ação às tools que ela pode usar —
// TODAS as outras saem do request. listar_tickets/listar_colunas entram nas
// classes de card/quadro porque o modelo quase sempre precisa conferir onde
// o card está (ou para onde pode ir) antes de agir.
var ferramentasDaAcao = map[ClasseConsulta][]string{
	AcaoCard: {"listar_tickets", "listar_colunas", "mover_card", "apagar_card",
		"observacao_card", "agendar_visita", "vincular_ticket", "verificar_tickets_fechados",
		"avaliar_cliente", "gerar_proposta"},
	AcaoTicket: {"pesquisar_tickets", "detalhe_ticket_360", "abrir_ticket_360",
		"responder_ticket_360", "alterar_status_ticket_360", "listar_tickets",
		"vincular_ticket", "verificar_tickets_fechados"},
	AcaoOrcamento: {"listar_tabela_orcamento", "gerar_orcamento", "buscar_cliente_360",
		"importar_cliente", "listar_colunas", "listar_tickets"},
	AcaoComissao: {"registrar_comissao", "comissao_recebida", "listar_comissoes",
		"listar_tickets", "listar_colunas"},
	AcaoPadroes:  {"salvar_padrao", "listar_padroes", "apagar_padrao", "editar_prompt_proposta"},
	AcaoImportar: {"buscar_cliente_360", "importar_cliente", "listar_colunas", "listar_tickets"},
}

// EhAcao diz se a classe é de ação (operação, não consulta de dados).
func EhAcao(c ClasseConsulta) bool {
	_, ok := ferramentasDaAcao[c]
	return ok
}

// FerramentasForaDaClasse devolve os NOMES das tools que saem do request
// para uma classe de ação (todas menos as da classe). Classe sem conjunto
// definido devolve nil — o request vai completo.
func FerramentasForaDaClasse(c ClasseConsulta) []string {
	permitidas := ferramentasDaAcao[c]
	if len(permitidas) == 0 {
		return nil
	}
	naClasse := map[string]bool{}
	for _, f := range permitidas {
		naClasse[f] = true
	}
	var out []string
	for _, t := range AgentTools() {
		nome := t["function"].(map[string]any)["name"].(string)
		if !naClasse[nome] {
			out = append(out, nome)
		}
	}
	return out
}

// Regexes de intenção. Acentos cobertos na mão (RE2 + \b é ASCII).
var (
	reConsultaFinanceiro = regexp.MustCompile(`(?i)(financeir|fatura|saldo|devend|d[ée]bito|boleto|mensalidade|inadimpl|encarg|multa|vencid|negocia|renegoc|proposta|acord|parcel)`)
	reConsultaOS         = regexp.MustCompile(`(?i)(\bos\b|ordens? de servi|atendimento|conclu[íi]d|cancelad|hist[óo]ric|quantas? os|t[ée]cnic[oa]s? foi)`)
	reConsultaEquip      = regexp.MustCompile(`(?i)(equipament|c[âa]mera|alarme|sensor|controle remoto|produto|instalad|modelo im|cftv)`)
	reConsultaTicket     = regexp.MustCompile(`(?i)(\bticket\b|intera[çc][õo]es?|o que (responderam|escreveram|falaram))`)
)

var reAlvoTicket = regexp.MustCompile(`(?:#|\bticket\s+)(\d{3,})`)
var reAlvoCod = regexp.MustCompile(`(?i)\b(?:c[óo]digo(?:\s+service)?|cod)\s*:?\s*(\d{2,})\b`)
var reAlvoNumero = regexp.MustCompile(`\b(\d{4,})\b`)
var reAlvoAspas = regexp.MustCompile(`["“'']([^"”'']{3,40})["”'']`)
var reAlvoNome = regexp.MustCompile(`(?i)(?:do cliente|do card|da cliente|cliente|card)\s+([A-Za-zÀ-ÿ][A-Za-zÀ-ÿ'’. ]{2,39})`)
var reCorteNome = regexp.MustCompile(`(?i)^(.+?)\s+(?:tem|est[áa]|possui|quant[oa]s?|estando|que|no|na|em|e)\b`)

// Board: agregação ("todos", "quais", "liste"...) sobre UMA coluna do quadro.
// Exige uma palavra de agregação ("quem", "cards" no plural entram também) E
// uma referência de lugar ("coluna X", "quadro", "em Cobrança") — ou o par
// agregação+cards ("liste os cards"), que vale para o quadro inteiro. Sem
// isso, qualquer mensagem com "card" viraria consulta de quadro.
var (
	reAgregBoard = regexp.MustCompile(`(?i)\b(todos|todas|quais|liste|lista|listar|verifi(que|car)|confer(a|ir)|confira|quant[oa]s?)\b`)
	reTemColuna  = regexp.MustCompile(`(?i)\bcoluna\b`)
	// Nome depois de "coluna"/"da coluna"/"na coluna"; o corte descarta o que
	// vem depois de conector ("coluna Cobrança e verifique...").
	reAlvoColuna = regexp.MustCompile(`(?i)\b(?:d[ao]?\s+)?coluna\s+([A-Za-zÀ-ÿ0-9][A-Za-zÀ-ÿ0-9'’.\- ]{1,39})`)
	reCorteCol   = regexp.MustCompile(`(?i)^(.+?)\s+(?:e|que|para|pra|com|tem|est[áa]|possui|de)\b`)

	// Coluna citada sem a palavra "coluna" ("quem está em Cobrança?",
	// "cards no Orçamento"): só em/na/no + inicial maiúscula — "do João" e
	// "em atraso" não podem virar coluna.
	reColunaSolta     = regexp.MustCompile(`(?i)\b(?:em|na|no)\s+[A-ZÂÉÍÓÚÃÕÇ]`)
	reAlvoColunaSolta = regexp.MustCompile(`\b(?:em|na|no)\s+([A-ZÂÉÍÓÚÃÕÇ][A-Za-zÀ-ÿ0-9'’.\- ]{1,39})`)

	reCardsPlural = regexp.MustCompile(`(?i)\bcards\b`)
	reQuemBoard   = regexp.MustCompile(`(?i)\bquem\b`)
	reQuadroBoard = regexp.MustCompile(`(?i)\b(quadro|kanban)\b`)
)

// ExtrairColunaBoard devolve o nome da coluna citado numa frase de board
// ("... da coluna Cobrança ...", "quem está em Ticket Fechado?"). Vazio quando
// nenhuma coluna é nomeada — o pedido então vale para o quadro inteiro.
func ExtrairColunaBoard(m string) string {
	nome := ""
	if mc := reAlvoColuna.FindStringSubmatch(m); mc != nil {
		nome = mc[1]
	} else if ms := reAlvoColunaSolta.FindStringSubmatch(m); ms != nil {
		nome = ms[1]
	} else {
		return ""
	}
	if corte := reCorteCol.FindStringSubmatch(nome); corte != nil {
		nome = corte[1]
	}
	nome = strings.TrimSpace(strings.TrimRight(nome, ".,;:!?"))
	if len([]rune(nome)) < 2 {
		return ""
	}
	return nome
}

// AlvoConsulta é o alvo identificado na mensagem.
type AlvoConsulta struct {
	Tipo  string // "ticket" | "cod" | "nome" | ""
	Valor string
}

// ClassificarConsulta devolve a(s) fatia(s) de dados que a mensagem pede.
// Vazia quando não é consulta de dados do 360 (ação, reescrita, meta...).
// Pedido agregado de board ("todos os clientes da coluna X", "quem tá em
// Cobrança?") vence: é dado LOCAL e um match simultâneo de financeiro
// ("...com renegociação") não pode reduzir o escopo a um cliente só.
func ClassificarConsulta(m string) ClasseConsulta {
	palavraBoard := reAgregBoard.MatchString(m) || reCardsPlural.MatchString(m) || reQuemBoard.MatchString(m)
	lugarBoard := reTemColuna.MatchString(m) || reQuadroBoard.MatchString(m) || reColunaSolta.MatchString(m)
	if palavraBoard && (lugarBoard || (reAgregBoard.MatchString(m) && reCardsPlural.MatchString(m))) {
		return ConsultaBoard
	}
	// Ações com palavra-chave própria primeiro: "os meus padrões" e "tabela
	// de preços" são fisgados pelo \bos\b (ASCII) das fatias de consulta —
	// com verbo de operação explícito, a intenção de ação vence.
	switch {
	case reAcaoPadroes.MatchString(m):
		return AcaoPadroes
	case reAcaoComissao.MatchString(m):
		return AcaoComissao
	case reAcaoOrcamento.MatchString(m):
		return AcaoOrcamento
	}
	// Ticket com número: consulta de leitura vence a ação ("o que
	// responderam no ticket 123?") — e vem antes das fatias porque o
	// \bos\b (ASCII) do reConsultaOS fisga "os tickets".
	if reConsultaTicket.MatchString(m) && reAlvoTicket.MatchString(m) && !reAcaoTicketForte.MatchString(m) {
		return ConsultaTicket
	}
	// Ações com alvo de ticket/card/importar antes das fatias de dados,
	// pelo mesmo motivo (\bos\b pega "os tickets fechados", "os cards").
	switch {
	case reAcaoTicket.MatchString(m):
		return AcaoTicket
	case reAcaoCard.MatchString(m):
		return AcaoCard
	case reAcaoImportar.MatchString(m):
		return AcaoImportar
	}
	n, primeira := 0, ConsultaNenhuma
	for _, c := range []struct {
		re     *regexp.Regexp
		classe ClasseConsulta
	}{
		{reConsultaFinanceiro, ConsultaFinanceiro},
		{reConsultaOS, ConsultaOS},
		{reConsultaEquip, ConsultaEquipamentos},
	} {
		if c.re.MatchString(m) {
			n++
			if primeira == ConsultaNenhuma {
				primeira = c.classe
			}
		}
	}
	if n >= 2 {
		return ConsultaCliente
	}
	if n == 1 {
		return primeira
	}
	return ConsultaNenhuma
}

// ExtrairAlvo identifica sobre quem a consulta recai: número de ticket
// (#123 ou "ticket 123"), código service explícito, número solto (código) ou
// nome do cliente (entre aspas ou depois de "cliente/card"). Vazio quando não
// há alvo explícito — a pré-busca então usa o último ticket capturado.
func ExtrairAlvo(m string) AlvoConsulta {
	if mt := reAlvoTicket.FindStringSubmatch(m); mt != nil {
		return AlvoConsulta{Tipo: "ticket", Valor: mt[1]}
	}
	if mc := reAlvoCod.FindStringSubmatch(m); mc != nil {
		return AlvoConsulta{Tipo: "cod", Valor: mc[1]}
	}
	if ma := reAlvoAspas.FindStringSubmatch(m); ma != nil {
		return AlvoConsulta{Tipo: "nome", Valor: strings.TrimSpace(ma[1])}
	}
	if mn := reAlvoNumero.FindStringSubmatch(m); mn != nil {
		return AlvoConsulta{Tipo: "cod", Valor: mn[1]}
	}
	if mn := reAlvoNome.FindStringSubmatch(m); mn != nil {
		nome := mn[1]
		if corte := reCorteNome.FindStringSubmatch(nome); corte != nil {
			nome = corte[1]
		}
		nome = strings.TrimSpace(strings.TrimRight(nome, ".,;:!?"))
		if len([]rune(nome)) >= 3 {
			return AlvoConsulta{Tipo: "nome", Valor: nome}
		}
	}
	return AlvoConsulta{}
}

// ferramentaPorFatia mapeia cada fatia de dados às ferramentas de leitura
// que ela torna redundantes (saem do request quando o dado já foi injetado).
var ferramentaPorFatia = map[ClasseConsulta][]string{
	ConsultaFinanceiro:   {"financeiro_cliente"},
	ConsultaOS:           {"atendimentos_cliente"},
	ConsultaEquipamentos: {"equipamentos_cliente"},
	ConsultaCliente:      {"financeiro_cliente", "atendimentos_cliente", "equipamentos_cliente"},
	ConsultaTicket:       {"detalhe_ticket_360"},
	ConsultaBoard:        {"listar_tickets"},
}

// FerramentasFatia devolve as ferramentas de leitura cobertas pela classe.
func FerramentasFatia(c ClasseConsulta) []string {
	return ferramentaPorFatia[c]
}

// FerramentasRemovidas monta a lista de tools a tirar do request: as fatias
// já injetadas no contexto (redundantes) e, com alvo identificado, a busca
// de clientes (o backend já resolveu o código) e a listagem de colunas.
// Sem alvo explícito nada sai do request: o bloco pode ser sobre o último
// ticket capturado, mas a pergunta pode alcançar outros clientes — a IA
// precisa das ferramentas para o resto do quadro.
func FerramentasRemovidas(classe ClasseConsulta, temAlvo bool) []string {
	rem := map[string]bool{}
	for _, f := range ferramentaPorFatia[classe] {
		rem[f] = true
	}
	if !temAlvo && classe != ConsultaBoard {
		return nil
	}
	if temAlvo && classe != ConsultaTicket {
		rem["buscar_cliente_360"] = true
		rem["listar_colunas"] = true
	}
	out := make([]string, 0, len(rem))
	for f := range rem {
		out = append(out, f)
	}
	return out
}
