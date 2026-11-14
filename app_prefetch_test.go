package main

import (
	"context"
	"strings"
	"testing"

	"rota-smart/internal/db"
)

// Contexto do teste: conversa com cliente alvo identificado (fluxo Shift+F2:
// "código service X" no cabeçalho) + card no quadro + acordo ativo + tentativas
// de contato. A pré-busca deve montar o bloco de contexto SEM rede (sem
// sessão no 360) e nunca devolver erro — mensagem genérica tem que continuar
// funcionando mesmo quando nada é identificável.
func testePrebuscaSetup(t *testing.T) (*App, int64, db.Ticket) {
	t.Helper()
	a := newTestApp(t)
	tk, err := a.SaveTicket(db.Ticket{
		NomeCliente:     "Maria Souza",
		TipoTicket:      "Manutencao",
		ColunaAtual:     "Fazer contato",
		CodService:      "12345",
		Ticket360Numero: "777",
	})
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	if err := a.tickets.VincularTicket360(a.ctx, tk.ID, "uuid-777", "777", "12345"); err != nil {
		t.Fatalf("VincularTicket360: %v", err)
	}
	chatID, err := a.chats.Create(a.ctx, "teste")
	if err != nil {
		t.Fatalf("Create chat: %v", err)
	}
	if err := a.chats.AddMensagem(a.ctx, chatID, "user", "código service 12345 — cliente reclama de sensor", ""); err != nil {
		t.Fatalf("AddMensagem: %v", err)
	}
	if _, err := a.RegistrarTentativaContato(tk.ID, "ligação", "sem resposta"); err != nil {
		t.Fatalf("RegistrarTentativaContato: %v", err)
	}
	if _, err := a.cobrancas.SaveAtiva(a.ctx, &db.Cobranca{
		TicketID:           tk.ID,
		DataContrato:       "2026-01-10",
		DataVencimento:     "2026-02-10",
		TotalAbertoCents:   50000,
		EntradaCents:       10000,
		NumeroVezes:        3,
		ParcelaBaseCents:   13333,
		ParceladoCents:     40000,
		UltimaParcelaCents: 13334,
		FinalCents:         50000,
		Status:             "em dia",
		Ativa:              true,
		VersaoRegra:        1,
	}); err != nil {
		t.Fatalf("SaveAtiva: %v", err)
	}
	return a, chatID, tk
}

// Mensagem genérica (nem consulta de dados nem ação) com cliente alvo na
// conversa: a pré-busca completa monta o bloco com posição no quadro, acordo
// e tentativas — sem sessão no 360, a parte de rede é pulada sem erro.
func TestPrebuscaContextoAlvoMontaBloco(t *testing.T) {
	a, chatID, tk := testePrebuscaSetup(t)
	ctx := context.WithValue(a.ctx, chaveChatID{}, chatID)

	pre, err := a.prepararPrebusca(ctx, "e agora, o que faço com ele?")
	if err != nil {
		t.Fatalf("prepararPrebusca: %v", err)
	}
	if pre == nil {
		t.Fatal("esperava prebusca não nula com cliente alvo na conversa")
	}
	if !strings.Contains(pre.bloco, "CONTEXTO DO CLIENTE") {
		t.Fatalf("bloco sem cabeçalho de contexto: %q", pre.bloco)
	}
	if !strings.Contains(pre.bloco, "card id") || !strings.Contains(pre.bloco, tk.ColunaAtual) {
		t.Fatalf("bloco sem posição no quadro: %q", pre.bloco)
	}
	if !strings.Contains(pre.bloco, "ticket 360 #777") {
		t.Fatalf("bloco sem ticket vinculado: %q", pre.bloco)
	}
	if !strings.Contains(pre.bloco, "Último acordo ativo") {
		t.Fatalf("bloco sem acordo: %q", pre.bloco)
	}
	if !strings.Contains(pre.bloco, "ligação") || !strings.Contains(pre.bloco, "sem resposta") {
		t.Fatalf("bloco sem tentativas de contato: %q", pre.bloco)
	}
	// Sem sessão no 360 nada de tool de leitura sai do request: o modelo
	// continua podendo buscar por conta própria.
	for _, f := range []string{"financeiro_cliente", "atendimentos_cliente", "equipamentos_cliente"} {
		for _, r := range pre.removidas {
			if r == f {
				t.Fatalf("tool %s não deveria sair sem dado pré-buscado", f)
			}
		}
	}
}

// Mensagem de AÇÃO com cliente em discussão: a pré-busca continua sem os
// dashboards do 360, mas o contexto LOCAL (acordo, tentativas, posição) entra
// de graça no bloco.
func TestPrebuscaAcaoGanhaContextoLocal(t *testing.T) {
	a, chatID, _ := testePrebuscaSetup(t)
	ctx := context.WithValue(a.ctx, chaveChatID{}, chatID)

	pre, err := a.prepararPrebusca(ctx, "mova o card para Cobrança")
	if err != nil {
		t.Fatalf("prepararPrebusca: %v", err)
	}
	if pre == nil {
		t.Fatal("esperava prebusca de ação")
	}
	if !strings.Contains(pre.bloco, "CONTEXTO LOCAL") {
		t.Fatalf("bloco de ação sem contexto local: %q", pre.bloco)
	}
	if !strings.Contains(pre.bloco, "Posição no quadro") {
		t.Fatalf("bloco de ação sem posição no quadro: %q", pre.bloco)
	}
	if len(pre.removidas) == 0 {
		t.Fatal("ação deveria continuar roteando tools fora da classe")
	}
}

// Fallback: sem cliente alvo identificável (conversa vazia, mensagem sem
// código/nome), a pré-busca devolve nil e o fluxo antigo (tools completas)
// segue sem quebrar.
func TestPrebuscaSemAlvoNaoQuebra(t *testing.T) {
	a := newTestApp(t)
	chatID, err := a.chats.Create(a.ctx, "vazio")
	if err != nil {
		t.Fatalf("Create chat: %v", err)
	}
	ctx := context.WithValue(a.ctx, chaveChatID{}, chatID)

	if pre, err := a.prepararPrebusca(ctx, "e agora, o que faço com ele?"); err != nil || pre != nil {
		t.Fatalf("esperava (nil, nil) sem cliente alvo, veio (%+v, %v)", pre, err)
	}
	// Mensagem de consulta de dados SEM sessão continua PARANDO com o erro
	// de sessão (a IA não é acionada sem dados oficiais) — comportamento
	// existente preservado.
	if _, err := a.prepararPrebusca(ctx, "qual o financeiro do código 12345?"); err == nil {
		t.Fatal("consulta de dados sem sessão deveria parar com erro de sessão")
	}
}

// Interruptor de configuração: desligado, nem bloco local entra (fluxo
// v1.96 preservado) e o GetConfig reflete o estado.
func TestPrebuscaIAInterruptor(t *testing.T) {
	a, chatID, _ := testePrebuscaSetup(t)
	a.config = db.NewConfigRepo(a.db)
	if !a.prebuscaIAAtivada() {
		t.Fatal("pré-busca deveria vir ligada por padrão")
	}
	if err := a.SetPrebuscaIA(false); err != nil {
		t.Fatalf("SetPrebuscaIA(false): %v", err)
	}
	if a.prebuscaIAAtivada() {
		t.Fatal("pré-busca deveria estar desligada")
	}
	cfg := a.GetConfig()
	if cfg.PrebuscaIA {
		t.Fatal("ConfigApp.PrebuscaIA deveria refletir o desligado")
	}
	ctx := context.WithValue(a.ctx, chaveChatID{}, chatID)
	if pre, err := a.prepararPrebusca(ctx, "e agora, o que faço com ele?"); err != nil || pre != nil {
		t.Fatalf("pré-busca desligada deveria devolver (nil, nil), veio (%+v, %v)", pre, err)
	}
	if err := a.SetPrebuscaIA(true); err != nil {
		t.Fatalf("SetPrebuscaIA(true): %v", err)
	}
	if !a.prebuscaIAAtivada() || !a.GetConfig().PrebuscaIA {
		t.Fatal("religada deveria voltar ao default")
	}
}
