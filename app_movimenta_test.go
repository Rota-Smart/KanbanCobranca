package main

import (
	"errors"
	"strings"
	"testing"

	"rota-smart/internal/rota360"
	"rota-smart/internal/service"
)

// TestMovimentarTicket360Validacao: a ação manual (Shift+F6) valida entrada e
// coluna na borda e exige sessão no 360 (sem ela não há de onde puxar o
// cliente). O fluxo completo com o 360 é o mesmo de ImportarCliente360 +
// vínculo, já exercidos nos testes desses caminhos.
func TestMovimentarTicket360Validacao(t *testing.T) {
	a := newTestApp(t)

	if _, err := a.MovimentarTicket360("12345", "Coluna que não existe"); !errors.Is(err, service.ErrColunaInvalida) {
		t.Fatalf("coluna inválida deveria dar ErrColunaInvalida, veio %v", err)
	}
	if _, err := a.MovimentarTicket360("   ", "Cobrança"); err == nil {
		t.Fatalf("entrada vazia deveria dar erro")
	}
	if _, err := a.MovimentarTicket360("12345", "Cobrança"); !errors.Is(err, rota360.ErrSemSessao) {
		t.Fatalf("sem sessão no 360 deveria dar ErrSemSessao, veio %v", err)
	}
}

// TestComandoMoveLocal: o /move do chat roda sem IA — a variante de ticket sem
// sessão no 360 devolve erro amigável (não manda pro agente) e a de card move
// o card existente direto no banco. O /move sozinho devolve o uso.
func TestComandoMoveLocal(t *testing.T) {
	a := newTestApp(t)
	salvo, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}

	if _, ok := a.executarComandoMove("mova o card do João para Cobrança", 0); ok {
		t.Fatal("frase sem barra não é comando /move")
	}

	res, ok := a.executarComandoMove("/move", 0)
	if !ok || res.Acoes == nil || len(res.Acoes) != 0 {
		t.Fatalf("/move sozinho deveria ser reconhecido com resposta de uso; ok=%v acoes=%v", ok, res.Acoes)
	}
	if !strings.Contains(res.Resposta, "Uso: /move") || !strings.Contains(res.Resposta, "Colunas:") {
		t.Fatalf("resposta de uso inesperada: %q", res.Resposta)
	}

	if _, ok := a.executarComandoMove("/move 99999 para Fazer contato", 0); !ok {
		t.Fatal("/move com ticket deveria ser reconhecido mesmo sem sessão no 360")
	}
	// (o resultado do ticket sem sessão vem na resposta; ErrSemSessao é
	// tratado em TestMovimentarTicket360Validacao)

	res, ok = a.executarComandoMove("/move Maria Souza para Negociando", 0)
	if !ok {
		t.Fatal("/move com card deveria ser reconhecido")
	}
	if len(res.Acoes) != 1 || res.Acoes[0].Ferramenta != "mover_card" {
		t.Fatalf("ação esperada mover_card, veio %+v", res.Acoes)
	}
	t2, err := a.tickets.Get(a.ctx, salvo.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if t2.ColunaAtual != "Negociando" {
		t.Fatalf("card deveria ter ido para Negociando, está em %q", t2.ColunaAtual)
	}

	res, ok = a.executarComandoMove("/move Maria Souza para coluna inexistente", 0)
	if !ok || !strings.Contains(res.Resposta, "nenhuma coluna") {
		t.Fatalf("coluna inexistente deveria listar as colunas, veio %q", res.Resposta)
	}
}

// TestComandoComContextoDoChat: "/move cobrança" e "/apagar" sem cliente usam
// o cliente citado por último nas mensagens da conversa.
func TestComandoComContextoDoChat(t *testing.T) {
	a := newTestApp(t)
	salvo, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	criarChat := func(titulo string) int64 {
		t.Helper()
		id, err := a.chats.Create(a.ctx, titulo)
		if err != nil {
			t.Fatalf("Create chat: %v", err)
		}
		return id
	}
	chat1 := criarChat("contexto 1")
	if err := a.chats.AddMensagem(a.ctx, chat1, "user", "qual o financeiro da Maria Souza?", ""); err != nil {
		t.Fatalf("AddMensagem: %v", err)
	}

	res, ok := a.executarComandoMove("/move cobrança", chat1)
	if !ok || len(res.Acoes) != 1 || res.Acoes[0].Ferramenta != "mover_card" {
		t.Fatalf("/move cobrança deveria mover via contexto: ok=%v acoes=%v resp=%q", ok, res.Acoes, res.Resposta)
	}
	t2, _ := a.tickets.Get(a.ctx, salvo.ID)
	if t2.ColunaAtual != "Cobrança" {
		t.Fatalf("card deveria estar em Cobrança, está em %q", t2.ColunaAtual)
	}

	// conversa sem nenhum cliente citado: erro claro, nada sai do lugar
	chat2 := criarChat("contexto 2")
	if err := a.chats.AddMensagem(a.ctx, chat2, "user", "bom dia", ""); err != nil {
		t.Fatalf("AddMensagem: %v", err)
	}
	res, ok = a.executarComandoApagar("/apagar", chat2)
	if !ok || !strings.Contains(res.Resposta, "Não identifiquei o cliente") {
		t.Fatalf("/apagar sem contexto deveria pedir o cliente, veio %q (ok=%v)", res.Resposta, ok)
	}

	// /apagar com contexto apaga o card do cliente em discussão
	chat3 := criarChat("contexto 3")
	if err := a.chats.AddMensagem(a.ctx, chat3, "user", "vamos resolver o Maria Souza", ""); err != nil {
		t.Fatalf("AddMensagem: %v", err)
	}
	res, ok = a.executarComandoApagar("/apagar", chat3)
	if !ok || len(res.Acoes) != 1 || res.Acoes[0].Ferramenta != "apagar_card" {
		t.Fatalf("/apagar deveria apagar via contexto: ok=%v acoes=%v resp=%q", ok, res.Acoes, res.Resposta)
	}
	if _, err := a.tickets.Get(a.ctx, salvo.ID); err == nil {
		t.Fatal("card deveria ter sido apagado")
	}

	// /apagar João em Coluna-errada: confere antes e não apaga
	tk, _ := a.SaveTicket(exemploTicket())
	res, ok = a.executarComandoApagar("/apagar Maria Souza em Negociando", chat3)
	if !ok || !strings.Contains(res.Resposta, "Nada foi apagado") {
		t.Fatalf("coluna divergente deveria bloquear, veio %q (ok=%v)", res.Resposta, ok)
	}
	if _, err := a.tickets.Get(a.ctx, tk.ID); err != nil {
		t.Fatal("card não deveria ter sido apagado")
	}
}
