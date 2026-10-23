package db

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
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

func TestColunaRenomear(t *testing.T) {
	ctx := context.Background()
	colunas, tickets := abreTesteColunas(t)

	if _, err := tickets.Save(ctx, &Ticket{NomeCliente: "X", TipoTicket: "Outro", ColunaAtual: "Negociando"}); err != nil {
		t.Fatal(err)
	}
	if err := colunas.Renomear(ctx, "Negociando", "  Em negociação "); err != nil {
		t.Fatalf("renomear: %v", err)
	}
	cs, _ := colunas.List(ctx)
	if cs[2] != "Em negociação" {
		t.Errorf("ordem após renomear: %v", cs)
	}
	tk, err := tickets.List(ctx)
	if err != nil || len(tk) != 1 || tk[0].ColunaAtual != "Em negociação" {
		t.Errorf("tickets não acompanharam o rename: %v %v", err, tk)
	}

	// Nome novo já existente recusa.
	if err := colunas.Renomear(ctx, "Em negociação", "Cobrança"); err == nil {
		t.Error("renomear para nome existente deveria falhar")
	}
	// Nome vazio ou comprido demais recusa.
	if err := colunas.Renomear(ctx, "Em negociação", "  "); err == nil {
		t.Error("renomear para vazio deveria falhar")
	}
	if err := colunas.Renomear(ctx, "Em negociação", strings.Repeat("x", 41)); err == nil {
		t.Error("renomear com 41 chars deveria falhar")
	}
}

func TestColunasReordenar(t *testing.T) {
	ctx := context.Background()
	colunas, _ := abreTesteColunas(t)

	cs, _ := colunas.List(ctx)
	invertida := make([]string, len(cs))
	copy(invertida, cs)
	for i, j := 0, len(invertida)-1; i < j; i, j = i+1, j-1 {
		invertida[i], invertida[j] = invertida[j], invertida[i]
	}
	if err := colunas.Reordenar(ctx, invertida); err != nil {
		t.Fatalf("reordenar: %v", err)
	}
	nova, _ := colunas.List(ctx)
	for i := range invertida {
		if nova[i] != invertida[i] {
			t.Fatalf("ordem não gravou: %v", nova)
		}
	}

	// Lista incompleta, com desconhecida ou repetida recusa.
	if err := colunas.Reordenar(ctx, cs[:5]); err == nil {
		t.Error("lista incompleta deveria falhar")
	}
	ruim := append([]string{"Não existe"}, cs...)
	if err := colunas.Reordenar(ctx, ruim); err == nil {
		t.Error("lista com coluna desconhecida deveria falhar")
	}
	dup := append([]string{}, cs...)
	dup[1] = dup[0]
	if err := colunas.Reordenar(ctx, dup); err == nil {
		t.Error("lista com repetida deveria falhar")
	}
}
