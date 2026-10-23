package db

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPadraoRepoUpsertListDelete(t *testing.T) {
	path, _, _ := newTestDB(t)
	database, err := Init(path)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer database.Close()
	r := NewPadraoRepo(database)
	ctx := context.Background()

	if err := r.Upsert(ctx, "estilo de tickets", "Escreva frases curtas, sem jargão."); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Reensinar pelo mesmo nome atualiza em vez de duplicar.
	if err := r.Upsert(ctx, "estilo de tickets", "Escreva frases curtas e objetivas."); err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	if err := r.Upsert(ctx, "assinatura", "Assine com 'Atenciosamente, Equipe Rota'."); err != nil {
		t.Fatalf("upsert 3: %v", err)
	}

	// 2 seeds da migration + "assinatura" = 3 padrões.
	ps, err := r.List(ctx)
	if err != nil || len(ps) != 3 {
		t.Fatalf("list: %v (%d padrões)", err, len(ps))
	}
	if ps[0].Nome != "assinatura" {
		t.Errorf("esperava ordem alfabética, veio %q", ps[0].Nome)
	}
	if ps[1].Conteudo != "Escreva frases curtas e objetivas." {
		t.Errorf("reensinar não atualizou: %q", ps[1].Conteudo)
	}

	if err := r.Delete(ctx, "assinatura"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := r.Delete(ctx, "assinatura"); !errors.Is(err, ErrPadraoNaoEncontrado) {
		t.Errorf("delete inexistente: %v", err)
	}
	// Restam os 2 seeds (estilo de tickets + proposta de renegociação).
	if n, _ := r.Contagem(ctx); n != 2 {
		t.Errorf("contagem = %d, esperava 1", n)
	}
}

func TestPadraoRepoValidacao(t *testing.T) {
	path, _, _ := newTestDB(t)
	database, err := Init(path)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer database.Close()
	r := NewPadraoRepo(database)
	ctx := context.Background()

	if err := r.Upsert(ctx, "  ", "conteúdo"); !errors.Is(err, ErrPadraoNomeVazio) {
		t.Errorf("nome vazio: %v", err)
	}
	if err := r.Upsert(ctx, "nome", "   "); !errors.Is(err, ErrPadraoConteudoVazio) {
		t.Errorf("conteúdo vazio: %v", err)
	}
	grande := strings.Repeat("x", MaxConteudoPadrao+500)
	if err := r.Upsert(ctx, "grande", grande); err != nil {
		t.Fatalf("upsert grande: %v", err)
	}
	ps, _ := r.List(ctx)
	var conteudo string
	for _, p := range ps {
		if p.Nome == "grande" {
			conteudo = p.Conteudo
		}
	}
	if len([]rune(conteudo)) != MaxConteudoPadrao {
		t.Errorf("conteúdo não foi cortado no limite (%d runas)", len([]rune(conteudo)))
	}
}
