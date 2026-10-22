package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func abreTesteColunas(t *testing.T) (*ColumnRepo, *TicketRepo) {
	t.Helper()
	database, err := Init(filepath.Join(t.TempDir(), "colunas.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return NewColumnRepo(database), NewTicketRepo(database)
}

func TestColunasSeedEAddRemove(t *testing.T) {
	ctx := context.Background()
	colunas, tickets := abreTesteColunas(t)

	cs, err := colunas.List(ctx)
	if err != nil || len(cs) != 11 {
		t.Fatalf("seed: %v, %d colunas", err, len(cs))
	}
	if cs[0] != "Fazer contato" || cs[3] != "Cobrança" {
		t.Errorf("ordem da seed: %v", cs)
	}

	criada, err := colunas.Add(ctx, "  Renegociação ")
	if err != nil || criada != "Renegociação" {
		t.Fatalf("add: %v %q", err, criada)
	}
	cs, _ = colunas.List(ctx)
	if cs[len(cs)-1] != "Renegociação" {
		t.Errorf("nova coluna não está no fim: %v", cs)
	}

	// Não remove com card dentro.
	if _, err := tickets.Save(ctx, &Ticket{NomeCliente: "X", TipoTicket: "Outro", ColunaAtual: "Renegociação"}); err != nil {
		t.Fatal(err)
	}
	if err := colunas.Remove(ctx, "Renegociação"); !errors.Is(err, ErrColunaNaoVazia) {
		t.Errorf("esperava ErrColunaNaoVazia, veio %v", err)
	}

	// Vazia remove.
	if _, err := colunas.Add(ctx, "Vazia"); err != nil {
		t.Fatal(err)
	}
	if err := colunas.Remove(ctx, "Vazia"); err != nil {
		t.Errorf("remover vazia: %v", err)
	}
}
