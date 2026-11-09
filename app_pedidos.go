package main

// Fila de pedidos do site para o app: o site grava em /pedidos, este app
// busca (junto de cada ciclo do sync da conta — e o aviso do /eventos acorda
// o ciclo na hora), abre o chat do assistente com o ticket no contexto e
// marca o pedido como atendido. App fechado: o pedido espera na fila.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"rota-smart/internal/contasync"
)

// evPedidoSite: o frontend escuta e abre o chat igual ao Shift+F2.
const evPedidoSite = "pedidos:site"

// PedidoSite é o recado do site (espelha repo.Pedido da API na nuvem).
type PedidoSite struct {
	ID          int64  `json:"id"`
	Tipo        string `json:"tipo"`
	TicketID    string `json:"ticket_id"` // uuid do ticket no 360
	Numero      string `json:"numero"`
	Titulo      string `json:"titulo"`
	CodService  string `json:"codigo_service"`
	NomeCliente string `json:"nome_cliente"`
}

// atenderPedidos busca a fila da conta e atende cada pedido. Chamado no fim
// de cada cicloConta(): com o app conectado, um pedido novo chega em segundos
// (o site avisa pelo /eventos); sem conexão, o erro é só log — o ciclo
// seguinte tenta de novo.
func (a *App) atenderPedidos(token string) {
	pedidos, err := pedidosPendentes(a.ctx, token)
	if err != nil {
		slog.Warn("pedidos do site", "err", err)
		return
	}
	for _, p := range pedidos {
		dado := a.pedidoParaChat(p)
		slog.Info("pedido do site", "id", p.ID, "tipo", p.Tipo, "achou", dado.Achou)
		a.mostrarJanela()
		if a.uiPronta {
			runtime.EventsEmit(a.ctx, evPedidoSite, dado)
		}
		if err := atenderPedidoSite(a.ctx, token, p.ID); err != nil {
			slog.Warn("pedido continua pendente", "id", p.ID, "err", err)
		}
	}
}

// pedidoParaChat monta o briefing do assistente. Com o uuid do ticket, lê o
// ticket inteiro no 360 (mesmo caminho do Shift+F2); sem detalhe, segue com
// o código service do pedido.
func (a *App) pedidoParaChat(p PedidoSite) TicketDaTela {
	res := TicketDaTela{Fonte: "site", CodService: p.CodService, Titulo: p.Titulo}
	if p.Numero != "" {
		res.Titulo = "#" + p.Numero + " " + p.Titulo
	}
	if p.TicketID != "" {
		if det := a.detalheDoTicket(p.TicketID); det != nil {
			res.Achou = true
			res.CodService = det.CodService
			res.NomeCliente = det.Solicitante
			autor := a.autorSessao360()
			if autor == "" {
				autor = det.ResponsavelNome
			}
			res.Mensagem = mensagemTicketDetalhe(det, autor)
			if t, err := a.tickets.GetPorCodService(a.ctx, det.CodService); err == nil && t != nil {
				res.TicketID = t.ID
				res.NomeCliente = t.NomeCliente
			}
			return res
		}
	}
	if res.CodService == "" {
		res.Mensagem = fmt.Sprintf(
			"Chegou do site o pedido para o ticket #%s (%s), mas sem código service e sem detalhe do 360. Peça ao cliente o que falta para identificar o caso.",
			p.Numero, p.Titulo)
		return res
	}
	if t, err := a.tickets.GetPorCodService(a.ctx, res.CodService); err == nil && t != nil {
		res.Achou = true
		res.TicketID = t.ID
		res.NomeCliente = t.NomeCliente
	}
	if res.Achou && res.TicketID > 0 {
		res.Mensagem = fmt.Sprintf(
			"Chegou do site o pedido para analisar o ticket #%s (%s) — card #%d, %s (código service %s). Decida quais dados do cliente faltam e consulte SÓ isso no 360 com as ferramentas (financeiro, equipamentos, atendimentos), revise acordos e sugira os próximos passos.",
			p.Numero, p.Titulo, res.TicketID, res.NomeCliente, res.CodService)
	} else {
		res.Mensagem = fmt.Sprintf(
			"Chegou do site o pedido para analisar o ticket #%s (%s) do cliente de código %s no 360. Decida quais dados dele faltam e consulte SÓ isso com as ferramentas (financeiro, equipamentos, atendimentos), aponte pendências relevantes e sugira os próximos passos.",
			p.Numero, p.Titulo, res.CodService)
	}
	return res
}

func clientePedidos() *http.Client { return &http.Client{Timeout: 20 * time.Second} }

func pedidosPendentes(ctx context.Context, token string) ([]PedidoSite, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, contasync.URLPadrao+"/pedidos/pendentes", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := clientePedidos().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	corpo, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /pedidos/pendentes: %s", strings.TrimSpace(string(corpo)))
	}
	var pedidos []PedidoSite
	if err := json.Unmarshal(corpo, &pedidos); err != nil {
		return nil, err
	}
	return pedidos, nil
}

func atenderPedidoSite(ctx context.Context, token string, id int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		fmt.Sprintf("%s/pedidos/%d", contasync.URLPadrao, id), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := clientePedidos().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("DELETE /pedidos/%d: %d", id, resp.StatusCode)
	}
	return nil
}
