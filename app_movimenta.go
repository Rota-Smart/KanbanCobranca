package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"rota-smart/internal/db"
	"rota-smart/internal/rota360"
	"rota-smart/internal/service"
)

// MovimentarTicket360 é a ação manual (sem IA, sem chat): dado um ticket do
// 360 — número ("12345"), uuid ou URL — e uma coluna do Kanban, busca o
// ticket e o cliente no 360 e coloca o card na coluna. Se o cliente já tem
// card, move o card existente em vez de duplicar; senão importa um novo.
// Usa exatamente as mesmas funções do import manual e do vínculo de ticket.
func (a *App) MovimentarTicket360(entrada, coluna string) (db.Ticket, error) {
	entrada = strings.TrimSpace(entrada)
	coluna = strings.TrimSpace(coluna)
	// Valida contra o board dinâmico (colunas criadas depois, ex.: Ticket
	// Fechado, não estão na lista estática ColunasValidas) e grava o nome
	// canônico — "aguardando assinatura" vira "Aguardando Assinatura".
	if canon, ok := a.colunaCanonica(coluna); ok {
		coluna = canon
	}
	if !a.colunaValida(coluna) {
		return db.Ticket{}, service.ErrColunaInvalida
	}
	uuid, numero := dividirEntradaTicket(entrada)
	if uuid == "" && numero == "" {
		return db.Ticket{}, fmt.Errorf("informe o nº do ticket, o uuid ou a URL do ticket no 360")
	}
	c := a.rotaAtual()
	if c == nil {
		return db.Ticket{}, rota360.ErrSemSessao
	}
	ctx, cancel := context.WithTimeout(a.ctx, 45*time.Second)
	defer cancel()

	if uuid == "" {
		item, err := a.resolverTicket360(ctx, numero)
		if err != nil {
			return db.Ticket{}, fmt.Errorf("ticket #%s não está nas filas em aberto/pendentes do 360", numero)
		}
		uuid = item.ID
	}
	det, err := c.DetalheTicket(ctx, uuid)
	if err != nil {
		return db.Ticket{}, friendly("MovimentarTicket360", err)
	}
	if det.CodService == "" {
		return db.Ticket{}, fmt.Errorf("o ticket #%s não tem código service no 360 — importe o cliente pela busca", det.Numero)
	}

	// Cliente já tem card? Move em vez de duplicar; senão, importa novo.
	existente, err := a.tickets.GetPorCodService(a.ctx, det.CodService)
	if err == nil && existente != nil && existente.ID > 0 {
		if existente.ColunaAtual != coluna {
			if err := a.MoveTicket(existente.ID, coluna, a.contagemColuna(coluna)); err != nil {
				return db.Ticket{}, friendly("MovimentarTicket360", err)
			}
		}
		t, _, err := a.vincularTicket360(existente.ID, uuid)
		if err != nil {
			return db.Ticket{}, friendly("MovimentarTicket360", err)
		}
		return t, nil
	}

	criado, err := a.ImportarCliente360(det.CodService, coluna)
	if err != nil {
		return db.Ticket{}, err
	}
	t, _, err := a.vincularTicket360(criado.ID, uuid)
	if err != nil {
		return criado, friendly("MovimentarTicket360", err)
	}
	return t, nil
}

// executarComandoMove reconhece o comando "/move" do chat e executa direto,
// sem IA: com nº/uuid/URL de ticket roda o mesmo fluxo do Shift+F6 (busca no
// 360, move o card do cliente ou importa um novo e vincula); com nome/código
// move um card que já existe; "/move cobrança" (só a coluna) move o cliente
// que está em discussão no chat — a.msg usa as mensagens recentes da conversa
// para achar o card citado por último. ok=false quando não é o comando (o
// texto segue para o fluxo normal do agente).
func (a *App) executarComandoMove(mensagem string, chatID int64) (*service.ChatResultado, bool) {
	cmd, ok := service.ParseComandoMove(mensagem)
	if !ok {
		return nil, false
	}
	colunas := a.colunasAtivas()
	// "/move cobrança" sem cliente: o corpo é a coluna de destino e o cliente
	// vem do contexto do chat.
	if cmd.Ticket == "" && cmd.Card != "" && cmd.Coluna == "" {
		if coluna, err := service.CasarColuna(cmd.Card, colunas); err == nil {
			cmd.Coluna, cmd.Card = coluna, ""
		}
	}
	if cmd.Ticket == "" && cmd.Card == "" && cmd.Coluna != "" {
		coluna, err := service.CasarColuna(cmd.Coluna, colunas)
		if err != nil {
			return &service.ChatResultado{Resposta: err.Error(), Acoes: []service.AcaoDoAgente{}}, true
		}
		card, ok := a.cardDoContextoChat(chatID)
		if !ok {
			return &service.ChatResultado{
				Resposta: fmt.Sprintf("Não identifiquei o cliente em discussão nesta conversa. Diga: /move <cliente> para %s (ou cite o código service antes).", coluna),
				Acoes:    []service.AcaoDoAgente{},
			}, true
		}
		if err := a.MoveTicket(card.ID, coluna, a.contagemColuna(coluna)); err != nil {
			return &service.ChatResultado{Resposta: "Falhou ao mover: " + err.Error(), Acoes: []service.AcaoDoAgente{}}, true
		}
		return &service.ChatResultado{
			Resposta: fmt.Sprintf("✅ Card %d (%s) movido para **%s** (cliente do contexto do chat).", card.ID, card.NomeCliente, coluna),
			Acoes: []service.AcaoDoAgente{{Ferramenta: "mover_card",
				Resumo: fmt.Sprintf("moveu o card %d (%s) para %s via /move sem cliente", card.ID, card.NomeCliente, coluna)}},
		}, true
	}
	if cmd.Ticket == "" && cmd.Card == "" {
		return &service.ChatResultado{
			Resposta: fmt.Sprintf(
				"Uso: /move <nº do ticket> para <coluna> — ex.: /move 12345 para Cobrança (mesma ação do Shift+F6; aceita nº, uuid ou URL do ticket). Também move um card existente: /move João para Cobrança. Sem cliente, move quem está em discussão na conversa: /move cobrança.\nColunas: %s.",
				strings.Join(colunas, ", ")), Acoes: []service.AcaoDoAgente{},
		}, true
	}
	if cmd.Ticket != "" {
		coluna, err := service.CasarColuna(cmd.Coluna, colunas)
		if err != nil {
			return &service.ChatResultado{Resposta: err.Error(), Acoes: []service.AcaoDoAgente{}}, true
		}
		t, err := a.MovimentarTicket360(cmd.Ticket, coluna)
		if err != nil {
			return &service.ChatResultado{Resposta: "⚠ " + err.Error(), Acoes: []service.AcaoDoAgente{}}, true
		}
		numero := t.Ticket360Numero
		if numero == "" {
			numero = strings.TrimPrefix(cmd.Ticket, "#")
		}
		return &service.ChatResultado{
			Resposta: fmt.Sprintf("✅ Ticket #%s → card %d (%s) na coluna **%s**.", numero, t.ID, t.NomeCliente, t.ColunaAtual),
			Acoes: []service.AcaoDoAgente{{Ferramenta: "movimentar_ticket_360",
				Resumo: fmt.Sprintf("moveu o ticket #%s para %s via /move", numero, t.ColunaAtual)}},
		}, true
	}
	coluna, err := service.CasarColuna(cmd.Coluna, colunas)
	if err != nil {
		return &service.ChatResultado{Resposta: err.Error(), Acoes: []service.AcaoDoAgente{}}, true
	}
	ts, err := a.tickets.List(a.ctx)
	if err != nil {
		return &service.ChatResultado{Resposta: "Não consegui listar os cards: " + err.Error(), Acoes: []service.AcaoDoAgente{}}, true
	}
	cards := make([]service.CardResumo, 0, len(ts))
	for _, t := range ts {
		cards = append(cards, service.CardResumo{ID: t.ID, NomeCliente: t.NomeCliente, CodService: t.CodService})
	}
	card, err := service.ResolverCard(cmd.Card, cards)
	if err != nil {
		return &service.ChatResultado{Resposta: err.Error(), Acoes: []service.AcaoDoAgente{}}, true
	}
	if err := a.MoveTicket(card.ID, coluna, a.contagemColuna(coluna)); err != nil {
		return &service.ChatResultado{Resposta: "Falhou ao mover: " + err.Error(), Acoes: []service.AcaoDoAgente{}}, true
	}
	return &service.ChatResultado{
		Resposta: fmt.Sprintf("✅ Card %d (%s) movido para **%s**.", card.ID, card.NomeCliente, coluna),
		Acoes: []service.AcaoDoAgente{{Ferramenta: "mover_card",
			Resumo: fmt.Sprintf("moveu o card %d (%s) para %s via /move", card.ID, card.NomeCliente, coluna)}},
	}, true
}
