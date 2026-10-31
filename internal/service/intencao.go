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
	Coluna string // mover: destino; apagar: onde o usuário disse que o card está
}

var (
	reApagarCard = regexp.MustCompile(
		`(?i)^\s*(?:apag\w+|exclu\w+|remov\w+|delet\w+|elimin\w+|tire|tira|retir\w+)\s+(?:o\s+|a\s+|esse\s+|este\s+|aquele\s+|um\s+|uma\s+|esse\s+aqui\s+o\s+)?(?:card|cliente|ticket)\b(?:\s+d[oa]s?\s+cliente)?\s+(.+?)\s*[.!?]*$`)
	reMoverCard = regexp.MustCompile(
		`(?i)^\s*(?:mov\w+|arrast\w+|passe|passar|lev\w+|mand\w+|mud\w+|jog\w+|pux\w+|coloc\w+|coloque|coloq\w+|ponh\w+|põe|poe|pôr|por|deix\w+|mex\w+|encaix\w+)\s+(?:o\s+|a\s+|esse\s+|este\s+|aquele\s+|um\s+|uma\s+)?(?:card|cliente|ticket)\b(?:\s+d[oa]s?\s+cliente)?\s+(.+?)\s+(?:para|pro|pra|p\/|em|na|no|to)\s+(?:a\s+coluna\s+|coluna\s+|o\s+|a\s+)?(.+?)\s*[.!?]*$`)
	// Qualificador de local no fim da frase ("que está em Cobrança",
	// "tá na Cobrança", "em Cobrança", "da Cobrança") — a coluna citada
	// serve para conferir onde o card realmente está antes de agir. O
	// (?i:) é escopado: o nome da coluna exige inicial maiúscula, senão
	// nomes de cliente com "de/em" viram falso qualificador.
	reQualificadorColuna = regexp.MustCompile(
		`^(.+?)\s+(?i:(?:que\s+(?:est[áa]|t[aá])\s+|t[aá]\s+)?(?:em|na|no|d[ao])\s+(?:a\s+coluna\s+|coluna\s+|o\s+|a\s+)?)([A-ZÂÉÍÓÚÃÕÇ].*?)\s*[.!?]*$`)
)

// ParseIntencaoCard reconhece "apagar card X" e "mover card X para Y" no
// texto; ok=false quando não é um desses comandos.
func ParseIntencaoCard(msg string) (IntencaoLocal, bool) {
	if m := reApagarCard.FindStringSubmatch(msg); m != nil {
		in := IntencaoLocal{Apagar: true, Alvo: strings.TrimSpace(m[1])}
		if q := reQualificadorColuna.FindStringSubmatch(in.Alvo); q != nil {
			in.Alvo = strings.TrimRight(strings.TrimSpace(q[1]), ",;")
			in.Coluna = strings.TrimSpace(q[2])
		}
		return in, true
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
	ColunaAtual string
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

// ---- Comando /vincular do chat ----

var comandosVincular = []string{"/vincular", "/vincule", "/vincula", "/vincular_todos", "/vinculartodos"}

// ParseComandoVincular reconhece o comando /vincular (e variantes), sem
// argumentos: uma varredura única que liga a cada card sem ticket o ticket
// mais recente do cliente na fila do 360.
func ParseComandoVincular(msg string) bool {
	f := strings.Fields(strings.TrimSpace(msg))
	if len(f) == 0 {
		return false
	}
	palavra := strings.ToLower(strings.Trim(f[0], ".,!?;:"))
	for _, c := range comandosVincular {
		if palavra == c {
			return true
		}
	}
	return false
}

// NormalizarTexto expõe a normalização interna (caixa, acentos, pontuação
// das pontas) para quem está fora do pacote casar nomes de cliente com
// solicitantes do 360.
func NormalizarTexto(s string) string {
	return normalizar(s)
}

// ---- Comando /realizada do chat ----

// ComandoRealizada é o "/realizada ..." reconhecido no texto do chat: Card é
// o nome (pedaço) ou código do card; vazio = o cliente em discussão no chat.
type ComandoRealizada struct {
	Card string
}

var comandosRealizada = []string{"/realizada", "/realizado", "/visitarealizada", "/visita_realizada"}

// ParseComandoRealizada reconhece o comando /realizada (e variantes) no texto:
// "/realizada", "/realizada João". ok=false quando o texto não começa com o
// comando (cai no fluxo normal do chat).
func ParseComandoRealizada(msg string) (ComandoRealizada, bool) {
	campos := strings.Fields(strings.TrimSpace(msg))
	if len(campos) == 0 {
		return ComandoRealizada{}, false
	}
	palavra := strings.ToLower(strings.Trim(campos[0], ".,!?;:"))
	ehComando := false
	for _, c := range comandosRealizada {
		if palavra == c {
			ehComando = true
			break
		}
	}
	if !ehComando {
		return ComandoRealizada{}, false
	}
	corpo := limparBordas(strings.TrimSpace(msg[len(campos[0]):]))
	return ComandoRealizada{Card: corpo}, true
}

// ---- Comando /apagar do chat ----

// ComandoApagar é o "/apagar ..." reconhecido no texto do chat: Card é o nome
// (pedaço) ou código do card — vazio quando o comando veio sem alvo (aí quem
// resolve é o contexto do chat); Coluna é o qualificador opcional "em X" que
// serve só para conferir onde o card está antes de destruir.
type ComandoApagar struct {
	Card   string
	Coluna string
}

var comandosApagar = []string{
	"/apagar", "/apague", "/apaga", "/excluir", "/exclua", "/exclui",
	"/delete", "/deletar", "/deleta", "/remover", "/remova", "/remove",
}

// reConectorLocal acha o "em/na/no" (com artigo/coluna opcional) que separa o
// alvo do qualificador de coluna; o último vence.
var reConectorLocal = regexp.MustCompile(`(?i)\s+(?:em|na|no)\s+(?:a\s+coluna\s+|coluna\s+|o\s+|a\s+)?`)

// ParseComandoApagar reconhece o comando /apagar (e variantes) no texto:
// "/apagar João", "/apagar #12345 em Cobrança" ou "/apagar" sozinho — sem
// alvo, o cliente é quem está em discussão no chat. ok=false quando o texto
// não começa com o comando (cai no fluxo normal do chat).
func ParseComandoApagar(msg string) (ComandoApagar, bool) {
	campos := strings.Fields(strings.TrimSpace(msg))
	if len(campos) == 0 {
		return ComandoApagar{}, false
	}
	palavra := strings.ToLower(strings.Trim(campos[0], ".,!?;:"))
	ehComando := false
	for _, c := range comandosApagar {
		if palavra == c {
			ehComando = true
			break
		}
	}
	if !ehComando {
		return ComandoApagar{}, false
	}
	corpo := limparBordas(strings.TrimSpace(msg[len(campos[0]):]))
	if locs := reConectorLocal.FindAllStringIndex(corpo, -1); len(locs) > 0 {
		loc := locs[len(locs)-1]
		return ComandoApagar{
			Card:   limparBordas(corpo[:loc[0]]),
			Coluna: limparBordas(corpo[loc[1]:]),
		}, true
	}
	return ComandoApagar{Card: corpo}, true
}

// ---- Cliente citado num texto livre (contexto do chat) ----

// reNumeroCard acha códigos service/ids citados no texto: "#1" curto com
// cerilha, ou número de 3+ dígitos solto ("24755") — soltos curtos demais
// ("3 dias") casariam id de card por coincidência.
var reNumeroCard = regexp.MustCompile(`#(\d{1,10})\b|(?:^|[\s(])(\d{3,10})\b`)

// ResolverCardNoTexto acha, num texto livre (uma mensagem do chat), UM card
// citado — é assim que "/move cobrança" descobre o cliente em discussão.
// Primeiro por código service/id citado no texto; depois por nome completo;
// por fim por um pedaço do nome (≥4 letras, palavra inteira) que case com um
// único card. ok=false quando nada casa ou o casamento é ambíguo — agir no
// card errado é pior que pedir de novo.
func ResolverCardNoTexto(texto string, cards []CardResumo) (CardResumo, bool) {
	for _, m := range reNumeroCard.FindAllStringSubmatch(texto, -1) {
		n := m[1]
		if n == "" {
			n = m[2]
		}
		for _, c := range cards {
			if c.CodService == n || strconv.FormatInt(c.ID, 10) == n {
				return c, true
			}
		}
	}
	textoN := " " + normalizar(texto) + " "
	var porNome, porPedaço []CardResumo
	for _, c := range cards {
		nome := normalizar(c.NomeCliente)
		if nome != "" && strings.Contains(textoN, " "+nome) {
			porNome = append(porNome, c)
			continue
		}
		for _, tk := range strings.Fields(nome) {
			if len(tk) < 4 || pedaçoComum(tk) {
				continue
			}
			if strings.Contains(textoN, " "+tk+" ") {
				porPedaço = append(porPedaço, c)
				break
			}
		}
	}
	if len(porNome) == 1 {
		return porNome[0], true
	}
	if len(porPedaço) == 1 {
		return porPedaço[0], true
	}
	return CardResumo{}, false
}

// pedaçoComum filtra tokens que aparecem em qualquer nome/razão social e não
// identificam ninguém sozinhos.
func pedaçoComum(tk string) bool {
	switch tk {
	case "ltda", "me", "eireli", "comercio", "empresa", "servicos", "sistema",
		"sistemas", "silva", "santos":
		return true
	}
	return false
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

// Normalizar é a versão exportada de normalizar, para quem de fora do service
// precisa comparar textos com a mesma regra (caixa/acento/pontuação).
func Normalizar(s string) string { return normalizar(s) }

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

// ---- Comando "abra o card do cliente X" (texto livre) ----

// IntencaoAbrir é o pedido de abrir/verificar o card de um cliente, ticket ou
// código: "abra o cliente X", "abra o card do X", "o ticket 123 tem card?".
type IntencaoAbrir struct {
	Alvo     string // nome, código service, id do card ou nº do ticket 360
	Contexto bool   // alvo é pronome ("esse cliente", "ele"): quem resolve é o contexto da conversa
	Abrir    bool   // comando de abertura ("abra o card"): abre o modal, não só responde
}

// reAbrirCard: verbo de abertura + palavra-chave obrigatória (card/cliente/
// ticket) — "abra o orçamento" não é card.
var reAbrirCard = regexp.MustCompile(
	`(?i)^\s*(?:abra|abre|abrir|abri|mostre|mostra|me\s+mostre|me\s+mostra)\s+(?:o\s+|a\s+|esse\s+|este\s+)?(?:card|cliente|ticket)\b(?:\s+d[oa]s?\s+(?:cliente\s+)?)?(.+?)\s*[.!?]*$`)

// reTemCard: "verifique se o cliente X tem card", "o ticket 123 tem card?".
var reTemCard = regexp.MustCompile(
	`(?i)^\s*(?:(?:verifi\w+|confer\w+|confira|veja|checa|checar|olha|olhe)\s+)?(?:se\s+)?(?:o\s+|a\s+|esse\s+|este\s+|aquele\s+)?(?:(?:card|cliente|ticket)\b(?:\s+d[oa]s?\s+(?:cliente\s+)?)?\s+)?(.+?)\s+(?:tem|possui|poss\w+|existe)\s+(?:um\s+|algum\s+)?cards?\s*(?:abertos?|em\s+aberto)?\s*[.!?]*$`)

// reContextoCard reconhece a pergunta SEM nome: "esse cliente possui card
// aberto?", "ele tem card?", "o cliente tem card?". Tem de vir antes das
// regexes gerais, senão o pronome vira alvo.
// reAbrirCardContexto: comando de abertura SEM nome — "abra o card",
// "abrir o card", "abre o card do cliente", "me mostre o card". O card é o
// do cliente em discussão na conversa e o modal abre de verdade.
var reAbrirCardContexto = regexp.MustCompile(
	`(?i)^\s*(?:abra|abre|abrir|abri|expand\w*|mostre|mostra|me\s+mostre|me\s+mostra|visualiz\w*|vejo|veja)\s+(?:o\s+|esse\s+|este\s+|aquele\s+)?cards?\s*(?:d[oa]\s+)?(?:cliente\b)?\s*[.!?]?\s*$`)

var reContextoCard = regexp.MustCompile(
	`(?i)^\s*(?:(?:verifi\w+|confer\w+|confira|veja|checa|checar|olha|olhe)\s+)?(?:se\s+)?(?:o\s+|a\s+|esse\s+|este\s+|aquele\s+|ele\s+|ela\s+)?(?:card\s+|cliente\s+|ticket\s+)?(?:d[oa]\s+)?(?:cliente\s+)?(?:tem|possui|poss\w+|existe)\s+(?:um\s+|algum\s+)?cards?\s*(?:abertos?|em\s+aberto)?\s*[.!?]*$`)

// ParseIntencaoAbrir reconhece os pedidos de abrir/verificar card no texto;
// ok=false quando não é um deles.
func ParseIntencaoAbrir(msg string) (IntencaoAbrir, bool) {
	if reAbrirCardContexto.MatchString(msg) {
		return IntencaoAbrir{Contexto: true, Abrir: true}, true
	}
	if reContextoCard.MatchString(msg) {
		return IntencaoAbrir{Contexto: true}, true
	}
	for _, re := range []*regexp.Regexp{reAbrirCard, reTemCard} {
		if m := re.FindStringSubmatch(msg); m != nil {
			alvo := strings.TrimSpace(m[1])
			// "abra o card do cliente João" → "cliente João" vira "João".
			alvo = strings.TrimPrefix(alvo, "cliente ")
			alvo = limparBordas(alvo)
			if alvo == "" {
				continue
			}
			// Pronome em vez de nome: quem sabe de quem se fala é o
			// contexto da conversa (executarIntencaoAbrir resolve).
			switch normalizar(alvo) {
			case "esse cliente", "este cliente", "aquele cliente", "o cliente", "a cliente",
				"ele", "ela", "desse cliente", "deste cliente", "esse card", "este card":
				return IntencaoAbrir{Contexto: true}, true
			}
			return IntencaoAbrir{Alvo: alvo}, true
		}
	}
	return IntencaoAbrir{}, false
}

// ---- Mover pelo contexto do chat: "coloque em cobrança" ----

// reMoverContexto reconhece mover SEM citar o nome: "coloque em cobrança",
// "põe o cliente em Cobrança", "mova pra Negociando". A coluna vem do texto;
// o cliente é o que está em discussão na conversa.
var reMoverContexto = regexp.MustCompile(
	`(?i)^\s*(?:mov\w+|arrast\w+|passe|passar|lev\w+|mand\w+|mud\w+|jog\w+|pux\w+|coloc\w+|coloque|coloq\w+|ponh\w+|p[õo]e|pôr|por|deix\w+|mex\w+)\s+(?:o\s+|a\s+|esse\s+|este\s+|aquele\s+|um\s+|uma\s+)?(?:card\s+|cliente\s+|ticket\s+)?(?:d[oa]\s+)?(?:cliente\s+)?(?:em|na|no)\s+(?:a\s+coluna\s+|coluna\s+|o\s+|a\s+)?(.+?)\s*[.!?]*$`)

// ParseIntencaoMoverContexto devolve a coluna pedida quando a mensagem move
// o cliente do contexto sem citar nome. ok=false quando citar nome (ou não
// for mover) — aí quem resolve é ParseIntencaoCard.
func ParseIntencaoMoverContexto(msg string) (string, bool) {
	m := reMoverContexto.FindStringSubmatch(msg)
	if m == nil {
		return "", false
	}
	return limparBordas(m[1]), true
}

// ---- Sufixo "… e abra o card" ----

// reAbraNoFim captura um "e abra o card" pendurado no fim de outro comando
// ("coloque o cliente João em Cobrança e abra o card"): quem pede executa a
// ação E abre o card no final.
var reAbraNoFim = regexp.MustCompile(
	`(?i)[,;]?\s*(?:e\s+|e\s+depois\s+|depois\s+)?abr[ae]r?\s+(?:o\s+|esse\s+|este\s+)?cards?\s*[.!?]*\s*$`)

// TirarAbraNoFim tira o "e abra o card" do fim da mensagem e sinaliza que o
// card da ação deve ser aberto ao terminar. ok=false quando não há sufixo.
func TirarAbraNoFim(msg string) (string, bool) {
	loc := reAbraNoFim.FindStringIndex(msg)
	if loc == nil {
		return msg, false
	}
	return strings.TrimSpace(msg[:loc[0]]), true
}
