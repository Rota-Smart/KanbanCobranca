package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"rota-smart/internal/db"
	"rota-smart/internal/service"
)

// Cancelamento obrigatório: bindings para o diálogo de motivo e o dashboard.
// A trava em si vive no MoveTicket (assim botão de ações, arrastar, chat, IA
// e verificação automática passam todos pelo mesmo lugar).

// cancelamentoPendente diz se o card tem ticket de assunto "Cancelamento"
// sem motivo registrado — só ele bloqueia o fechamento.
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
	return c == nil, nil
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
	return nil
}

// MetricasCancelamentos alimenta o dashboard de cancelamentos (últimos dias;
// 0 = todos).
func (a *App) MetricasCancelamentos(dias int) (*db.MetricasCancelamento, error) {
	if a.cancelamentos == nil {
		return nil, fmt.Errorf("banco indisponível nesta instalação")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, err := a.cancelamentos.Metricas(ctx, dias)
	if err != nil {
		return nil, friendly("MetricasCancelamentos", err)
	}
	return m, nil
}

// CancelamentosDoMotivo devolve os cards de uma tag do gráfico (clique).
func (a *App) CancelamentosDoMotivo(motivo string) ([]db.Cancelamento, error) {
	if a.cancelamentos == nil {
		return nil, fmt.Errorf("banco indisponível nesta instalação")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cs, err := a.cancelamentos.CancelamentosDoMotivo(ctx, db.ChaveMotivo(motivo))
	if err != nil {
		return nil, friendly("CancelamentosDoMotivo", err)
	}
	return cs, nil
}
