package db

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

// bancoStats abre um banco migrado (inclui migration012) para o repo de stats.
func bancoStats(t *testing.T) *AgenteStatsRepo {
	t.Helper()
	database, err := Init(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return NewAgenteStatsRepo(database)
}

func TestAgenteStatsIncrementarETop(t *testing.T) {
	ctx := context.Background()
	r := bancoStats(t)

	if err := r.Incrementar(ctx, "financeiro", "atendimentos_cliente"); err != nil {
		t.Fatalf("Incrementar: %v", err)
	}
	// Ainda abaixo do mínimo.
	if tops, err := r.TopFerramentas(ctx, "financeiro", 3); err != nil || len(tops) != 0 {
		t.Fatalf("com contagem 1 e min 3, esperava vazio: %v %v", tops, err)
	}
	for i := 0; i < 2; i++ {
		if err := r.Incrementar(ctx, "financeiro", "atendimentos_cliente"); err != nil {
			t.Fatalf("Incrementar: %v", err)
		}
	}
	tops, err := r.TopFerramentas(ctx, "financeiro", 3)
	if err != nil {
		t.Fatalf("TopFerramentas: %v", err)
	}
	if !reflect.DeepEqual(tops, []string{"atendimentos_cliente"}) {
		t.Fatalf("esperava [atendimentos_cliente], veio %v", tops)
	}
	// Classe diferente não contamina.
	if tops, _ := r.TopFerramentas(ctx, "os", 1); len(tops) != 0 {
		t.Fatalf("classe 'os' deveria estar vazia, veio %v", tops)
	}
}

func TestAgenteStatsIgnoraVazio(t *testing.T) {
	ctx := context.Background()
	r := bancoStats(t)
	if err := r.Incrementar(ctx, "", "financeiro_cliente"); err != nil {
		t.Fatalf("classe vazia deve ser ignorada sem erro: %v", err)
	}
	if tops, _ := r.TopFerramentas(ctx, "", 1); len(tops) != 0 {
		t.Fatalf("nada deveria ter sido gravado: %v", tops)
	}
}
