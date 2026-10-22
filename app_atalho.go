package main

import (
	"context"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"rota-smart/internal/hotkey"
	"rota-smart/internal/tela"
)

// Eventos Wails emitidos quando um atalho global dispara (o frontend
// escuta via EventsOn e abre o chat do Groq ou a busca de clientes).
const (
	evAtalhoTela  = "atalho:tela"
	evAtalhoBusca = "atalho:busca"
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

// iniciarAtalhos registra os atalhos globais em goroutine dedicada —
// a thread do hotkey precisa viver com o loop de mensagens até o fim.
func (a *App) iniciarAtalhos() {
	go func() {
		if err := hotkey.Registrar(a.onAtalhoTicketTela, a.onAtalhoBusca); err != nil {
			slog.Warn("atalhos globais indisponíveis", "err", err)
		}
	}()
}

// mostrarJanela traz o app para frente (minimizado ou coberto).
func (a *App) mostrarJanela() {
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
}

// onAtalhoBusca (Shift+F3): abre o app na busca de clientes.
func (a *App) onAtalhoBusca() {
	a.mostrarJanela()
	runtime.EventsEmit(a.ctx, evAtalhoBusca)
}

// onAtalhoTicketTela (Shift+F2): identifica o ticket/cliente na janela em
// foco, traz o app para frente e emite o evento para o chat do Groq.
func (a *App) onAtalhoTicketTela() {
	cap := tela.Capturar()
	dado := a.identificarDaTela(cap)
	slog.Info("atalho shift+f2", "processo", cap.Processo, "achou", dado.Achou,
		"fonte", dado.Fonte, "cod", dado.CodService != "")
	a.mostrarJanela()
	runtime.EventsEmit(a.ctx, evAtalhoTela, dado)
}

// regexUUID captura ids de ticket no estilo Octadesk/360 na URL.
var regexUUID = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// regexCodigo procura um código numérico curto (cod service / nº do ticket).
var regexCodigo = regexp.MustCompile(`\d{3,10}`)

// identificarDaTela deduz qual cliente/ticket estava na tela:
//  1. código numérico na URL ou no título da janela → card local;
//  2. uuid de ticket na URL → acha o código service na fila do 360;
//  3. texto recém-copiado (clipboard) como último recurso.
func (a *App) identificarDaTela(cap tela.Captura) TicketDaTela {
	res := TicketDaTela{Titulo: cap.Titulo}

	urlSemUUID := regexUUID.ReplaceAllString(cap.URL, " ")
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
		}
	}

	if res.CodService == "" {
		res.Mensagem = "Não reconheci um ticket ou cliente na tela em foco. Cole o código ou peça no chat."
		return res
	}
	if t, err := a.tickets.GetPorCodService(a.ctx, res.CodService); err == nil && t != nil {		res.Achou = true
		res.TicketID = t.ID
		res.NomeCliente = t.NomeCliente
	}
	return res
}

// primeiroCodigo devolve o primeiro código numérico isolado do texto
// (não pode estar colado em letra, para não pegar pedaços de palavras).
func primeiroCodigo(s string) string {
	for _, m := range regexCodigo.FindAllString(s, -1) {
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
