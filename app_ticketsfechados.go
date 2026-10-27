package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

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
	Verificados int          `json:"verificados"` // tickets únicos consultados no 360
	Fechados    int          `json:"fechados"`    // tickets detectados como fechados
	Movidos     []CardFechado `json:"movidos"`
	Ficaram     int          `json:"ficaram"` // cards com ticket ainda aberto
	Erros       []string     `json:"erros"`
	Coluna      string       `json:"coluna"`
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

	// Garante a coluna de destino (board é dinâmico).
	if ok, err := a.colunas.Existe(ctx, ColunaTicketFechado); err == nil && !ok {
		if _, err := a.colunas.Add(ctx, ColunaTicketFechado); err != nil {
			return res, fmt.Errorf("não consegui criar a coluna %s: %w", ColunaTicketFechado, err)
		}
	}

	vs, err := a.vinculos.ListTodos(ctx)
	if err != nil {
		return res, err
	}
	if len(vs) == 0 {
		return res, nil
	}

	// Tickets do board para nome/status do card; um card pode ter vários vínculos.
	tickets := map[int64]string{}
	if ts, err := a.tickets.List(ctx); err == nil {
		for _, t := range ts {
			tickets[t.ID] = t.NomeCliente
		}
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
				// 404 = ticket saiu da fila: conta como fechado. Outros erros
				// (rede/sessão) não movem card — vão para a lista de erros.
				if strings.Contains(strings.ToLower(fmt.Sprint(err)), "http 404") {
					vd.fechado, vd.motivo = true, "saiu da fila de tickets"
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
