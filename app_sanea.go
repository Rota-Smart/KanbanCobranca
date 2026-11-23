package main

import (
	"context"
	"fmt"
	"time"

	"rota-smart/internal/rota360"
)

// Saneamento em massa dos vínculos: a fila do 360 já traz o assunto e o setor
// reais de cada ticket — uma varredura atualiza TODOS os vínculos gravados
// com o título antigo (pré-1.104.0) ou sem setor, sem esperar o app reler
// ticket por ticket. Alimenta o filtro de assunto/setor do board.

// ResultadoSanea resume a varredura.
type ResultadoSanea struct {
	Vistos     int      `json:"vistos"`     // tickets únicos vistos na fila
	Corrigidos int      `json:"corrigidos"` // vínculos alterados
	Erros      []string `json:"erros"`
}

// saneaViews varre as mesmas views da verificação de fechados: juntas cobrem
// os tickets em aberto de todo mundo.
var saneaViews = []string{"em_aberto", "meus_em_aberto", "pendentes"}

// SaneaVinculos360 compara cada vínculo com o que a fila do 360 diz hoje e
// regrava assunto/setor divergentes. Sem sessão do 360, devolve erro claro.
func (a *App) SaneaVinculos360() (ResultadoSanea, error) {
	c := a.rotaAtual()
	if c == nil {
		return ResultadoSanea{Erros: []string{}}, rota360.ErrSemSessao
	}
	ctx, cancel := context.WithTimeout(a.ctx, 120*time.Second)
	defer cancel()

	res := ResultadoSanea{Erros: []string{}}
	vistos := map[string]rota360.Ticket360{}
	for _, view := range saneaViews {
		for page := 1; page <= 20; page++ { // 100 por página = teto 2k por view
			itens, total, err := c.TicketsDaFila(ctx, view, page, 100)
			if err != nil {
				res.Erros = append(res.Erros, fmt.Sprintf("%s p.%d: %v", view, page, err))
				break
			}
			for _, t := range itens {
				vistos[t.ID] = t
			}
			if page*100 >= total || len(itens) == 0 {
				break
			}
		}
	}
	res.Vistos = len(vistos)
	if len(vistos) == 0 {
		return res, nil
	}

	vs, err := a.vinculos.ListTodos(ctx)
	if err != nil {
		return res, err
	}
	for _, v := range vs {
		f, ok := vistos[v.Ticket360ID]
		if !ok {
			continue // ticket não está mais na fila (fechado): não mexe
		}
		mudouAssunto := f.AssuntoNome != "" && f.AssuntoNome != v.Assunto
		mudouSetor := f.SetorNome != "" && f.SetorNome != v.Setor
		if !mudouAssunto && !mudouSetor {
			continue
		}
		if err := a.vinculos.Upsert(ctx, v.TicketID, v.Ticket360ID, v.Ticket360Numero, f.AssuntoNome, f.SetorNome); err != nil {
			res.Erros = append(res.Erros, fmt.Sprintf("ticket %s: %v", v.Ticket360Numero, err))
			continue
		}
		res.Corrigidos++
	}
	return res, nil
}

// ListarAssuntos360 devolve as opções de assunto do helpdesk (lista oficial
// do "novo ticket"), ordenadas — alimenta o filtro do board.
func (a *App) ListarAssuntos360() ([]string, error) {
	c := a.rotaAtual()
	if c == nil {
		return nil, rota360.ErrSemSessao
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	lista, err := c.AssuntosHelpdesk(ctx)
	if err != nil {
		return nil, friendly("ListarAssuntos360", err)
	}
	nomes := []string{}
	for _, x := range lista {
		if x.Ativo && x.Nome != "" {
			nomes = append(nomes, x.Nome)
		}
	}
	return nomes, nil
}

// ListarSetores360 devolve os setores do helpdesk com fila ativa — alimenta
// o filtro de setor do board (mesma lista da tela "novo ticket" do 360).
func (a *App) ListarSetores360() ([]string, error) {
	c := a.rotaAtual()
	if c == nil {
		return nil, rota360.ErrSemSessao
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	grupos, err := c.GruposHelpdesk(ctx)
	if err != nil {
		return nil, friendly("ListarSetores360", err)
	}
	vistos := map[string]bool{}
	nomes := []string{}
	for _, g := range grupos {
		if !g.Ativo || g.Nome == "" || vistos[g.Nome] {
			continue
		}
		vistos[g.Nome] = true
		nomes = append(nomes, g.Nome)
	}
	return nomes, nil
}
