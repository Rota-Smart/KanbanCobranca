package db

import (
	"context"
	"testing"
)

func TestChatRepoHistoricoPersistente(t *testing.T) {
	path, _, _ := newTestDB(t)
	database, err := Init(path)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer database.Close()
	r := NewChatRepo(database)
	ctx := context.Background()

	id, err := r.Create(ctx, "Nova conversa")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := r.AddMensagem(ctx, id, "user", "Buscar o cliente RESOLUTE", ""); err != nil {
		t.Fatalf("add user: %v", err)
	}
	if err := r.AddMensagem(ctx, id, "assistant", "Cliente encontrado.", `[{"ferramenta":"buscar_cliente_360","resumo":"buscou"}]`); err != nil {
		t.Fatalf("add assistant: %v", err)
	}

	chats, err := r.List(ctx)
	if err != nil || len(chats) != 1 {
		t.Fatalf("list: %v (%d chats)", err, len(chats))
	}
	if chats[0].Titulo != "Buscar o cliente RESOLUTE" {
		t.Errorf("título auto = %q", chats[0].Titulo)
	}

	ms, err := r.Mensagens(ctx, id)
	if err != nil || len(ms) != 2 {
		t.Fatalf("mensagens: %v (%d)", err, len(ms))
	}
	if ms[0].Papel != "user" || ms[1].Papel != "assistant" {
		t.Errorf("ordem errada: %s, %s", ms[0].Papel, ms[1].Papel)
	}
	if ms[1].AcoesJSON == "" {
		t.Errorf("acoes_json vazio")
	}

	if err := r.Delete(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if ms, _ := r.Mensagens(ctx, id); len(ms) != 0 {
		t.Errorf("mensagens sobreviveram ao delete (cascade)")
	}
}

func TestChatRepoLimparVazios(t *testing.T) {
	path, _, _ := newTestDB(t)
	database, err := Init(path)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer database.Close()
	r := NewChatRepo(database)
	ctx := context.Background()

	if _, err := r.Create(ctx, "Nova conversa"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := r.Create(ctx, "Nova conversa"); err != nil {
		t.Fatalf("create: %v", err)
	}
	usado, _ := r.Create(ctx, "Nova conversa")
	if err := r.AddMensagem(ctx, usado, "user", "Financeiro do 24755", ""); err != nil {
		t.Fatalf("add: %v", err)
	}

	if err := r.LimparVazios(ctx); err != nil {
		t.Fatalf("limpar: %v", err)
	}
	chats, err := r.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(chats) != 1 || chats[0].ID != usado {
		t.Errorf("esperava só o chat %d com mensagens, veio %v", usado, chats)
	}
}
