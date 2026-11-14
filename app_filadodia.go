package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	"rota-smart/internal/service"
)

// Fila do dia: visão única de manhã com o que merece ação hoje — cards parados
// há muito em colunas ativas, acordos com prazo chegando e orçamentos sem
// resposta. O frontend junta com as visitas de hoje e monta o painel do topo.
//
// Visitas de hoje ficam no frontend (já vinham do board); as três seções abaixo
// precisam de banco, então são calculadas aqui.

// FilaItem é uma linha de qualquer seção da fila do dia.
type FilaItem struct {
	TicketID int64  `json:"ticketId"`
	Cliente  string `json:"cliente"`
	Coluna   string `json:"coluna"`
	// Detalhe é o texto humano da linha ("parado há 8 dias em Em contato",
	// "acordo vence em 2 dias", "orçamento enviado há 6 dias sem resposta").
	Detalhe string `json:"detalhe"`
}

// FilaDoDiaResult agrupa as seções calculadas no banco local.
type FilaDoDiaResult struct {
	Parados    []FilaItem `json:"parados"`
	Acordos    []FilaItem `json:"acordos"`
	Orcamentos []FilaItem `json:"orcamentos"`
}

const filaDiasPadrao = 5

// filaVazio devolve o resultado com slices vazios (não nulos) para o JSON do
// frontend nunca ter que lidar com null.
func filaVazio() FilaDoDiaResult {
	return FilaDoDiaResult{Parados: []FilaItem{}, Acordos: []FilaItem{}, Orcamentos: []FilaItem{}}
}

// colunaAtivaFila: colunas finalizadas não entram na fila de parados — o card
// ali já virou história (visita realizada) ou encerrou (Ticket Fechado).
func colunaAtivaFila(coluna string) bool {
	return coluna != ColunaTicketFechado && !ehColunaVisitaRealizadaFila(coluna)
}

// ehColunaVisitaRealizadaFila reconhece "Visita Realizada" e variações —
// mesma regra usada ao mover o card (service.EhColunaVisitaRealizada).
func ehColunaVisitaRealizadaFila(coluna string) bool {
	return service.EhColunaVisitaRealizada(coluna)
}

// FilaDoDia calcula as três seções de follow-up. diasParado <= 0 usa o padrão
// (5 dias). Tudo é leitura — nada é gravado aqui.
func (a *App) FilaDoDia(diasParado int) (FilaDoDiaResult, error) {
	res := filaVazio()
	if a.db == nil {
		return res, fmt.Errorf("banco indisponível nesta instalação")
	}
	if diasParado <= 0 {
		diasParado = filaDiasPadrao
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	agora := time.Now().UTC()

	// --- Parados há muito -----------------------------------------------
	// Consultas separadas (o LEFT JOIN com movimentacoes fica instável no
	// driver sqlite — ver TempoPorColuna): o último toque do card é o
	// created_at da movimentação mais recente, ou a criação do card se ele
	// nunca se moveu.
	ultimoToque := map[int64]time.Time{}
	clientes := map[int64]string{}
	colunaDo := map[int64]string{}
	linhasTickets, err := a.db.QueryContext(ctx,
		`SELECT id, nome_cliente, coluna_atual, created_at FROM tickets`)
	if err != nil {
		return res, fmt.Errorf("FilaDoDia: %w", err)
	}
	for linhasTickets.Next() {
		var id int64
		var cliente, coluna, criado string
		if err := linhasTickets.Scan(&id, &cliente, &coluna, &criado); err != nil {
			linhasTickets.Close()
			return res, fmt.Errorf("FilaDoDia: %w", err)
		}
		clientes[id] = cliente
		colunaDo[id] = coluna
		if !colunaAtivaFila(coluna) {
			continue
		}
		if t, ok := parseCarimbo(criado); ok {
			ultimoToque[id] = t
		}
	}
	if err := linhasTickets.Close(); err != nil && err != linhasTickets.Err() {
		return res, fmt.Errorf("FilaDoDia: %w", err)
	}
	if err := linhasTickets.Err(); err != nil {
		return res, fmt.Errorf("FilaDoDia: %w", err)
	}

	linhasMov, err := a.db.QueryContext(ctx,
		`SELECT ticket_id, MAX(created_at) FROM movimentacoes GROUP BY ticket_id`)
	if err != nil {
		return res, fmt.Errorf("FilaDoDia: %w", err)
	}
	for linhasMov.Next() {
		var id int64
		var movEm string
		if err := linhasMov.Scan(&id, &movEm); err != nil {
			linhasMov.Close()
			return res, fmt.Errorf("FilaDoDia: %w", err)
		}
		if t, ok := parseCarimbo(movEm); ok {
			if _, ainda := ultimoToque[id]; ainda {
				ultimoToque[id] = t
			}
		}
	}
	linhasMov.Close()

	for id, toque := range ultimoToque {
		dias := int(agora.Sub(toque).Hours() / 24)
		if dias < diasParado {
			continue
		}
		res.Parados = append(res.Parados, FilaItem{
			TicketID: id,
			Cliente:  clientes[id],
			Coluna:   colunaDo[id],
			Detalhe:  fmt.Sprintf("sem movimento há %d dia(s) — último toque %s", dias, toque.Format("02/01")),
		})
	}
	sort.Slice(res.Parados, func(i, j int) bool {
		return res.Parados[i].Detalhe > res.Parados[j].Detalhe // mais dias primeiro
	})

	// --- Acordos com prazo chegando -------------------------------------
	// Acordo ativo: cobrancas com ativa = 1 e ainda não concluída. O vencimento
	// vem de data_vencimento quando existe; como a tabela não tem campo de data
	// da 1ª parcela/próxima ação, sem data_vencimento estimamos criação + 30
	// dias (prazo típico de um acordo em até 3x). Acende com <= 3 dias ou
	// vencido.
	linhasAc, err := a.db.QueryContext(ctx, `
		SELECT c.ticket_id, t.nome_cliente, t.coluna_atual,
		       COALESCE(c.data_vencimento, ''), COALESCE(c.created_at, '')
		FROM cobrancas c JOIN tickets t ON t.id = c.ticket_id
		WHERE c.ativa = 1 AND c.status != 'concluida'`)
	if err != nil {
		return res, fmt.Errorf("FilaDoDia: %w", err)
	}
	defer linhasAc.Close()
	for linhasAc.Next() {
		var id int64
		var cliente, coluna, venc, criado string
		if err := linhasAc.Scan(&id, &cliente, &coluna, &venc, &criado); err != nil {
			return res, fmt.Errorf("FilaDoDia: %w", err)
		}
		vencimento, ok := parseDataFila(venc)
		if !ok {
			vencimento, ok = parseCarimbo(criado)
			if ok {
				vencimento = vencimento.AddDate(0, 0, 30) // estimativa: acordo criado + 30 dias
			}
		}
		if !ok {
			continue // sem como estimar o prazo; não acende falso alarme
		}
		dias := int(time.Until(vencimento).Hours() / 24)
		if dias > 3 {
			continue
		}
		detalhe := fmt.Sprintf("acordo vence em %d dia(s) — %s", dias, vencimento.Format("02/01"))
		if dias < 0 {
			detalhe = fmt.Sprintf("acordo vencido há %d dia(s) — %s", -dias, vencimento.Format("02/01"))
		}
		res.Acordos = append(res.Acordos, FilaItem{
			TicketID: id,
			Cliente:  cliente,
			Coluna:   coluna,
			Detalhe:  detalhe,
		})
	}
	sort.Slice(res.Acordos, func(i, j int) bool { return res.Acordos[i].Detalhe < res.Acordos[j].Detalhe })

	// --- Orçamentos sem resposta ----------------------------------------
	// Orçamento conta como respondido quando o card sai da fila: foi para
	// coluna finalizada, virou acordo ativo ou o orçamento foi gerado de novo
	// (carimbo recente reinicia a contagem). Envio há mais de 3 dias acende.
	acordosAtivos := map[int64]bool{}
	linhasAc2, err := a.db.QueryContext(ctx,
		`SELECT DISTINCT ticket_id FROM cobrancas WHERE ativa = 1 AND status != 'concluida'`)
	if err != nil {
		return res, fmt.Errorf("FilaDoDia: %w", err)
	}
	for linhasAc2.Next() {
		var id int64
		if err := linhasAc2.Scan(&id); err != nil {
			linhasAc2.Close()
			return res, fmt.Errorf("FilaDoDia: %w", err)
		}
		acordosAtivos[id] = true
	}
	linhasAc2.Close()

	linhasOrc, err := a.db.QueryContext(ctx, `
		SELECT id, nome_cliente, coluna_atual, orcamento_gerado_em
		FROM tickets WHERE orcamento_gerado_em != ''`)
	if err != nil {
		return res, fmt.Errorf("FilaDoDia: %w", err)
	}
	defer linhasOrc.Close()
	for linhasOrc.Next() {
		var id int64
		var cliente, coluna, geradoEm string
		if err := linhasOrc.Scan(&id, &cliente, &coluna, &geradoEm); err != nil {
			return res, fmt.Errorf("FilaDoDia: %w", err)
		}
		if !colunaAtivaFila(coluna) || acordosAtivos[id] {
			continue // resolveu: fechou ou virou acordo
		}
		gerado, ok := parseCarimbo(geradoEm)
		if !ok {
			continue
		}
		dias := int(agora.Sub(gerado).Hours() / 24)
		if dias <= 3 {
			continue
		}
		res.Orcamentos = append(res.Orcamentos, FilaItem{
			TicketID: id,
			Cliente:  cliente,
			Coluna:   coluna,
			Detalhe:  fmt.Sprintf("orçamento enviado há %d dia(s) sem resposta — %s", dias, gerado.Format("02/01")),
		})
	}
	sort.Slice(res.Orcamentos, func(i, j int) bool { return res.Orcamentos[i].Detalhe < res.Orcamentos[j].Detalhe })

	return res, nil
}

// parseDataFila entende tanto data pura (AAAA-MM-DD, como data_vencimento) que
// quanto carimbo completo.
func parseDataFila(s string) (time.Time, bool) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, true
	}
	return parseCarimbo(s)
}
