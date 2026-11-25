package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"rota-smart/internal/contasync"
	"rota-smart/internal/db"
	"rota-smart/internal/service"
)

// Cancelamento obrigatório: bindings para o diálogo de motivo e o dashboard.
// A trava em si vive no MoveTicket (assim botão de ações, arrastar, chat, IA
// e verificação automática passam todos pelo mesmo lugar).

// cancelamentoPendente diz se o card tem ticket de assunto "Cancelamento"
// sem resposta final (reversão/perda) — só ele bloqueia o fechamento.
func (a *App) cancelamentoPendente(ticketID int64) (bool, error) {
	if a.vinculos == nil || a.cancelamentos == nil {
		return false, nil // wiring ausente (testes antigos): não trava
	}
	vs, err := a.vinculos.List(a.ctx, ticketID)
	if err != nil {
		return false, err
	}
	temCancelamento := false
	for _, v := range vs {
		if service.AssuntoCancelamento(v.Assunto) {
			temCancelamento = true
			break
		}
	}
	if !temCancelamento {
		return false, nil
	}
	c, err := a.cancelamentos.CancelarDoTicket(a.ctx, ticketID)
	if err != nil {
		return false, err
	}
	// Registro com reversão ou perda é a resposta final — inclusive os que
	// vieram da importação. em_tratamento (ou sem registro) não fecha: o
	// fechamento do card precisa perguntar de novo.
	if c == nil {
		return true, nil
	}
	return c.Resultado != service.CancelamentoReversao && c.Resultado != service.CancelamentoPerda, nil
}

// ListarMotivosCancelamento alimenta o datalist do diálogo de motivo.
func (a *App) ListarMotivosCancelamento() ([]db.MotivoCancelamento, error) {
	if a.cancelamentos == nil {
		return nil, fmt.Errorf("banco indisponível nesta instalação")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ms, err := a.cancelamentos.MotivosListar(ctx)
	if err != nil {
		return nil, friendly("ListarMotivosCancelamento", err)
	}
	return ms, nil
}

// RegistrarCancelamento grava motivo (cria a tag se for nova) + resultado do
// cancelamento de um card. O movimento em si fica por conta do chamador.
func (a *App) RegistrarCancelamento(ticketID int64, motivo, resultado string) error {
	if a.cancelamentos == nil {
		return fmt.Errorf("banco indisponível nesta instalação")
	}
	if _, err := a.tickets.Get(a.ctx, ticketID); err != nil {
		return friendly("RegistrarCancelamento", err)
	}
	motivo = strings.TrimSpace(motivo)
	if motivo == "" {
		return fmt.Errorf("diga o motivo do cancelamento")
	}
	if !service.CancelamentoResultadoValido(resultado) {
		return fmt.Errorf("resultado deve ser reversão ou perda")
	}
	m, err := a.cancelamentos.MotivoCriarSeNaoExiste(a.ctx, motivo)
	if err != nil {
		return friendly("RegistrarCancelamento", err)
	}
	if err := a.cancelamentos.CancelarMarcar(a.ctx, ticketID, m.ID, resultado); err != nil {
		return friendly("RegistrarCancelamento", err)
	}
	if a.uiPronta {
		runtime.EventsEmit(a.ctx, "cancelamentos:mudou", nil)
	}
	// Sincroniza marcação de motivo de cancelamento com o site em background
	go func() {
		t, err := a.tickets.Get(context.Background(), ticketID)
		if err != nil || t == nil || t.NomeCliente == "" {
			return
		}
		// Formata linha para a API do site registrar o cancelamento do card
		resTrecho := " - REVERTIDO"
		if resultado == service.CancelamentoPerda {
			resTrecho = " - CANCELADO"
		} else if resultado == service.CancelamentoTratamento {
			resTrecho = ""
		}
		texto := fmt.Sprintf("%s - %s%s", t.NomeCliente, motivo, resTrecho)
		a.sincronizarImportSite(texto, t.ColunaAtual)
	}()
	return nil
}

// MetricasCancelamentos alimenta o dashboard de cancelamentos (últimos dias;
// 0 = todos). Sem incluirTratamento só entram os finalizados (reversão/perda) —
// mesmo padrão do painel do site.
func (a *App) MetricasCancelamentos(dias int, incluirTratamento bool) (*db.MetricasCancelamento, error) {
	if a.cancelamentos == nil {
		return nil, fmt.Errorf("banco indisponível nesta instalação")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, err := a.cancelamentos.Metricas(ctx, dias, incluirTratamento)
	if err != nil {
		return nil, friendly("MetricasCancelamentos", err)
	}
	return m, nil
}

// CancelamentosDoMotivo devolve os cards de uma tag do gráfico (clique).
// Sem incluirTratamento só entram os finalizados (reversão/perda).
func (a *App) CancelamentosDoMotivo(motivo string, incluirTratamento bool) ([]db.Cancelamento, error) {
	if a.cancelamentos == nil {
		return nil, fmt.Errorf("banco indisponível nesta instalação")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cs, err := a.cancelamentos.CancelamentosDoMotivo(ctx, db.ChaveMotivo(motivo), incluirTratamento)
	if err != nil {
		return nil, friendly("CancelamentosDoMotivo", err)
	}
	return cs, nil
}

// CancelamentosTodos devolve todos os registros de cancelamento para o quadro (filtros e badges).
func (a *App) CancelamentosTodos() ([]db.Cancelamento, error) {
	if a.cancelamentos == nil {
		return []db.Cancelamento{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cs, err := a.cancelamentos.CancelarTodos(ctx)
	if err != nil {
		return nil, friendly("CancelamentosTodos", err)
	}
	return cs, nil
}

// itemSyncCancel é um cancelamento local pronto para subir à nuvem.
type itemSyncCancel struct {
	TicketID  int64  `json:"ticket_id"`
	Motivo    string `json:"motivo"`
	Resultado string `json:"resultado"`
}

// SincronizarCancelamentosNuvem sobe TODOS os cancelamentos locais para o
// site (upsert idempotente por ticket). O histórico só existia no banco
// local — sem isso o dashboard do site nasce zerado. Devolve quantos
// registros a nuvem aceitou.
func (a *App) SincronizarCancelamentosNuvem() (int, int, error) {
	if a.cancelamentos == nil {
		return 0, 0, fmt.Errorf("banco indisponível nesta instalação")
	}
	tok, _ := lerTokenConta()
	if tok == "" {
		return 0, 0, fmt.Errorf("conta não pareada")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cs, err := a.cancelamentos.CancelarTodos(ctx)
	if err != nil {
		return 0, 0, friendly("SincronizarCancelamentosNuvem", err)
	}
	if len(cs) == 0 {
		return 0, 0, nil
	}
	itens := make([]itemSyncCancel, 0, len(cs))
	for _, c := range cs {
		itens = append(itens, itemSyncCancel{TicketID: c.TicketID, Motivo: c.Motivo, Resultado: c.Resultado})
	}
	corpo, err := json.Marshal(map[string]any{"itens": itens})
	if err != nil {
		return 0, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, contasync.URLPadrao+"/cancelamentos/sincronizar", bytes.NewReader(corpo))
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("a nuvem respondeu HTTP %d para /cancelamentos/sincronizar", resp.StatusCode)
	}
	var res struct {
		Registrados int `json:"registrados"`
		Falhas      int `json:"falhas"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&res); err != nil {
		return 0, 0, err
	}
	// Falhas = cancelamento cujo ticket ainda não chegou à nuvem (a fila de
	// sync continua rodando); sobem na próxima tentativa.
	slog.Info("cancelamentos sincronizados com a nuvem", "total", res.Registrados, "falhas", res.Falhas)
	return res.Registrados, res.Falhas, nil
}
