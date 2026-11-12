package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"rota-smart/internal/db"
	"rota-smart/internal/rota360"
)

// ColunaTicketFechado é o destino dos cards cujos tickets do 360 fecharam.
const ColunaTicketFechado = "Ticket Fechado"

// CardFechado é um card movido pela verificação, com o porquê.
type CardFechado struct {
	CardID       int64  `json:"cardId"`
	Cliente      string `json:"cliente"`
	TicketNumero string `json:"ticketNumero"`
	Motivo       string `json:"motivo"` // status no 360 ou "saiu da fila"
}

// ResultadoVerificacao resume o disparo do botão de tickets fechados.
type ResultadoVerificacao struct {
	Verificados int           `json:"verificados"` // tickets únicos consultados no 360
	Fechados    int           `json:"fechados"`    // tickets detectados como fechados
	Movidos     []CardFechado `json:"movidos"`
	Ficaram     int           `json:"ficaram"`   // cards com ticket ainda aberto
	Ignorados   int           `json:"ignorados"` // já fechados antes ou na coluna — a verificação não desfaz o que o usuário moveu
	Erros       []string      `json:"erros"`
	Coluna      string        `json:"coluna"`
}

// cardsQueNaoMexer junta os cards que a verificação de fechados não pode
// desfazer: quem já está na coluna de fechados agora e quem já passou por ela
// alguma vez (movimentações) — se o usuário arrastou um desses de volta para
// outra coluna, foi decisão manual e o card fica onde está. Também protege os
// cards em colunas de negociação: mesmo com todos os tickets fechados no
// 360, card em Aguardando Assinatura/Pagamento fica onde está — assinatura e
// pagamento pendentes significam negócio vivo. O casamento é por palavra-
// chave (contém "assinatura" ou "pagamento", sem olhar caixa/acento): as
// colunas do quadro são dinâmicas e variações do nome ("Aguardando assinatura
// do contrato") escapavam da comparação exata e eram fechadas.
func colunaNegociacaoFechado(col string) bool {
	s := strings.ToLower(strings.TrimSpace(col))
	return strings.Contains(s, "assinatura") || strings.Contains(s, "pagamento")
}

func cardsQueNaoMexer(ts []db.Ticket, hist []db.MovimentoFechado) map[int64]bool {
	naoMexer := map[int64]bool{}
	for _, t := range ts {
		if t.ColunaAtual == ColunaTicketFechado {
			naoMexer[t.ID] = true
			continue
		}
		if colunaNegociacaoFechado(t.ColunaAtual) {
			naoMexer[t.ID] = true
		}
	}
	for _, m := range hist {
		naoMexer[m.CardID] = true
	}
	return naoMexer
}

// ticketFechado avalia o status do 360: Resolvido, Fechado, Concluído,
// Cancelado/Encerrado contam como fechado; qualquer outra coisa, aberto.
func ticketFechado(status string) (bool, string) {
	s := strings.ToLower(strings.TrimSpace(status))
	switch {
	case s == "":
		return false, ""
	case strings.Contains(s, "resolv"),
		strings.Contains(s, "fechad"),
		strings.Contains(s, "conclu"),
		strings.Contains(s, "cancel"),
		strings.Contains(s, "encerr"):
		return true, status
	}
	return false, status
}

// VerificarTicketsFechados varre todos os vínculos card↔ticket, consulta o
// status de cada ticket no 360 (sem IA, disparo direto do app) e move para a
// coluna Ticket Fechado os cards em que TODOS os tickets fecharam — status
// Resolvido/Fechado/etc. ou ticket que saiu da fila (404).
func (a *App) VerificarTicketsFechados() (ResultadoVerificacao, error) {
	c := a.rotaAtual()
	if c == nil {
		return ResultadoVerificacao{}, rota360.ErrSemSessao
	}
	ctx, cancel := context.WithTimeout(a.ctx, 90*time.Second)
	defer cancel()

	res := ResultadoVerificacao{Movidos: []CardFechado{}, Erros: []string{}, Coluna: ColunaTicketFechado}

	// Garante a coluna de destino (board é dinâmico); o Upgrade permanece
	// a última coluna mesmo se a Ticket Fechado for criada depois dele.
	if ok, err := a.colunas.Existe(ctx, ColunaTicketFechado); err == nil && !ok {
		if _, err := a.colunas.Add(ctx, ColunaTicketFechado); err != nil {
			return res, fmt.Errorf("não consegui criar a coluna %s: %w", ColunaTicketFechado, err)
		}
		a.garantirUpgradeUltimo(ctx)
	}

	vs, err := a.vinculos.ListTodos(ctx)
	if err != nil {
		return res, err
	}

	// Tickets do board para nome do card e para saber em que coluna está.
	var ts []db.Ticket
	var hist []db.MovimentoFechado
	tickets := map[int64]string{}
	if aux, err := a.tickets.List(ctx); err == nil {
		ts = aux
		for _, t := range ts {
			tickets[t.ID] = t.NomeCliente
		}
	}
	if aux, err := a.tickets.HistoricoFechados(a.ctx, ColunaTicketFechado); err == nil {
		hist = aux
	}

	// Cards que a verificação não pode desfazer: quem já está na coluna de
	// fechados e quem já passou por ela alguma vez (registro em movimentações).
	// Se o usuário arrastou um desses cards de volta para outra coluna, foi
	// decisão manual — a verificação respeita e deixa onde está.
	naoMexer := cardsQueNaoMexer(ts, hist)
	ignorados := map[int64]bool{}
	ativos := make([]db.Vinculo360, 0, len(vs))
	for _, v := range vs {
		if naoMexer[v.TicketID] {
			ignorados[v.TicketID] = true
			continue
		}
		ativos = append(ativos, v)
	}
	res.Ignorados = len(ignorados)
	vs = ativos
	if len(vs) == 0 {
		return res, nil
	}

	// Consulta o status de cada ticket único no 360, com paralelismo limitado.
	type veredito struct {
		fechado bool
		motivo  string
		err     error
	}
	unicos := map[string]*veredito{}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	var sessaoMorta atomic.Bool
	for _, v := range vs {
		if _, ja := unicos[v.Ticket360ID]; ja {
			continue
		}
		vd := &veredito{}
		unicos[v.Ticket360ID] = vd
		wg.Add(1)
		sem <- struct{}{}
		go func(uuid string, vd *veredito) {
			defer wg.Done()
			defer func() { <-sem }()
			det, err := c.DetalheTicket(ctx, uuid)
			if err != nil {
				// 404 = ticket saiu da fila: conta como fechado. Sessão
				// expirada aborta a varredura (as outras consultas também
				// vão falhar). Outros erros (rede) não movem card — vão
				// para a lista de erros.
				if strings.Contains(strings.ToLower(fmt.Sprint(err)), "http 404") {
					vd.fechado, vd.motivo = true, "saiu da fila de tickets"
				} else if errors.Is(err, rota360.ErrSessaoExpirada) {
					vd.err = err
					sessaoMorta.Store(true)
					cancel() // interrompe as consultas em andamento
				} else {
					vd.err = err
				}
				return
			}
			vd.fechado, vd.motivo = ticketFechado(det.Status)
			if !vd.fechado {
				vd.motivo = det.Status
			}
		}(v.Ticket360ID, vd)
	}
	wg.Wait()
	if sessaoMorta.Load() {
		return res, fmt.Errorf("sessão do 360 expirada — nenhum ticket foi consultado; reconecte em Configurações e tente de novo")
	}

	// Card só se move quando TODOS os tickets vinculados estão fechados.
	porCard := map[int64][]*veredito{}
	numeros := map[int64]string{}
	for _, v := range vs {
		porCard[v.TicketID] = append(porCard[v.TicketID], unicos[v.Ticket360ID])
		if v.Ticket360Numero != "" {
			numeros[v.TicketID] = v.Ticket360Numero
		}
	}
	for cardID, vds := range porCard {
		for _, vd := range vds {
			if vd.err != nil {
				res.Erros = append(res.Erros, fmt.Sprintf("card %d: não consegui consultar o ticket (%v)", cardID, vd.err))
				continue
			}
			res.Verificados++
		}
		if len(vds) == 0 {
			continue
		}
		todosFechados := true
		motivo := ""
		algumFechado := false
		for _, vd := range vds {
			if vd.err != nil {
				todosFechados = false
				continue
			}
			if vd.fechado {
				algumFechado = true
				if motivo == "" {
					motivo = vd.motivo
				}
			} else {
				todosFechados = false
			}
		}
		if algumFechado && todosFechados {
			num := numeros[cardID]
			if err := a.MoveTicket(cardID, ColunaTicketFechado, a.contagemColuna(ColunaTicketFechado)); err != nil {
				res.Erros = append(res.Erros, fmt.Sprintf("card %d: falhou ao mover para %s (%v)", cardID, ColunaTicketFechado, err))
				continue
			}
			res.Fechados++
			res.Movidos = append(res.Movidos, CardFechado{
				CardID:       cardID,
				Cliente:      tickets[cardID],
				TicketNumero: num,
				Motivo:       motivo,
			})
		} else {
			res.Ficaram++
		}
	}
	return res, nil
}
