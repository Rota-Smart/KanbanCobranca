package main

import (
	"fmt"
	"strconv"
	"strings"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"rota-smart/internal/db"
	"rota-smart/internal/service"
)

// Comando local "abra o cliente X" / "o ticket 123 tem card?": resolve o card
// no banco local (nome, código service, id ou nº do ticket 360), responde no
// chat e, quando acha UM card, emite o evento evAbrirCard — o frontend abre o
// modal completo. Zero IA, zero rede.
const evAbrirCard = "chat:abrir-card"

// executarIntencaoAbrir roda o pedido de abrir/verificar card. ok=false
// devolve a mensagem ao fluxo normal do chat.
func (a *App) executarIntencaoAbrir(mensagem string, chatID int64) (*service.ChatResultado, bool) {
	in, ok := service.ParseIntencaoAbrir(mensagem)
	if !ok {
		return nil, false
	}
	// "esse cliente possui card aberto?": o alvo é o cliente em discussão
	// na conversa (mesma memória do /apagar e do /realizada).
	if in.Contexto {
		card, achou := a.cardDoContextoChat(chatID)
		if !achou {
			return &service.ChatResultado{
				Resposta: "Não identifiquei o cliente em discussão nesta conversa. Diga o nome: por exemplo, \"o card do João tem card?\".",
				Acoes:    []service.AcaoDoAgente{},
			}, true
		}
		return &service.ChatResultado{
			Resposta: fmt.Sprintf("Sim — Card #%d — %s (coluna %s)", card.ID, card.NomeCliente, card.ColunaAtual),
			Acoes: []service.AcaoDoAgente{{Ferramenta: "abrir_card",
				Resumo: fmt.Sprintf("verificou o card %d (%s, coluna %s) via comando local", card.ID, card.NomeCliente, card.ColunaAtual)}},
		}, true
	}
	ts, err := a.tickets.List(a.ctx)
	if err != nil {
		return &service.ChatResultado{Resposta: "Não consegui listar os cards: " + err.Error(), Acoes: []service.AcaoDoAgente{}}, true
	}
	card, resposta, achou := resolverAbrir(in.Alvo, ts)
	if !achou {
		return &service.ChatResultado{Resposta: resposta + "\nSe o cliente ainda não tem card, peça: \"importe o cliente <nome>\".", Acoes: []service.AcaoDoAgente{}}, true
	}
	if card.ID == 0 {
		// Vários matches: a resposta lista os cards; abrir nenhum.
		return &service.ChatResultado{Resposta: resposta, Acoes: []service.AcaoDoAgente{}}, true
	}
	a.emitirAbrirCard(card.ID)
	return &service.ChatResultado{
		Resposta: resposta,
		Acoes: []service.AcaoDoAgente{{Ferramenta: "abrir_card",
			Resumo: fmt.Sprintf("abriu o card %d (%s, coluna %s) via comando local", card.ID, card.NomeCliente, card.ColunaAtual)}},
	}, true
}

// emitirAbrirCard pede ao frontend a abertura do modal completo do card.
func (a *App) emitirAbrirCard(cardID int64) {
	if a.uiPronta {
		wailsRuntime.EventsEmit(a.ctx, evAbrirCard, cardID)
	}
}

// resolverAbrir casa o alvo com card(s): número = id, código service ou nº do
// ticket 360; texto = nome (substring, sem caixa). Um match abre o card;
// vários devolvem a lista sem abrir (ID 0); nenhum devolve a explicação.
func resolverAbrir(alvo string, ts []db.Ticket) (db.Ticket, string, bool) {
	alvo = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(alvo), "#"))
	if n, err := strconv.ParseInt(alvo, 10, 64); err == nil {
		for _, t := range ts {
			if t.ID == n || t.CodService == alvo || t.Ticket360Numero == alvo {
				return t, formatarCardAchado(t), true
			}
		}
		return db.Ticket{}, fmt.Sprintf("Nenhum card com código/ticket %s no quadro.", alvo), false
	}
	low := strings.ToLower(alvo)
	var achados []db.Ticket
	for _, t := range ts {
		if strings.Contains(strings.ToLower(t.NomeCliente), low) {
			achados = append(achados, t)
		}
	}
	switch len(achados) {
	case 1:
		return achados[0], formatarCardAchado(achados[0]), true
	case 0:
		return db.Ticket{}, fmt.Sprintf("Nenhum card com %q no nome.", alvo), false
	default:
		var sb strings.Builder
		fmt.Fprintf(&sb, "%d cards casam com %q — seja mais específico:", len(achados), alvo)
		for _, t := range achados {
			sb.WriteString("\n" + formatarCardAchado(t))
		}
		return db.Ticket{}, sb.String(), true
	}
}

// formatarCardAchado é a linha de resposta com o card encontrado.
func formatarCardAchado(t db.Ticket) string {
	s := fmt.Sprintf("Card #%d — %s (coluna %s", t.ID, t.NomeCliente, t.ColunaAtual)
	if t.CodService != "" {
		s += ", código " + t.CodService
	}
	if t.Ticket360Numero != "" {
		s += ", ticket 360 #" + t.Ticket360Numero
	}
	return s + ")"
}

// HistoricoAcordos devolve TODOS os acordos salvos do card (ativa e as
// antigas), para o painel de histórico do modal do cliente. Somente leitura.
func (a *App) HistoricoAcordos(ticketID int64) ([]db.Cobranca, error) {
	cs, err := a.cobrancas.History(a.ctx, ticketID)
	if err != nil {
		return nil, friendly("HistoricoAcordos", err)
	}
	if cs == nil {
		cs = []db.Cobranca{}
	}
	return cs, nil
}
