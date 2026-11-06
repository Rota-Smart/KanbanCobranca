package db

import (
	"context"
	"testing"
)

func TestAuditoriaSobreviveAoApagarChat(t *testing.T) {
	path, _, _ := newTestDB(t)
	database, err := Init(path)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer database.Close()
	ctx := context.Background()
	chats := NewChatRepo(database)
	aud := NewAuditoriaRepo(database)

	id, err := chats.Create(ctx, "Nova conversa")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := aud.Registrar(ctx, AuditoriaChat{ChatID: id, Origem: OrigemLocalComando, Comando: "move",
		Mensagem: "/move cobrança", Resposta: "movido"}); err != nil {
		t.Fatalf("registrar local: %v", err)
	}
	if err := aud.Registrar(ctx, AuditoriaChat{ChatID: id, Origem: OrigemIA, Mensagem: "quem deve?",
		Ferramentas: `[{"nome":"financeiro_cliente","args":"{\"cod\":\"1\"}"}]`,
		PromptTokens: 1200, CompletionTokens: 80, Rodadas: 2, DuracaoMs: 900}); err != nil {
		t.Fatalf("registrar ia: %v", err)
	}
	if err := chats.Delete(ctx, id); err != nil {
		t.Fatalf("delete chat: %v", err)
	}

	linhas, err := aud.Listar(ctx)
	if err != nil || len(linhas) != 2 {
		t.Fatalf("auditoria deveria sobreviver ao apagar o chat: %v %d", err, len(linhas))
	}
	if linhas[1].PromptTokens != 1200 || linhas[1].Rodadas != 2 || linhas[0].Comando != "move" {
		t.Fatalf("linhas gravadas erradas: %+v", linhas)
	}
	r, err := aud.Resumo(ctx)
	if err != nil {
		t.Fatalf("resumo: %v", err)
	}
	if r.Trocas != 2 || r.PorOrigem[OrigemIA] != 1 || r.PorOrigem[OrigemLocalComando] != 1 || r.PromptTokens != 1200 {
		t.Fatalf("resumo errado: %+v", r)
	}
	if r.PeriodoInicio == "" || r.PeriodoFim == "" {
		t.Fatalf("período vazio: %+v", r)
	}
}
