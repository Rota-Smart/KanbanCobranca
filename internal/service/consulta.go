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
)

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

// AlvoConsulta é o alvo identificado na mensagem.
type AlvoConsulta struct {
	Tipo  string // "ticket" | "cod" | "nome" | ""
	Valor string
}

// ClassificarConsulta devolve a(s) fatia(s) de dados que a mensagem pede.
// Vazia quando não é consulta de dados do 360 (ação, reescrita, meta...).
func ClassificarConsulta(m string) ClasseConsulta {
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
	if reConsultaTicket.MatchString(m) && reAlvoTicket.MatchString(m) {
		return ConsultaTicket
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
}

// FerramentasFatia devolve as ferramentas de leitura cobertas pela classe.
func FerramentasFatia(c ClasseConsulta) []string {
	return ferramentaPorFatia[c]
}

// FerramentasRemovidas monta a lista de tools a tirar do request: as fatias
// já injetadas no contexto (redundantes) e, com alvo identificado, a busca
// de clientes (o backend já resolveu o código) e a listagem de colunas.
func FerramentasRemovidas(classe ClasseConsulta, temAlvo bool) []string {
	rem := map[string]bool{}
	for _, f := range ferramentaPorFatia[classe] {
		rem[f] = true
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
