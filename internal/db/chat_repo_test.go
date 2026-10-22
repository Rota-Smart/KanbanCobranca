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
