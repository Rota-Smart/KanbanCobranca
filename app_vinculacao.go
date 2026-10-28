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

// Comando /vincular: sem IA, varre os cards do board que não têm NENHUM
// ticket do 360 vinculado e liga a cada um o ticket mais recente do cliente
// na fila do helpdesk. É correção de carga para a época em que o vínculo
// automático ainda não existia — roda uma vez, a pedido, direto do chat.

// CardVinculadoAgora é um card que ganhou vínculo na varredura.
type CardVinculadoAgora struct {
	CardID       int64  `json:"cardId"`
	Cliente      string `json:"cliente"`
	TicketNumero string `json:"ticketNumero"`
	Titulo       string `json:"titulo"`
}

// ResultadoVinculacao resume o disparo do /vincular.
type ResultadoVinculacao struct {
	Varridos   int                  `json:"varridos"`   // cards sem vínculo examinados
	Vinculados []CardVinculadoAgora `json:"vinculados"`
	SemTicket  []string             `json:"semTicket"` // clientes sem ticket na fila aberta
	Erros      []string             `json:"erros"`
}

// VincularTicketsPendentes liga a cada card sem vínculo o ticket mais
// recente do cliente no 360. O casamento é pelo código service (exato) e,
// sem código, pelo nome do cliente contido no solicitante do ticket (ou
// vice-versa). Empate fica com o ticket de abertura mais recente.
func (a *App) VincularTicketsPendentes() (ResultadoVinculacao, error) {
	c := a.rotaAtual()
	if c == nil {
		return ResultadoVinculacao{}, rota360.ErrSemSessao
	}
	ctx, cancel := context.WithTimeout(a.ctx, 120*time.Second)
	defer cancel()

	// Fila inteira uma vez só (as três views, sem repetição) — consultar o
	// 360 por card estouraria o tempo com board grande.
	filaMap := map[string]rota360.Ticket360{}
	filaPorCod := map[string]rota360.Ticket360{}
	falhasView := 0
	for _, view := range []string{"em_aberto", "meus_em_aberto", "pendentes"} {
		if err := ctx.Err(); err != nil {
			return ResultadoVinculacao{}, err
		}
		itens, _, err := c.TicketsDaFila(ctx, view, 1, 100)
		if err != nil {
			falhasView++
			continue // uma view pode falhar (permissão) — segue com as outras
		}
		for _, t := range itens {
			filaMap[t.ID] = t
			if t.CodigoService != "" {
				if antigo, ok := filaPorCod[t.CodigoService]; !ok || criadoDepois(&t, &antigo) {
					filaPorCod[t.CodigoService] = t
				}
			}
		}
	}
	if falhasView == 3 {
		return ResultadoVinculacao{}, fmt.Errorf("não consegui ler a fila do 360 (as 3 views falharam) — sessão do 360 pode ter vencido, reconecte em Configurações")
	}
	fila := make([]rota360.Ticket360, 0, len(filaMap))
	for _, t := range filaMap {
		fila = append(fila, t)
	}

	ts, err := a.tickets.List(a.ctx)
	if err != nil {
		return ResultadoVinculacao{}, err
	}
	comVinculo := map[int64]bool{}
	if a.vinculos != nil {
		if vs, err := a.vinculos.ListTodos(a.ctx); err == nil {
			for _, v := range vs {
				comVinculo[v.TicketID] = true
			}
		}
	}

	res := ResultadoVinculacao{Vinculados: []CardVinculadoAgora{}, SemTicket: []string{}, Erros: []string{}}
	codCache := map[string]string{} // nome normalizado → código service resolvido no 360
	erroBusca := ""
	for _, t := range ts {
		if comVinculo[t.ID] || strings.TrimSpace(t.Ticket360ID) != "" {
			continue // restrição única: só cards sem NENHUM ticket vinculado
		}
		res.Varridos++
		tk := escolherTicketParaCard(t, fila)
		if tk == nil {
			// Nome não casou com o solicitante: resolve o código service do
			// cliente na busca do 360 e procura a fila por código — o
			// solicitante do ticket costuma ser uma pessoa, não a empresa.
			if cod := a.resolverCodService(ctx, c, t.NomeCliente, codCache, &erroBusca); cod != "" {
				if t2, ok := filaPorCod[cod]; ok {
					tk = &t2
				}
			}
		}
		if tk == nil {
			res.SemTicket = append(res.SemTicket, t.NomeCliente)
			continue
		}
		if _, _, err := a.vincularTicket360(t.ID, "#"+tk.Numero); err != nil {
			res.Erros = append(res.Erros, fmt.Sprintf("%s: falhou ao vincular o ticket #%s (%v)", t.NomeCliente, tk.Numero, err))
			continue
		}
		res.Vinculados = append(res.Vinculados, CardVinculadoAgora{
			CardID:       t.ID,
			Cliente:      t.NomeCliente,
			TicketNumero: tk.Numero,
			Titulo:       tk.Titulo,
		})
	}
	if erroBusca != "" && len(res.SemTicket) > 0 {
		res.Erros = append(res.Erros, "busca de clientes no 360 falhou (pode ter mascarado tickets em aberto): "+erroBusca)
	}
	return res, nil
}

// escolherTicketParaCard acha na fila o melhor ticket para o card: primeiro
// por código service exato; sem código (ou sem match), pelo nome do cliente
// contra o solicitante do ticket nos dois sentidos. nil = nenhum candidato.
func escolherTicketParaCard(card db.Ticket, fila []rota360.Ticket360) *rota360.Ticket360 {
	var porCod, porNome *rota360.Ticket360
	nome := service.NormalizarTexto(card.NomeCliente)
	for i := range fila {
		t := &fila[i]
		if card.CodService != "" && t.CodigoService == card.CodService {
			if porCod == nil || criadoDepois(t, porCod) {
				porCod = t
			}
			continue
		}
		if nome == "" || len([]rune(nome)) < 4 {
			continue // nome curto contido em qualquer solicitante: falso positivo
		}
		sol := service.NormalizarTexto(t.Solicitante)
		if sol == "" {
			continue
		}
		if strings.Contains(sol, nome) || strings.Contains(nome, sol) {
			if porNome == nil || criadoDepois(t, porNome) {
				porNome = t
			}
		}
	}
	if porCod != nil {
		return porCod
	}
	return porNome
}

// criadoDepois compara os carimbos de abertura (ISO 8601 do 360: a ordem de
// string já é cronológica). Empate ou formato estranho: novo não vence.
func criadoDepois(a, b *rota360.Ticket360) bool {
	return a.CriadoEm > b.CriadoEm
}

// resolverCodService acha o código service do cliente na busca do 360 pelo
// nome do card (com cache por nome — a busca é a mesma para cards homônimos).
// Só aceita resultado cujo nome confira com o do card nos dois sentidos; se
// a busca falhar uma vez, guarda o erro e não insiste nos próximos cards.
func (a *App) resolverCodService(ctx context.Context, c *rota360.Cliente, nome string, cache map[string]string, erroBusca *string) string {
	chave := service.NormalizarTexto(nome)
	if cod, ok := cache[chave]; ok {
		return cod
	}
	cache[chave] = "" // padrão: não achou
	if *erroBusca != "" {
		return ""
	}
	cls, err := c.BuscarClientes(ctx, nome, "")
	if err != nil {
		*erroBusca = err.Error()
		return ""
	}
	alvo := service.NormalizarTexto(nome)
	if alvo == "" {
		return ""
	}
	for _, cl := range cls {
		achado := service.NormalizarTexto(cl.Nome)
		if achado == "" {
			continue
		}
		if strings.Contains(achado, alvo) || strings.Contains(alvo, achado) {
			cache[chave] = fmt.Sprintf("%d", cl.CodCliente)
			break
		}
	}
	return cache[chave]
}

// FechadoHistorico é um card no histórico da coluna Ticket Fechado.
type FechadoHistorico struct {
	CardID       int64  `json:"cardId"`
	Cliente      string `json:"cliente"`
	TicketNumero string `json:"ticketNumero"`
	MovidoEm     string `json:"movidoEm"`
}

// HistoricoTicketsFechados devolve todos os cards já movidos para a coluna
// Ticket Fechado, do mais recente para o mais antigo — alimenta o botão de
// histórico da coluna (que exibe só os 5 últimos).
func (a *App) HistoricoTicketsFechados() ([]FechadoHistorico, error) {
	ms, err := a.tickets.HistoricoFechados(a.ctx, ColunaTicketFechado)
	if err != nil {
		return nil, friendly("HistoricoTicketsFechados", err)
	}
	out := make([]FechadoHistorico, 0, len(ms))
	for _, m := range ms {
		out = append(out, FechadoHistorico(m))
	}
	return out, nil
}
