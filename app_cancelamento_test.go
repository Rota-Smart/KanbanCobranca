package main

import (
	"errors"
	"strings"
	"testing"

	"rota-smart/internal/db"
	"rota-smart/internal/rota360"
	"rota-smart/internal/service"
)

// Helpers: vínculo com assunto "Cancelamento" — o gatilho do motivo obrigatório.
func vincularCancelamento(t *testing.T, a *App, cardID int64, assunto string) {
	t.Helper()
	if err := a.vinculos.Upsert(a.ctx, cardID, "uuid-cancel-"+assunto, "123", assunto, ""); err != nil {
		t.Fatalf("Upsert vínculo: %v", err)
	}
}

// Card comum fecha direto — a trava só vale para cancelamento.
func TestFechamentoSemCancelamentoNaoTrava(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.colunas.Add(a.ctx, ColunaTicketFechado); err != nil {
		t.Fatalf("criar coluna: %v", err)
	}
	card, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	if err := a.MoveTicket(card.ID, ColunaTicketFechado, 0); err != nil {
		t.Fatalf("card comum deveria mover sem motivo: %v", err)
	}
}

// Card de cancelamento sem motivo é barrado em qualquer porta; após
// RegistrarCancelamento o mesmo MoveTicket passa.
func TestCancelamentoExigeMotivo(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.colunas.Add(a.ctx, ColunaTicketFechado); err != nil {
		t.Fatalf("criar coluna: %v", err)
	}
	card, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	vincularCancelamento(t, a, card.ID, "Cancelamento")

	err = a.MoveTicket(card.ID, ColunaTicketFechado, 0)
	if !errors.Is(err, service.ErrCancelamentoSemMotivo) {
		t.Fatalf("deveria barrar com ErrCancelamentoSemMotivo, veio %v", err)
	}
	if !strings.Contains(err.Error(), "CANCELAMENTO_SEM_MOTIVO") {
		t.Fatalf("mensagem deveria carregar o marcador do frontend: %v", err)
	}

	// Resultado inválido e motivo vazio são recusados antes de registrar.
	if err := a.RegistrarCancelamento(card.ID, "", service.CancelamentoPerda); err == nil {
		t.Fatal("motivo vazio deveria dar erro")
	}
	if err := a.RegistrarCancelamento(card.ID, "preço", "tanto faz"); err == nil {
		t.Fatal("resultado inválido deveria dar erro")
	}

	// Motivo novo é criado na tag list; registrar destrava o movimento.
	if err := a.RegistrarCancelamento(card.ID, "Preço Alto", service.CancelamentoPerda); err != nil {
		t.Fatalf("RegistrarCancelamento: %v", err)
	}
	ms, err := a.ListarMotivosCancelamento()
	if err != nil || len(ms) != 1 || ms[0].Nome != "Preço Alto" {
		t.Fatalf("motivo novo deveria existir: %+v err=%v", ms, err)
	}
	if err := a.MoveTicket(card.ID, ColunaTicketFechado, 0); err != nil {
		t.Fatalf("após registrar o motivo o card deveria mover: %v", err)
	}
}

// A mesma grafia com caixa diferente reusa a tag — não duplica motivo.
func TestMotivoChaveNormalizada(t *testing.T) {
	a := newTestApp(t)
	card, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	if err := a.RegistrarCancelamento(card.ID, "preço  alto", service.CancelamentoReversao); err != nil {
		t.Fatalf("registrar: %v", err)
	}
	if err := a.RegistrarCancelamento(card.ID, "Preço Alto", service.CancelamentoPerda); err != nil {
		t.Fatalf("registrar de novo: %v", err)
	}
	ms, err := a.ListarMotivosCancelamento()
	if err != nil || len(ms) != 1 {
		t.Fatalf("tag deveria ser única: %+v err=%v", ms, err)
	}
	c, err := a.cancelamentos.CancelarDoTicket(a.ctx, card.ID)
	if err != nil || c == nil || c.Resultado != service.CancelamentoPerda {
		t.Fatalf("re-registro deveria atualizar p/ perda: %+v err=%v", c, err)
	}
}

// Card de cancelamento que volta do Ticket Fechado perde o registro — se
// fechar de novo, pede motivo outra vez.
func TestSairDeFechadosApagaCancelamento(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.colunas.Add(a.ctx, ColunaTicketFechado); err != nil {
		t.Fatalf("criar coluna: %v", err)
	}
	card, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	vincularCancelamento(t, a, card.ID, "cancelamento de internet")
	if err := a.RegistrarCancelamento(card.ID, "mudou de cidade", service.CancelamentoPerda); err != nil {
		t.Fatalf("registrar: %v", err)
	}
	if err := a.MoveTicket(card.ID, ColunaTicketFechado, 0); err != nil {
		t.Fatalf("mover p/ fechados: %v", err)
	}
	if err := a.MoveTicket(card.ID, "Fazer contato", 0); err != nil {
		t.Fatalf("voltar: %v", err)
	}
	c, err := a.cancelamentos.CancelarDoTicket(a.ctx, card.ID)
	if err != nil || c != nil {
		t.Fatalf("registro deveria ter sido apagado, veio %+v err=%v", c, err)
	}
	if err := a.MoveTicket(card.ID, ColunaTicketFechado, 0); !errors.Is(err, service.ErrCancelamentoSemMotivo) {
		t.Fatalf("segundo fechamento deveria pedir motivo de novo: %v", err)
	}
}

// Assuntos que não são cancelamento não ativam a trava (contém "cancelar" no
// texto não basta; tem que ser "cancelamento").
func TestAssuntoCancelamento(t *testing.T) {
	casos := map[string]bool{
		"Cancelamento":          true,
		"cancelamento de plano": true,
		"CANCELAMENTO":          true,
		"Cobrança":              false,
		"Em andamento":          false,
		"":                      false,
	}
	for assunto, esperado := range casos {
		if got := service.AssuntoCancelamento(assunto); got != esperado {
			t.Fatalf("AssuntoCancelamento(%q) = %v, esperava %v", assunto, got, esperado)
		}
	}
}

// Métricas agregam reversão/perda e a quebra por motivo.
func TestMetricasCancelamentos(t *testing.T) {
	a := newTestApp(t)
	c1, _ := a.SaveTicket(exemploTicket())
	c2, _ := a.SaveTicket(exemploTicket())
	c3, _ := a.SaveTicket(exemploTicket())
	if err := a.RegistrarCancelamento(c1.ID, "preço", service.CancelamentoPerda); err != nil {
		t.Fatalf("registrar c1: %v", err)
	}
	if err := a.RegistrarCancelamento(c2.ID, "preço", service.CancelamentoReversao); err != nil {
		t.Fatalf("registrar c2: %v", err)
	}
	if err := a.RegistrarCancelamento(c3.ID, "internet lenta", service.CancelamentoPerda); err != nil {
		t.Fatalf("registrar c3: %v", err)
	}
	m, err := a.MetricasCancelamentos(30)
	if err != nil {
		t.Fatalf("MetricasCancelamentos: %v", err)
	}
	if m.Total != 3 || m.Perda != 2 || m.Reversao != 1 {
		t.Fatalf("totais errados: %+v", m)
	}
	if len(m.PorMotivo) != 2 || m.PorMotivo[0].Motivo != "preço" || m.PorMotivo[0].Total != 2 {
		t.Fatalf("porMotivo errado: %+v", m.PorMotivo)
	}
	if len(m.Serie) == 0 {
		t.Fatal("série por dia não deveria vir vazia")
	}
	// Clique no motivo devolve só os cards dele.
	cs, err := a.CancelamentosDoMotivo("Preço", true)
	if err != nil || len(cs) != 2 {
		t.Fatalf("CancelamentosDoMotivo: %v (%+v)", err, cs)
	}
}

// Verificação automática não move card de cancelamento sem motivo — fica
// aguardando no resultado.
func TestVerificacaoAutomaticaPulaCancelamentoSemMotivo(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.colunas.Add(a.ctx, ColunaTicketFechado); err != nil {
		t.Fatalf("criar coluna: %v", err)
	}
	card, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	vincularCancelamento(t, a, card.ID, "Cancelamento")
	precisa, err := a.cancelamentoPendente(card.ID)
	if err != nil || !precisa {
		t.Fatalf("cancelamentoPendente deveria ser true, veio %v err=%v", precisa, err)
	}
	// Card sem cancelamento não pede motivo.
	outro, _ := a.SaveTicket(exemploTicket())
	precisa, err = a.cancelamentoPendente(outro.ID)
	if err != nil || precisa {
		t.Fatalf("card comum não deveria pendar: %v err=%v", precisa, err)
	}
}

// Sem banco (wiring nil) o helper não trava — testes antigos seguem passando.
func TestCancelamentoSemWiringNaoTrava(t *testing.T) {
	a := &App{}
	precisa, err := a.cancelamentoPendente(1)
	if err != nil || precisa {
		t.Fatalf("sem wiring não deveria pendar nem errar: %v err=%v", precisa, err)
	}
	var _ = db.MotivoCancelamento{}
}

// Upsert do vínculo: setor/assunto vazios não apagam o que já está gravado;
// preenchidos substituem — é assim que o saneamento em massa corrige sem
// regredir vínculos bons.
func TestUpsertVinculoSetor(t *testing.T) {
	a := newTestApp(t)
	card, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	if err := a.vinculos.Upsert(a.ctx, card.ID, "uuid-setor", "777", "Cancelamento", "Comercial Ijuí"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	// Re-upsert sem assunto nem setor: nada se perde.
	if err := a.vinculos.Upsert(a.ctx, card.ID, "uuid-setor", "777", "", ""); err != nil {
		t.Fatalf("Upsert vazio: %v", err)
	}
	vs, err := a.vinculos.List(a.ctx, card.ID)
	if err != nil || len(vs) != 1 || vs[0].Assunto != "Cancelamento" || vs[0].Setor != "Comercial Ijuí" {
		t.Fatalf("vínculo deveria manter assunto+setor: %+v err=%v", vs, err)
	}
	// Setor novo substitui; assunto vazio continua preservado.
	if err := a.vinculos.Upsert(a.ctx, card.ID, "uuid-setor", "777", "", "Cobrança Ijuí"); err != nil {
		t.Fatalf("Upsert setor novo: %v", err)
	}
	vs, _ = a.vinculos.List(a.ctx, card.ID)
	if vs[0].Setor != "Cobrança Ijuí" || vs[0].Assunto != "Cancelamento" {
		t.Fatalf("setor deveria ter mudado e assunto permanecido: %+v", vs[0])
	}
}

// Sem sessão do 360 o saneamento em massa responde erro claro (não mexe).
func TestSaneaVinculos360SemSessao(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.SaneaVinculos360(); !errors.Is(err, rota360.ErrSemSessao) {
		t.Fatalf("deveria dar ErrSemSessao, veio %v", err)
	}
}
