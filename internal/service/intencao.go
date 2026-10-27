package service

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Reconhecimento local de intenção: comandos rígidos de card ("apague o card
// do João", "mova o card #123 para Cobrança") são detectados por regex e
// executados direto — sem chamada de API, sem chave, resposta instantânea.
// Frases fora do molde caem no fluxo normal do agente.

// IntencaoLocal é um comando de card reconhecido no texto puro.
type IntencaoLocal struct {
	Apagar bool   // true = apagar card; false = mover
	Alvo   string // nome, código service ou id do card
	Coluna string // destino (só no mover)
}

var (
	reApagarCard = regexp.MustCompile(
		`(?i)^\s*(?:apag\w+|exclu\w+|remov\w+|delet\w+|elimin\w+|tire|tira|retir\w+)\s+(?:o\s+|a\s+|esse\s+|este\s+|aquele\s+|um\s+|uma\s+|esse\s+aqui\s+o\s+)?(?:card|cliente|ticket)\b(?:\s+d[oa]s?\s+cliente)?\s+(.+?)\s*[.!?]*$`)
	reMoverCard = regexp.MustCompile(
		`(?i)^\s*(?:mov\w+|arrast\w+|passe|passar|lev\w+|mand\w+|mud\w+|jog\w+|pux\w+|coloc\w+|coloque|coloq\w+|ponh\w+|põe|poe|pôr|por|deix\w+|mex\w+|encaix\w+)\s+(?:o\s+|a\s+|esse\s+|este\s+|aquele\s+|um\s+|uma\s+)?(?:card|cliente|ticket)\b(?:\s+d[oa]s?\s+cliente)?\s+(.+?)\s+(?:para|pro|pra|p\/|em|na|no|to)\s+(?:a\s+coluna\s+|coluna\s+|o\s+|a\s+)?(.+?)\s*[.!?]*$`)
)

// ParseIntencaoCard reconhece "apagar card X" e "mover card X para Y" no
// texto; ok=false quando não é um desses comandos.
func ParseIntencaoCard(msg string) (IntencaoLocal, bool) {
	if m := reApagarCard.FindStringSubmatch(msg); m != nil {
		return IntencaoLocal{Apagar: true, Alvo: strings.TrimSpace(m[1])}, true
	}
	if m := reMoverCard.FindStringSubmatch(msg); m != nil {
		return IntencaoLocal{Apagar: false, Alvo: strings.TrimSpace(m[1]), Coluna: strings.TrimSpace(m[2])}, true
	}
	return IntencaoLocal{}, false
}

// CardResumo é o mínimo que o reconhecimento precisa saber dos cards.
type CardResumo struct {
	ID          int64
	NomeCliente string
	CodService  string
}

// ResolverCard casa o alvo com UM card: número puro ou "#n" = código service
// (ou id), texto = nome (substring, sem caixa). Zero ou vários matches são
// erro — apagar/mover o card errado é pior que pedir de novo.
func ResolverCard(alvo string, cards []CardResumo) (CardResumo, error) {
	alvo = strings.TrimSpace(alvo)
	alvo = strings.TrimPrefix(alvo, "#")
	if alvo == "" {
		return CardResumo{}, fmt.Errorf("diga o nome do cliente (ou o código) do card")
	}
	if n, err := strconv.ParseInt(alvo, 10, 64); err == nil {
		for _, c := range cards {
			if c.ID == n || c.CodService == alvo {
				return c, nil
			}
		}
		return CardResumo{}, fmt.Errorf("nenhum card com código/id %s", alvo)
	}
	low := strings.ToLower(alvo)
	var achados []CardResumo
	for _, c := range cards {
		if strings.Contains(strings.ToLower(c.NomeCliente), low) {
			achados = append(achados, c)
		}
	}
	switch len(achados) {
	case 1:
		return achados[0], nil
	case 0:
		return CardResumo{}, fmt.Errorf("nenhum card com %q no nome", alvo)
	default:
		nomes := make([]string, 0, len(achados))
		for _, c := range achados {
			nomes = append(nomes, fmt.Sprintf("%s (código %s)", c.NomeCliente, mapaTexto(c.CodService)))
		}
		return CardResumo{}, fmt.Errorf("%d cards casam com %q: %s — seja mais específico",
			len(achados), alvo, strings.Join(nomes, "; "))
	}
}

func mapaTexto(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

// ---- Comando /move do chat ----

// ComandoMove é o "/move ..." reconhecido no texto do chat: com Ticket preen-
// chido move um ticket do 360 (mesma ação do Shift+F6); com Card move um card
// que já existe; Coluna é o destino como foi digitado (ainda sem casar com o
// board — quem casa é CasarColuna).
type ComandoMove struct {
	Ticket string // nº (com ou sem "#"), uuid ou URL do ticket no 360
	Card   string // nome (pedaço) ou código do card
	Coluna string // texto livre da coluna de destino
}

var comandosMove = []string{"/move", "/mover", "/movimenta", "/movimentar"}

// reConectorDestino acha o "para/pro/pra/p/|em|na|no" que separa o alvo da
// coluna; o último vence ("mover 1 para A em B" → coluna "B" — inexistente no
// board, mas é o que foi escrito).
var reConectorDestino = regexp.MustCompile(`(?i)\s+(?:para|pro|pra|p/|em|na|no)\s+`)

// ParseComandoMove reconhece o comando /move (e variantes) no texto:
// "/move 12345 para Cobrança", "/move #123 Cobrança", "/move <URL do 360> para
// Aguardando", "/move João para Cobrança". ok=false quando o texto não começa
// com o comando (cai no fluxo normal do chat).
func ParseComandoMove(msg string) (ComandoMove, bool) {
	campos := strings.Fields(strings.TrimSpace(msg))
	if len(campos) == 0 {
		return ComandoMove{}, false
	}
	palavra := strings.ToLower(strings.Trim(campos[0], ".,!?;:"))
	ehComando := false
	for _, c := range comandosMove {
		if palavra == c {
			ehComando = true
			break
		}
	}
	if !ehComando {
		return ComandoMove{}, false
	}
	corpo := limparBordas(strings.TrimSpace(msg[len(campos[0]):]))
	ticket, resto := tirarTokenTicket(corpo)
	if ticket != "" {
		return ComandoMove{Ticket: ticket, Coluna: limparBordas(resto)}, true
	}
	alvo, coluna := dividirAlvoColuna(corpo)
	return ComandoMove{Card: alvo, Coluna: coluna}, true
}

// tirarTokenTicket tira do corpo o primeiro token que parece ticket do 360
// (nº com ou sem "#", uuid ou URL) e devolve o resto do texto.
func tirarTokenTicket(corpo string) (ticket, resto string) {
	f := strings.Fields(corpo)
	for i, tk := range f {
		if !ehTokenTicket(tk) {
			continue
		}
		sobrou := append(append([]string{}, f[:i]...), f[i+1:]...)
		return tk, strings.Join(sobrou, " ")
	}
	return "", corpo
}

// ehTokenTicket reconhece os três formatos de ticket que o 360 aceita.
func ehTokenTicket(tk string) bool {
	if strings.Contains(strings.ToLower(tk), "helpdesk/tickets/") {
		return true
	}
	if len(tk) == 36 && strings.Count(tk, "-") == 4 {
		return true
	}
	n := strings.TrimPrefix(tk, "#")
	if n == "" || len(n) > 10 {
		return false
	}
	for _, r := range n {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// dividirAlvoColuna separa "João da Silva para Cobrança" no último conector
// de destino; sem conector, tudo é alvo e a coluna fica vazia.
func dividirAlvoColuna(corpo string) (alvo, coluna string) {
	if locs := reConectorDestino.FindAllStringIndex(corpo, -1); len(locs) > 0 {
		loc := locs[len(locs)-1]
		return strings.TrimSpace(corpo[:loc[0]]), limparBordas(corpo[loc[1]:])
	}
	return limparBordas(corpo), ""
}

// limparBordas tira conectores e artigos das pontas e a pontuação que sobrar
// nelas: "para a coluna Cobrança." → "Cobrança". O meio fica intacto (nomes
// têm "da", "de"...).
func limparBordas(s string) string {
	f := strings.Fields(s)
	i, j := 0, len(f)
	for i < j && ehConector(f[i]) {
		i++
	}
	for j > i && ehConector(f[j-1]) {
		j--
	}
	return strings.Trim(strings.Join(f[i:j], " "), ".,!?;:")
}

func ehConector(p string) bool {
	switch strings.ToLower(strings.Trim(p, ".,!?;:")) {
	case "para", "pro", "pra", "p/", "p", "to", "em", "na", "no", "a", "o", "coluna":
		return true
	}
	return false
}

// replAcentos mapeados para casar coluna digitada sem acento ("orcamento" =
// "Orçamento"). Só as letras usadas em português.
var replAcentos = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a",
	"é", "e", "ê", "e",
	"í", "i",
	"ó", "o", "ô", "o", "õ", "o",
	"ú", "u", "ü", "u",
	"ç", "c",
)

// normalizar baixa a caixa, tira acentos e pontas de pontuação.
func normalizar(s string) string {
	return replAcentos.Replace(strings.Trim(strings.ToLower(strings.TrimSpace(s)), ".,!?;:"))
}

// CasarColuna casa o texto digitado com UMA coluna do board, sem caixa e sem
// acento, aceitando pedaço ("cobr" → "Cobrança"). Zero ou vários matches são
// erro — mover para a coluna errada é pior que pedir de novo.
func CasarColuna(texto string, colunas []string) (string, error) {
	lista := strings.Join(colunas, ", ")
	t := normalizar(texto)
	t = strings.TrimPrefix(t, "a coluna ")
	t = strings.TrimPrefix(t, "coluna ")
	if t == "" {
		return "", fmt.Errorf("diga a coluna de destino. Colunas: %s", lista)
	}
	for _, c := range colunas {
		if normalizar(c) == t {
			return c, nil
		}
	}
	var achadas []string
	for _, c := range colunas {
		if strings.Contains(normalizar(c), t) {
			achadas = append(achadas, c)
		}
	}
	switch len(achadas) {
	case 1:
		return achadas[0], nil
	case 0:
		return "", fmt.Errorf("nenhuma coluna com %q. Colunas: %s", texto, lista)
	default:
		return "", fmt.Errorf("%d colunas casam com %q: %s — seja mais específico", len(achadas), texto, strings.Join(achadas, "; "))
	}
}
