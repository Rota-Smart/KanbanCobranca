// CRM do 360 no app: leitura do funil, troca de etapa e envio do card do
// nosso quadro para o CRM. Tudo manual — disparado por botão/drag do usuário,
// nunca por automação da IA (decisão de projeto 14/11/2026).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"rota-smart/internal/db"
	"rota-smart/internal/janela360"
	"rota-smart/internal/rota360"
	"rota-smart/internal/service"
)

// CrmConfig agrupa a configuração do funil do CRM (um pedido no 360).
type CrmConfig struct {
	Funis        []rota360.FunilCRM       `json:"funis"`
	Etapas       []rota360.EtapaCRM       `json:"etapas"`
	MotivosPerda []rota360.MotivoPerdaCRM `json:"motivosPerda"`
}

// CrmFunis devolve funis + etapas + motivos de perda do CRM.
func (a *App) CrmFunis() (*CrmConfig, error) {
	c := a.rotaAtual()
	if c == nil {
		return nil, rota360.ErrSemSessao
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	f, e, m, err := c.FunisCRM(ctx)
	if err != nil {
		return nil, friendly("CrmFunis", err)
	}
	if f == nil {
		f = []rota360.FunilCRM{}
	}
	if e == nil {
		e = []rota360.EtapaCRM{}
	}
	if m == nil {
		m = []rota360.MotivoPerdaCRM{}
	}
	return &CrmConfig{Funis: f, Etapas: e, MotivosPerda: m}, nil
}

// CrmNegociacoes devolve os cards do funil (status: aberta/pausada/ganha/perdida/todas).
func (a *App) CrmNegociacoes(funilID, status string) (*rota360.PaginaNegociacoes, error) {
	c := a.rotaAtual()
	if c == nil {
		return nil, rota360.ErrSemSessao
	}
	ctx, cancel := context.WithTimeout(a.ctx, 25*time.Second)
	defer cancel()
	p, err := c.NegociacoesCRM(ctx, strings.TrimSpace(funilID), strings.TrimSpace(status))
	if err != nil {
		return nil, friendly("CrmNegociacoes", err)
	}
	return p, nil
}

// CrmMover troca a etapa de uma negociação (arrastar no espelho do funil).
func (a *App) CrmMover(negociacaoID, etapaID string) error {
	c := a.rotaAtual()
	if c == nil {
		return rota360.ErrSemSessao
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	if err := c.MoverNegociacao(ctx, strings.TrimSpace(negociacaoID), strings.TrimSpace(etapaID)); err != nil {
		return friendly("CrmMover", err)
	}
	return nil
}

// CrmAbrirJanela abre o painel do 360 na janela nativa (WebView2 próprio,
// cookie da sessão injetado — sem login). rota é o caminho depois do
// domínio, ex.: "/crm/funil", "/crm/projetos", "/crm/clientes".
func (a *App) CrmAbrirJanela(rota string) (string, error) {
	rota = strings.TrimSpace(rota)
	if rota == "" {
		rota = "/crm/funil"
	}
	if !strings.HasPrefix(rota, "/") {
		rota = "/" + rota
	}
	c := a.rotaAtual()
	if c == nil || !c.Autenticado() {
		return "", rota360.ErrSemSessao
	}
	hc := &http.Cookie{}
	if ck := c.CookieSalva(); ck != nil {
		hc = &http.Cookie{Name: ck.Nome, Value: ck.Valor}
	}
	url := rota360.BaseURL + rota
	// A janela tem loop próprio: roda em goroutine e o binding volta na hora.
	go func() {
		if err := janela360.Abrir("CRM — Rota | 360", url, hc); err != nil {
			slog.Error("janela CRM falhou", "op", "CrmAbrirJanela", "erro", err)
		}
	}()
	return url, nil
}

// CrmEnviarCard leva o card do nosso quadro para o CRM: se o ticket já tem
// card lá (o 360 cria sozinho), só devolve a negociação achada; senão cria
// uma negociação nova no funil/etapa escolhidos.
func (a *App) CrmEnviarCard(ticketID int64, funilID, etapaID string) (*rota360.NegociacaoCRM, error) {
	c := a.rotaAtual()
	if c == nil {
		return nil, rota360.ErrSemSessao
	}
	t, err := a.tickets.Get(a.ctx, ticketID)
	if err != nil {
		return nil, friendly("CrmEnviarCard", err)
	}

	// 1) o 360 já criou card a partir deste ticket? (coluna ticket-card)
	if t.Ticket360ID != "" {
		ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
		corpo, tem, errC := c.CardCrmTicket(ctx, t.Ticket360ID)
		cancel()
		if errC == nil && tem {
			var n rota360.NegociacaoCRM
			if err := decodificaCorpo(corpo, &n); err == nil && n.ID != "" {
				// quer mover para uma etapa escolhida? move o card existente
				if strings.TrimSpace(etapaID) != "" && etapaID != n.EtapaID {
					if err := a.CrmMover(n.ID, etapaID); err != nil {
						return nil, err
					}
					n.EtapaID = etapaID
				}
				return &n, nil
			}
		}
		// sem card ou formato diferente: cria um (segue abaixo)
	}

	// 2) cria a negociação nova com os dados que o card do app tem
	if strings.TrimSpace(funilID) == "" {
		return nil, fmt.Errorf("escolha o funil antes de enviar")
	}
	corpo := map[string]any{
		"funil_id":     strings.TrimSpace(funilID),
		"titulo":       tituloNegociacao(t),
		"cliente_nome": t.NomeCliente,
	}
	if t.Telefone != "" {
		corpo["telefone"] = t.Telefone
	}
	if t.CodService != "" {
		corpo["cod_cliente_service"] = t.CodService
	}
	if strings.TrimSpace(etapaID) != "" {
		corpo["etapa_id"] = strings.TrimSpace(etapaID)
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	n, err := c.CriarNegociacao(ctx, corpo)
	if err != nil {
		return nil, friendly("CrmEnviarCard", err)
	}
	return n, nil
}

// ---- Visitas do CRM → kanban ----

// CrmVisitaItem é uma visita agendada no CRM com o resultado da comparação
// com o kanban: tem card? a data bate?
type CrmVisitaItem struct {
	NegociacaoID string `json:"negociacaoID"`
	Titulo       string `json:"titulo"`
	Cliente      string `json:"cliente"`
	Data         string `json:"data"`          // yyyy-mm-dd no CRM
	Status       string `json:"status"`        // agendada/vencida/realizada
	DiasVencida  int    `json:"diasVencida"`   // visita vencida há N dias
	Unidade      string `json:"unidade"`
	TicketNumero string `json:"ticketNumero"`  // ticket de origem no 360, se veio de ticket
	CardID       int64  `json:"cardID"`        // 0 = sem card no kanban
	NoKanban     bool   `json:"noKanban"`      // card com a mesma data já gravada
}

// ResumoCrmVisitas é o resultado de CrmVisitas.
type ResumoCrmVisitas struct {
	Itens []CrmVisitaItem `json:"itens"`
	// Novas = visitas que o kanban ainda não sabe (sem card ou data diferente).
	Novas int `json:"novas"`
}

// CrmVisitas lista as visitas agendadas pelo CRM (cards do funil com visita
// pendente — agendadas e vencidas; realizada não entra) e compara com o
// kanban: casa o card pelo código service do cliente ou pelo ticket 360.
func (a *App) CrmVisitas() (*ResumoCrmVisitas, error) {
	p, err := a.CrmNegociacoes("", "aberta")
	if err != nil {
		return nil, err
	}
	res := &ResumoCrmVisitas{Itens: []CrmVisitaItem{}}
	for _, n := range p.Itens {
		v := n.Visita
		if v == nil || v.Data == "" || v.Status == "realizada" {
			continue
		}
		item := CrmVisitaItem{
			NegociacaoID: n.ID,
			Titulo:       n.Titulo,
			Cliente:      n.ClienteNome,
			Data:         v.Data,
			Status:       v.Status,
			DiasVencida:  v.DiasVencida,
			Unidade:      n.UnidadeNome,
			TicketNumero: strings.TrimPrefix(valor(n.TicketNumero), "#"),
		}
		if card := a.cardDaNegociacao(n); card != nil {
			item.CardID = card.ID
			item.NoKanban = card.VisitaData == item.Data
		}
		if !item.NoKanban {
			res.Novas++
		}
		res.Itens = append(res.Itens, item)
	}
	return res, nil
}

// CrmImportarVisitas traz todas as visitas novas do CRM para o kanban:
// card existente ganha a visita (data/endereço) e vai para "Visita Agendada";
// sem card, cria um na hora. Devolve o resumo do que fez.
func (a *App) CrmImportarVisitas() (string, error) {
	resumo, err := a.CrmVisitas()
	if err != nil {
		return "", err
	}
	criados, atualizados, emDia := 0, 0, 0
	for _, item := range resumo.Itens {
		if item.NoKanban {
			emDia++
			continue
		}
		cardID := item.CardID
		if cardID == 0 {
			card, err := a.criarCardDoCRM(item, ColunaVisitaAgendada)
			if err != nil {
				return resumoParcial(criados, atualizados, emDia, item.Cliente, err)
			}
			cardID = card
			criados++
		} else {
			atualizados++
		}
		// grava a visita e garante o card na coluna de visitas
		if err := a.tickets.SetVisita(a.ctx, cardID, item.Data, "", "", ""); err != nil {
			return resumoParcial(criados, atualizados, emDia, item.Cliente, err)
		}
		if err := a.levarParaVisitaAgendada(cardID); err != nil {
			return resumoParcial(criados, atualizados, emDia, item.Cliente, err)
		}
	}
	if criados+atualizados == 0 {
		return fmt.Sprintf("Kanban em dia com o CRM — %d visita(s) já sincronizada(s).", emDia), nil
	}
	msg := fmt.Sprintf("Visitas do CRM no kanban: %d card(s) criado(s), %d atualizado(s)", criados, atualizados)
	if emDia > 0 {
		msg += fmt.Sprintf(" (%d já em dia)", emDia)
	}
	return msg + ".", nil
}

// cardDaNegociacao acha o card do kanban da negociação: primeiro pelo código
// service do cliente, depois pelo número do ticket 360 de origem.
func (a *App) cardDaNegociacao(n rota360.NegociacaoCRM) *db.Ticket {
	if n.CodCliente != nil && *n.CodCliente > 0 {
		if t, err := a.tickets.GetPorCodService(a.ctx, fmt.Sprint(*n.CodCliente)); err == nil && t != nil {
			return t
		}
	}
	if n.TicketNumero != nil && *n.TicketNumero != "" {
		var num int64
		if _, err := fmt.Sscanf(*n.TicketNumero, "%d", &num); err == nil && num > 0 {
			if t, err := a.tickets.GetPorNumero360(a.ctx, num); err == nil && t != nil {
				return t
			}
		}
	}
	return nil
}

// criarCardDoCRM cria um card novo no kanban para o item do CRM (cliente
// sem card), com nome/código/endereço/telefone do card do funil, na coluna
// pedida.
func (a *App) criarCardDoCRM(item CrmVisitaItem, coluna string) (int64, error) {
	// funil só traz o nome "23808 - LUIS IZOMAR...": código service separa
	cod := ""
	nome := item.Cliente
	if i := strings.Index(item.Cliente, " - "); i > 0 {
		cod = strings.TrimSpace(item.Cliente[:i])
		nome = strings.TrimSpace(item.Cliente[i+3:])
	}
	if !a.colunaValida(coluna) {
		return 0, fmt.Errorf("coluna %s não existe no quadro", coluna)
	}
	t := db.Ticket{
		NomeCliente: nome,
		TipoTicket:  "Outro",
		CodService:  cod,
		ColunaAtual: coluna,
	}
	if len(t.NomeCliente) > service.MaxNomeCliente {
		t.NomeCliente = t.NomeCliente[:service.MaxNomeCliente]
	}
	// endereço/telefone do cadastro do 360 (cache) quando temos o código
	if cod != "" && a.clientes != nil {
		if cli, err := a.clientes.Get(a.ctx, cod); err == nil && cli != nil {
			if cli.Endereco != "" {
				t.Endereco = cli.Endereco
			}
			if cli.Telefone != "" {
				t.Telefone = cli.Telefone
			}
		}
	}
	id, err := a.tickets.Save(a.ctx, &t)
	if err != nil {
		return 0, friendly("criarCardDoCRM", err)
	}
	if cod != "" {
		a.vinculoAutomaticoTela(id, cod, t.NomeCliente)
	}
	return id, nil
}

// levarParaVisitaAgendada move o card para a coluna de visitas lembrando de
// onde ele veio (cancelar a visita devolve pro lugar), igual ao arraste.
func (a *App) levarParaVisitaAgendada(cardID int64) error {
	t, err := a.tickets.Get(a.ctx, cardID)
	if err != nil {
		return friendly("levarParaVisitaAgendada", err)
	}
	destino := ColunaVisitaAgendada
	if colunas, err := a.colunas.List(a.ctx); err == nil {
		if c, err := service.CasarColuna(destino, colunas); err == nil {
			destino = c
		}
	}
	if t.ColunaAtual == destino {
		return nil
	}
	origem := t.VisitaOrigem
	if origem == "" || origem == destino {
		origem = t.ColunaAtual
	}
	if err := a.tickets.Move(a.ctx, cardID, t.ColunaAtual, destino, 1<<30); err != nil {
		return friendly("levarParaVisitaAgendada", err)
	}
	if err := a.tickets.SetVisitaOrigem(a.ctx, cardID, origem); err != nil {
		return friendly("levarParaVisitaAgendada", err)
	}
	return nil
}

func resumoParcial(criados, atualizados, emDia int, cliente string, err error) (string, error) {
	return "", fmt.Errorf("visitas importadas até agora: %d criado(s), %d atualizado(s) — falhou em %s: %w",
		criados, atualizados, cliente, err)
}

// valor desembrulha ponteiro de string com valor padrão.
func valor(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
// tituloNegociacao monta o título como o 360 monta para cards vindos de
// ticket ("Ticket #12345 · Tipo"), mantendo o vínculo visível nos dois lados.
func tituloNegociacao(t *db.Ticket) string {
	if t.Ticket360Numero != "" {
		return fmt.Sprintf("Ticket #%s · %s", t.Ticket360Numero, t.TipoTicket)
	}
	if t.NumeroTicket != "" {
		return fmt.Sprintf("Ticket #%s · %s", t.NumeroTicket, t.TipoTicket)
	}
	return fmt.Sprintf("%s · %s", t.NomeCliente, t.TipoTicket)
}

// decodificaCorpo decodifica JSON cru (respostas de formato incerto).
func decodificaCorpo(corpo []byte, out any) error {
	if len(corpo) == 0 {
		return fmt.Errorf("corpo vazio")
	}
	return json.Unmarshal(corpo, out)
}
