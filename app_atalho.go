package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"rota-smart/internal/hotkey"
	"rota-smart/internal/rota360"
	"rota-smart/internal/tela"
)

// Eventos Wails emitidos quando um atalho global dispara (o frontend
// escuta via EventsOn e abre o chat do Groq ou a busca de clientes).
const (
	evAtalhoTela  = "atalho:tela"
	evAtalhoBusca = "atalho:busca"
	// Shift+F4: abre a pesquisa de tickets do helpdesk.
	evAtalhoBuscaTickets = "atalho:busca-tickets"
	// Shift+F5: lê a tela do Access (OS) e abre o chat com o texto.
	evAtalhoAcesso = "atalho:acesso"
)

// TicketDaTela é o resultado da identificação do que estava na tela
// quando o usuário apertou Shift+F2.
type TicketDaTela struct {
	Achou       bool   `json:"achou"`
	Fonte       string `json:"fonte"` // "tela", "clipboard" ou "fila360"
	TicketID    int64  `json:"ticketId"`
	NomeCliente string `json:"nomeCliente"`
	CodService  string `json:"codService"`
	Titulo      string `json:"titulo"`
	Mensagem    string `json:"mensagem"`
}

// TicketTelaCapturado guarda o ticket do 360 identificado pelo Shift+F2, para
// vinculá-lo automaticamente ao card caso o cliente do ticket seja adicionado
// ao Kanban depois (import do 360, chat do assistente, card manual). Válido só
// com uuid/numero: código service sozinho não identifica um ticket.
type TicketTelaCapturado struct {
	UUID        string
	Numero      string
	CodService  string
	NomeCliente string
}

// guardarTicketTela anota o último ticket visto na tela pelo Shift+F2.
func (a *App) guardarTicketTela(t TicketTelaCapturado) {
	a.muTicketTela.Lock()
	a.ticketTela = t
	a.muTicketTela.Unlock()
}

// ultimoTicketTela devolve uma cópia do último ticket capturado.
func (a *App) ultimoTicketTela() TicketTelaCapturado {
	a.muTicketTela.Lock()
	defer a.muTicketTela.Unlock()
	return a.ticketTela
}

// iniciarAtalhos registra os atalhos globais em goroutine dedicada —
// a thread do hotkey precisa viver com o loop de mensagens até o fim.
func (a *App) iniciarAtalhos() {
	go func() {
		if err := hotkey.Registrar(a.onAtalhoTicketTela, a.onAtalhoBusca, a.onAtalhoBuscaTickets, a.onAtalhoAcesso); err != nil {
			slog.Warn("atalhos globais indisponíveis", "err", err)
		}
	}()
}

// mostrarJanela traz o app para frente em tela cheia (maximizada), mesmo
// que tenha sido fechada minimizada de um tamanho menor.
func (a *App) mostrarJanela() {
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
	runtime.WindowMaximise(a.ctx)
}

// onAtalhoBusca (Shift+F3): abre o app na busca de clientes.
func (a *App) onAtalhoBusca() {
	a.mostrarJanela()
	runtime.EventsEmit(a.ctx, evAtalhoBusca)
}

// onAtalhoBuscaTickets (Shift+F4): abre o app na pesquisa de tickets do 360.
func (a *App) onAtalhoBuscaTickets() {
	a.mostrarJanela()
	runtime.EventsEmit(a.ctx, evAtalhoBuscaTickets)
}

// LeituraAcesso é o resultado do Shift+F5: o texto lido da janela em foco
// (normalmente o Access, com a OS) e o briefing pronto para o chat.
type LeituraAcesso struct {
	Achou    bool   `json:"achou"` // texto legível na janela
	Processo string `json:"processo"`
	Titulo   string `json:"titulo"`
	Mensagem string `json:"mensagem"` // briefing completo para o agente (vazio se falhou)
}

// onAtalhoAcesso (Shift+F5): lê o texto da janela em foco via UIA (a OS do
// Service no Access), traz o app para frente e abre o chat com o briefing.
func (a *App) onAtalhoAcesso() {
	cap := tela.CapturaTextoDaTela()
	res := a.lerAcesso(cap)
	slog.Info("atalho shift+f5", "processo", cap.Processo, "achou", res.Achou,
		"runas", len([]rune(cap.Texto)))
	a.mostrarJanela()
	runtime.EventsEmit(a.ctx, evAtalhoAcesso, res)
}

// lerAcesso monta o briefing do chat a partir do texto lido da tela. Só
// captura com o Access em foco: outra janela vira aviso, sem leitura.
func (a *App) lerAcesso(cap tela.CapturaTexto) LeituraAcesso {
	res := LeituraAcesso{Processo: cap.Processo, Titulo: cap.Titulo}
	if !strings.EqualFold(cap.Processo, "msaccess.exe") {
		res.Mensagem = fmt.Sprintf("A janela em foco é %q, não o Access. Abra a tela da OS no Service (Access) e aperte Shift+F5 de novo.", cap.Titulo)
		return res
	}
	texto := strings.TrimSpace(cap.Texto)
	if texto == "" {
		res.Mensagem = "Não consegui ler texto da tela do Access. Confira se a janela da OS está visível (não minimizada) e aperte Shift+F5 de novo."
		return res
	}
	res.Achou = true
	res.Mensagem = fmt.Sprintf(
		"Li a tela da OS no Access (Service), janela %q. O texto bruto da tela está abaixo. Extraia o que interessa — número da OS, cliente, código service, equipamento, defeito relatado — resuma o caso e sugira os próximos passos. Se precisar de dados do cliente no 360, use as ferramentas com o código service que aparecer (não invente se não estiver no texto).\n\n[texto da tela]\n%s",
		cap.Titulo, texto)
	return res
}

// onAtalhoTicketTela (Shift+F2): identifica o ticket/cliente na janela em
// foco, traz o app para frente e emite o evento para o chat do Groq.
func (a *App) onAtalhoTicketTela() {
	cap := tela.Capturar()
	dado := a.identificarDaTela(cap)
	slog.Info("atalho shift+f2", "processo", cap.Processo, "achou", dado.Achou,
		"fonte", dado.Fonte, "cod", dado.CodService != "", "temUrl", cap.URL != "",
		"urlTicket", regexTicketURL.MatchString(cap.URL))
	a.mostrarJanela()
	runtime.EventsEmit(a.ctx, evAtalhoTela, dado)
}

// regexUUID captura ids de ticket no estilo Octadesk/360 na URL.
var regexUUID = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// regexTicketURL captura o uuid quando a tela é a página de um ticket.
var regexTicketURL = regexp.MustCompile(`/helpdesk/tickets/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`)

// regexCodigo procura um código numérico curto (cod service / nº do ticket).
var regexCodigo = regexp.MustCompile(`\d{3,10}`)

// identificarDaTela deduz qual cliente/ticket estava na tela:
//  1. página de ticket do 360 na URL → detalhe completo do ticket;
//  2. código numérico na URL ou no título da janela → card local;
//  3. uuid de ticket na URL → acha o código service na fila do 360;
//  4. texto recém-copiado (clipboard) como último recurso.
func (a *App) identificarDaTela(cap tela.Captura) TicketDaTela {
	res := TicketDaTela{Titulo: cap.Titulo}

	if m := regexTicketURL.FindStringSubmatch(cap.URL); m != nil {
		uuidTela := m[1]
		if det := a.detalheDoTicket(uuidTela); det != nil {
			res.Fonte = "tela"
			res.CodService = det.CodService
			res.NomeCliente = det.Solicitante
			res.Titulo = "#" + det.Numero + " " + det.Titulo
			a.guardarTicketTela(TicketTelaCapturado{
				UUID: uuidTela, Numero: det.Numero,
				CodService: det.CodService, NomeCliente: det.Solicitante,
			})
			autor := a.autorSessao360()
			if autor == "" {
				autor = det.ResponsavelNome // sem config, quem responde o ticket serve de aproximação
			}
			res.Mensagem = mensagemTicketDetalhe(det, autor)
			if t, err := a.tickets.GetPorCodService(a.ctx, det.CodService); err == nil && t != nil {
				res.Achou = true
				res.TicketID = t.ID
				res.NomeCliente = t.NomeCliente
			}
			return res
		}
		// A URL é de um ticket, mas o detalhe falhou (sem sessão no 360,
		// erro de rede, sessão expirada...) e a fila não resolveu o código.
		// Sem sessão o aviso precisa apontar a causa: sem ela nada resolve.
		if a.rotaAtual() == nil {
			res.Mensagem = "O app não está conectado ao 360. Abra as Configurações e clique em Conectar; depois aperte Shift+F2 de novo."
			return res
		}
		// Com sessão, pode ser ticket fora da primeira página da fila:
		// procurar o código nas pistas genéricas antes de desistir.
		if cod := a.codigoDoTicketNaFila(m[1]); cod != "" {
			res.CodService = cod
			res.Fonte = "fila360"
			a.guardarTicketTela(TicketTelaCapturado{UUID: uuidTela, CodService: cod})
			if t, err := a.tickets.GetPorCodService(a.ctx, cod); err == nil && t != nil {
				res.Achou = true
				res.TicketID = t.ID
				res.NomeCliente = t.NomeCliente
			}
			if res.Mensagem == "" {
				res.Mensagem = fmt.Sprintf(
					"Estou no ticket do cliente de código %s no 360. Decida quais dados dele faltam e consulte SÓ isso com as ferramentas (financeiro, equipamentos, atendimentos), aponte pendências relevantes e sugira os próximos passos.",
					cod)
			}
			return res
		}
	}

	urlSemUUID := regexUUID.ReplaceAllString(cap.URL, " ")
	// O host (360.rotagroup.com.br) não tem código de cliente — só o caminho
	// interessa, senão o "360" do domínio vira falso positivo.
	if u, err := url.Parse(urlSemUUID); err == nil && u.Host != "" {
		urlSemUUID = u.Path + "?" + u.RawQuery
	}
	if cod := primeiroCodigo(urlSemUUID); cod != "" {
		res.CodService = cod
		res.Fonte = "tela"
	} else if cod := primeiroCodigo(cap.Titulo); cod != "" {
		res.CodService = cod
		res.Fonte = "tela"
	} else if cod := primeiroCodigo(cap.Clipboard); cod != "" {
		res.CodService = cod
		res.Fonte = "clipboard"
	} else if u := regexUUID.FindString(cap.URL); u != "" {
		if cod := a.codigoDoTicketNaFila(u); cod != "" {
			res.CodService = cod
			res.Fonte = "fila360"
			a.guardarTicketTela(TicketTelaCapturado{UUID: u, CodService: cod})
		}
	}

	if res.CodService == "" {
		res.Mensagem = "Não reconheci um ticket ou cliente na tela em foco. Cole o código ou peça no chat."
		return res
	}
	if t, err := a.tickets.GetPorCodService(a.ctx, res.CodService); err == nil && t != nil {
		res.Achou = true
		res.TicketID = t.ID
		res.NomeCliente = t.NomeCliente
	}
	if res.Mensagem == "" {
		if res.Achou && res.TicketID > 0 {
			res.Mensagem = fmt.Sprintf(
				"Continue o atendimento do card #%d — %s (código service %s). Decida quais dados do cliente faltam e consulte SÓ isso no 360 com as ferramentas (financeiro, equipamentos, atendimentos), revise acordos e avaliação, e sugira os próximos passos.",
				res.TicketID, res.NomeCliente, res.CodService)
		} else {
			res.Mensagem = fmt.Sprintf(
				"Estou com o cliente de código %s na tela do 360. Decida quais dados dele faltam e consulte SÓ isso com as ferramentas (financeiro, equipamentos, atendimentos), aponte pendências relevantes e sugira os próximos passos.",
				res.CodService)
		}
	}
	return res
}

// detalheDoTicket busca o ticket completo pelo uuid; devolve nil se não houver
// sessão no 360 ou se a busca falhar (o fluxo segue pelos caminhos antigos).
func (a *App) detalheDoTicket(uuid string) *rota360.TicketDetalhe360 {
	c := a.rotaAtual()
	if c == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	t, err := c.DetalheTicket(ctx, uuid)
	if err != nil {
		slog.Warn("detalhe do ticket do 360", "uuid", uuid, "err", err)
		return nil
	}
	return t
}

// Limites do resumo de ticket enviado à IA: o Go lê o ticket inteiro no 360,
// mas a IA recebe só o que serve para formular a resposta, dentro de um teto
// — cada rune daqui é reenviado em toda rodada do agente (TPM 8K).
const (
	maxRunasDescricaoTicket  = 700
	maxRunasInteracaoTicket  = 700
	maxRunasResumoTicket     = 2500
)

// resumoMensagensTicket formata o resumo do ticket para a IA: a descrição
// (primeira mensagem, o pedido original) e a última interação que NÃO é do
// autor da sessão — o último recado do outro lado, que é o que a resposta
// precisa endereçar. Interações intermediárias ficam de fora; a nota diz
// quantas existem no 360. autorSessao vazio desativa o filtro (pega a última).
func resumoMensagensTicket(mensagens []rota360.MensagemTicket360, autorSessao string) string {
	indiceDesc := -1
	for i, m := range mensagens {
		if m.Tipo == "descricao" && strings.TrimSpace(m.Corpo) != "" {
			indiceDesc = i
			break
		}
	}
	indiceUltima := -1
	for i := len(mensagens) - 1; i >= 0; i-- {
		m := mensagens[i]
		if m.Tipo == "sistema" || strings.TrimSpace(m.Corpo) == "" {
			continue
		}
		if autorSessao != "" && strings.EqualFold(strings.TrimSpace(m.Autor), autorSessao) {
			continue // mensagem da própria sessão: não é o "último recado"
		}
		indiceUltima = i
		break
	}
	if indiceDesc < 0 && indiceUltima < 0 {
		return ""
	}

	var validas int
	for _, m := range mensagens {
		if m.Tipo != "sistema" && strings.TrimSpace(m.Corpo) != "" {
			validas++
		}
	}

	var sb strings.Builder
	if indiceDesc >= 0 {
		fmt.Fprintf(&sb, "[descrição] %s: %s\n\n",
			mensagens[indiceDesc].Autor, limitarTexto(mensagens[indiceDesc].Corpo, maxRunasDescricaoTicket))
	}
	if indiceUltima >= 0 && indiceUltima != indiceDesc {
		m := mensagens[indiceUltima]
		titulo := m.Tipo
		if titulo == "descricao" {
			titulo = "descrição"
		}
		fmt.Fprintf(&sb, "[última interação] %s: %s\n\n",
			m.Autor, limitarTexto(m.Corpo, maxRunasInteracaoTicket))
	}
	resumo := sb.String()
	if r := []rune(resumo); len(r) > maxRunasResumoTicket {
		resumo = string(r[:maxRunasResumoTicket]) + "…\n"
	}
	incluidas := 1
	if indiceUltima >= 0 && indiceUltima != indiceDesc {
		incluidas = 2
	}
	if validas > incluidas {
		resumo += fmt.Sprintf("(+%d interação(ões) anteriores no ticket no 360)\n", validas-incluidas)
	}
	return resumo
}

// autorSessao360 é o nome do usuário logado no 360, configurado uma vez em
// ⚙ Configurações; sem ele, o resumo usa o responsável do ticket como
// aproximação de "quem responde pelo app".
func (a *App) autorSessao360() string {
	if n := strings.TrimSpace(a.configGet(a.ctx, cfgAutor360)); n != "" {
		return n
	}
	return ""
}

// mensagemTicketDetalhe monta o briefing inicial do chat: identificação do
// ticket, o que ele pede, o último recado do cliente e a instrução de puxar
// do 360 só os dados que faltarem para formular a resposta.
func mensagemTicketDetalhe(det *rota360.TicketDetalhe360, autorSessao string) string {
	var sb strings.Builder
	assunto := det.AssuntoNome
	if assunto == "" {
		assunto = det.CategoriaNome
	}
	fmt.Fprintf(&sb, "Analise o ticket #%s (%s) do cliente %s (código service %s), assunto %s.\n\n",
		det.Numero, det.Titulo, det.Solicitante, det.CodService, assunto)
	sb.WriteString(resumoMensagensTicket(det.Mensagens, autorSessao))
	sb.WriteString("A partir do que o ticket pede, decida quais dados do cliente faltam para formular a resposta ")
	sb.WriteString("e consulte SÓ isso com as ferramentas (financeiro, equipamentos, atendimentos — cada uma é separada). ")
	sb.WriteString("Depois resuma o caso e diga os próximos passos.")
	return sb.String()
}

// primeiroCodigo devolve o primeiro código numérico isolado do texto
// (não pode estar colado em letra, para não pegar pedaços de palavras).
// "360" é pulado: é o nome do sistema (domínio/título da janela), não código.
func primeiroCodigo(s string) string {
	for _, m := range regexCodigo.FindAllString(s, -1) {
		if m == "360" {
			continue
		}
		if _, err := strconv.ParseInt(m, 10, 64); err == nil {
			return m
		}
	}
	return ""
}

// codigoDoTicketNaFila procura o uuid na fila de tickets do 360 e devolve
// o código service do cliente. Melhor esforço: sem sessão ou fora da
// primeira página da fila, devolve vazio.
func (a *App) codigoDoTicketNaFila(uuid string) string {
	c := a.rotaAtual()
	if c == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	for _, view := range []string{"em_aberto", "meus_em_aberto"} {
		itens, _, err := c.TicketsDaFila(ctx, view, 1, 50)
		if err != nil {
			return ""
		}
		for _, t := range itens {
			if strings.EqualFold(t.ID, uuid) && t.CodigoService != "" {
				return t.CodigoService
			}
		}
	}
	return ""
}

// CapturarTicketDaTela é o binding para o frontend recapturar a tela sob
// demanda (ex.: botão no chat).
func (a *App) CapturarTicketDaTela() TicketDaTela {
	return a.identificarDaTela(tela.Capturar())
}
