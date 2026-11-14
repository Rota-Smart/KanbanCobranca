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
