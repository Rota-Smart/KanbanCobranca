// Vigia do funil do CRM: quando um card sai de "Não atendido" para
// "Tentativa de contato" no 360, o card do cliente é garantido na coluna
// "Fazer contato" do quadro (cria se não existe, move se está em outra
// coluna). O 360 só é lido — a escrita é sempre no quadro local.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"rota-smart/internal/rota360"
	"rota-smart/internal/service"
)

const (
	// cfgCrmWatchEstado guarda o snapshot {negociação: etapa} da última leitura.
	cfgCrmWatchEstado = "crm_watch_estado"
	crmWatchTick      = 2 * time.Minute  // leitura periódica do funil
	crmWatchMinimo    = 20 * time.Second // piso entre verificações disparadas
	crmWatchColuna    = "Fazer contato"
	evCrmFunilAcoes   = "crm:funil-acoes"
)

var ultimoCRMWatch atomic.Int64

func (a *App) iniciarCrmWatch() {
	go a.monitorarFunilCRM()
}

// monitorarFunilCRM roda em background: confere o funil a cada 2 minutos
// enquanto houver sessão no 360.
func (a *App) monitorarFunilCRM() {
	for a.ctx.Err() == nil {
		if !dormir(a.ctx, crmWatchTick) {
			return
		}
		if _, err := a.verificarFunilCRM(); err != nil {
			slog.Warn("vigia do CRM falhou", "err", err)
		}
	}
}

// acordarCRMWatch roda a verificação fora de hora (poke do site, foco da
// janela), com piso de 20s para não martelar o 360.
func (a *App) acordarCRMWatch() {
	agora := time.Now().Unix()
	if ultimo := ultimoCRMWatch.Load(); agora-ultimo < int64(crmWatchMinimo.Seconds()) {
		return
	}
	ultimoCRMWatch.Store(agora)
	go func() {
		if _, err := a.verificarFunilCRM(); err != nil {
			slog.Warn("vigia do CRM (disparo) falhou", "err", err)
		}
	}()
}

// VerificarFunilCRM roda a verificação agora e devolve o resumo das ações
// (binding para o frontend disparar quando a janela ganha foco).
func (a *App) VerificarFunilCRM() (string, error) {
	return a.verificarFunilCRM()
}

func (a *App) verificarFunilCRM() (string, error) {
	c := a.rotaAtual()
	if c == nil {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	_, etapas, _, err := c.FunisCRM(ctx)
	if err != nil {
		return "", friendly("verificarFunilCRM", err)
	}
	origem := map[string]bool{}  // etapa "Não atendido"
	destino := map[string]bool{} // etapa "Tentativa de contato"
	for _, e := range etapas {
		switch crmEtapaChave(e.Nome) {
		case "nao atendido":
			origem[e.ID] = true
		case "tentativa de contato":
			destino[e.ID] = true
		}
	}
	if len(destino) == 0 {
		return "", nil
	}
	p, err := c.NegociacoesCRM(ctx, "", "aberta")
	if err != nil {
		return "", friendly("verificarFunilCRM", err)
	}
	anterior := map[string]string{}
	if bruto := a.configGet(a.ctx, cfgCrmWatchEstado); bruto != "" {
		_ = json.Unmarshal([]byte(bruto), &anterior)
	}
	base := len(anterior) == 0 // primeira leitura: só grava, não move nada
	atual := map[string]string{}
	var acoes []string
	for _, n := range p.Itens {
		atual[n.ID] = n.EtapaID
		if base || !destino[n.EtapaID] || !origem[anterior[n.ID]] {
			continue
		}
		msg, err := a.crmWatchGarantirCard(n)
		if err != nil {
			slog.Warn("vigia do CRM: card não garantido", "negociacao", n.ID, "err", err)
			continue
		}
		if msg != "" {
			acoes = append(acoes, msg)
		}
	}
	if bruto, err := json.Marshal(atual); err == nil {
		_ = a.config.Set(a.ctx, cfgCrmWatchEstado, string(bruto))
	}
	if len(acoes) == 0 {
		return "", nil
	}
	resumo := strings.Join(acoes, "; ")
	if a.uiPronta {
		runtime.EventsEmit(a.ctx, evCrmFunilAcoes, map[string]any{"resumo": resumo})
	}
	return resumo, nil
}

// crmWatchGarantirCard cria (cliente sem card) ou move (card em outra coluna)
// o card para a coluna do vigia. Devolve vazio quando nada precisou.
func (a *App) crmWatchGarantirCard(n rota360.NegociacaoCRM) (string, error) {
	destino, ok := a.colunaCanonica(crmWatchColuna)
	if !ok {
		return "", fmt.Errorf("coluna %s não existe no quadro", crmWatchColuna)
	}
	card := a.cardDaNegociacao(n)
	if card == nil {
		item := CrmVisitaItem{NegociacaoID: n.ID, Titulo: n.Titulo, Cliente: n.ClienteNome}
		// o código service do card do funil vence o que estiver no nome
		if n.CodCliente != nil && *n.CodCliente > 0 {
			cod := fmt.Sprint(*n.CodCliente)
			nome := strings.TrimSpace(n.ClienteNome)
			if i := strings.Index(nome, " - "); i > 0 {
				nome = strings.TrimSpace(nome[i+3:])
			}
			item.Cliente = cod + " - " + nome
		}
		if _, err := a.criarCardDoCRM(item, destino); err != nil {
			return "", err
		}
		return fmt.Sprintf("card de %s criado em %s", item.Cliente, destino), nil
	}
	if card.ColunaAtual == destino {
		return "", nil
	}
	if err := a.MoveTicket(card.ID, destino, 1<<30); err != nil {
		return "", err
	}
	return fmt.Sprintf("card de %s movido para %s", card.NomeCliente, destino), nil
}

// crmEtapaChave normaliza o nome da etapa para comparação sem caixa/acento.
func crmEtapaChave(nome string) string {
	return strings.Join(strings.Fields(service.Normalizar(nome)), " ")
}
