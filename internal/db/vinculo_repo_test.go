package db

import (
	"context"
	"database/sql"
	"testing"
)

func newVinculoTestDB(t *testing.T) (*sql.DB, *TicketRepo, *VinculoRepo) {
	t.Helper()
	db, err := Init(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, NewTicketRepo(db), NewVinculoRepo(db)
}

func TestObservacoesSalvamELimpam(t *testing.T) {
	_, tr, _ := newVinculoTestDB(t)
	ctx := context.Background()
	id, err := tr.Save(ctx, ticketExemploPtr())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := tr.SetObservacoes(ctx, id, "ligar após 18h"); err != nil {
		t.Fatalf("SetObservacoes: %v", err)
	}
	got, err := tr.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Observacoes != "ligar após 18h" {
		t.Errorf("observações = %q, queria %q", got.Observacoes, "ligar após 18h")
	}

	// Apagar (string vazia) também atualiza.
	if err := tr.SetObservacoes(ctx, id, ""); err != nil {
		t.Fatalf("SetObservacoes vazio: %v", err)
	}
	got, _ = tr.Get(ctx, id)
	if got.Observacoes != "" {
		t.Errorf("observações deviam ter sido apagadas, ficaram %q", got.Observacoes)
	}

	// Card inexistente: erro explícito.
	if err := tr.SetObservacoes(ctx, 99999, "x"); err != ErrTicketNaoEncontrado {
		t.Errorf("queria ErrTicketNaoEncontrado, veio %v", err)
	}
}

func TestVinculosUpsertListRemove(t *testing.T) {
	_, tr, vr := newVinculoTestDB(t)
	ctx := context.Background()
	id, _ := tr.Save(ctx, ticketExemploPtr())

	// Sem vínculo: lista vazia, sem erro.
	vs, err := vr.List(ctx, id)
	if err != nil || len(vs) != 0 {
		t.Fatalf("lista inicial = %v, %v", vs, err)
	}

	if err := vr.Upsert(ctx, id, "uuid-1", "101", "Sem sinal"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	// Mesmo ticket de novo: atualiza assunto/número em vez de duplicar.
	if err := vr.Upsert(ctx, id, "uuid-1", "101-A", "Sem sinal no quintal"); err != nil {
		t.Fatalf("Upsert de novo: %v", err)
	}
	if err := vr.Upsert(ctx, id, "uuid-2", "102", "Renegociação"); err != nil {
		t.Fatalf("Upsert 2: %v", err)
	}
	vs, err = vr.List(ctx, id)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(vs) != 2 {
		t.Fatalf("queria 2 vínculos, veio %d", len(vs))
	}
	// Mais recente primeiro.
	if vs[0].Ticket360ID != "uuid-2" || vs[0].Assunto != "Renegociação" {
		t.Errorf("primeiro vínculo = %+v", vs[0])
	}
	if vs[1].Assunto != "Sem sinal no quintal" || vs[1].Ticket360Numero != "101-A" {
		t.Errorf("segundo vínculo = %+v", vs[1])
	}

	// Remove o principal (uuid-2): some da lista.
	removido, err := vr.Remove(ctx, vs[0].ID)
	if err != nil || removido.Ticket360ID != "uuid-2" {
		t.Fatalf("Remove = %v, %v", removido, err)
	}
	vs, _ = vr.List(ctx, id)
	if len(vs) != 1 || vs[0].Ticket360ID != "uuid-1" {
		t.Fatalf("pós-remove = %+v", vs)
	}
	if _, err := vr.Remove(ctx, vs[0].ID); err != nil {
		t.Fatalf("Remove 2: %v", err)
	}
	if _, err := vr.Remove(ctx, vs[0].ID); err != ErrVinculoNaoEncontrado {
		t.Errorf("remover de novo devia dar ErrVinculoNaoEncontrado, veio %v", err)
	}
}
