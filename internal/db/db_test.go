package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func newTestDB(t *testing.T) (string, *TicketRepo, *CobrancaRepo) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Init(path)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return path, NewTicketRepo(db), NewCobrancaRepo(db)
}

func ticketExemplo() Ticket {
	return Ticket{NomeCliente: "Maria Souza", TipoTicket: "Cobrança", ColunaAtual: "Fazer contato"}
}

func TestMigracoesDoZeroEIdempotentes(t *testing.T) {
	path, _, _ := newTestDB(t)

	// Reabrir o mesmo banco: migrate roda de novo e não altera nada.
	db2, err := Init(path)
	if err != nil {
		t.Fatalf("reabrir banco: %v", err)
	}
	db2.Close()
}

func TestSaveListGetDelete(t *testing.T) {
	_, tr, _ := newTestDB(t)
	ctx := context.Background()

	id1, err := tr.Save(ctx, ticketExemploPtr())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	id2, err := tr.Save(ctx, ticketExemploPtr())
	if err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	if id1 == id2 {
		t.Fatal("ids deveriam ser distintos")
	}

	got, err := tr.Get(ctx, id1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.NomeCliente != "Maria Souza" || got.ColunaAtual != "Fazer contato" {
		t.Errorf("ticket inesperado: %+v", got)
	}
	if got.Posicao != 0 {
		t.Errorf("primeiro card da coluna deveria ter posição 0, got %d", got.Posicao)
	}

	list, err := tr.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("esperava 2 tickets, got %d", len(list))
	}

	if err := tr.Delete(ctx, id1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := tr.Get(ctx, id1); !errors.Is(err, ErrTicketNaoEncontrado) {
		t.Errorf("esperava ErrTicketNaoEncontrado, got %v", err)
	}
}

func TestMoveReordenaEntreColunas(t *testing.T) {
	_, tr, _ := newTestDB(t)
	ctx := context.Background()

	var ids []int64
	for i := 0; i < 3; i++ {
		tk := ticketExemplo()
		tk.ColunaAtual = "A"
		id, err := tr.Save(ctx, &tk)
		if err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	// Coluna B com um card para conferir abertura de espaço.
	tkB := ticketExemplo()
	tkB.ColunaAtual = "B"
	idB, err := tr.Save(ctx, &tkB)
	if err != nil {
		t.Fatalf("Save B: %v", err)
	}

	// Move o card 0 de A (posição 0) para B na posição 0.
	if err := tr.Move(ctx, ids[0], "A", "B", 0); err != nil {
		t.Fatalf("Move: %v", err)
	}

	list, err := tr.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	pos := map[int64]int{}
	for _, tk := range list {
		pos[tk.ID] = tk.Posicao
	}
	if pos[ids[0]] != 0 || pos[idB] != 1 {
		t.Errorf("esperava ids[0] em B/0 e idB em B/1, got %d e %d", pos[ids[0]], pos[idB])
	}
	// Cards restantes em A fecharam o buraco.
	if pos[ids[1]] != 0 || pos[ids[2]] != 1 {
		t.Errorf("esperava A: 0 e 1, got %d e %d", pos[ids[1]], pos[ids[2]])
	}
}

func TestMoveDentroDaMesmaColuna(t *testing.T) {
	_, tr, _ := newTestDB(t)
	ctx := context.Background()

	var ids []int64
	for i := 0; i < 3; i++ {
		tk := ticketExemplo()
		tk.ColunaAtual = "A"
		id, err := tr.Save(ctx, &tk)
		if err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
		ids = append(ids, id)
	}

	// Move o terceiro (posição 2) para a posição 0.
	if err := tr.Move(ctx, ids[2], "A", "A", 0); err != nil {
		t.Fatalf("Move: %v", err)
	}

	list, err := tr.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	pos := map[int64]int{}
	for _, tk := range list {
		pos[tk.ID] = tk.Posicao
	}
	if pos[ids[2]] != 0 || pos[ids[0]] != 1 || pos[ids[1]] != 2 {
		t.Errorf("reordenação inesperada: %v", pos)
	}
}

func TestOnDeleteCascade(t *testing.T) {
	_, tr, cr := newTestDB(t)
	ctx := context.Background()

	id, err := tr.Save(ctx, ticketExemploPtr())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := cr.SaveAtiva(ctx, &Cobranca{
		TicketID:           id,
		TotalAbertoCents:   10000,
		NumeroVezes:        2,
		ParceladoCents:     5000,
		UltimaParcelaCents: 5000,
		FinalCents:         5000,
	}); err != nil {
		t.Fatalf("SaveAtiva: %v", err)
	}

	if err := tr.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	hist, err := cr.History(ctx, id)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) != 0 {
		t.Errorf("ON DELETE CASCADE deveria apagar as cobranças, restaram %d", len(hist))
	}
}

func TestSaveAtivaEEIdempotenteNoIndiceUnico(t *testing.T) {
	_, tr, cr := newTestDB(t)
	ctx := context.Background()

	id, err := tr.Save(ctx, ticketExemploPtr())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	c1 := &Cobranca{TicketID: id, TotalAbertoCents: 10000, NumeroVezes: 2, ParceladoCents: 5000, UltimaParcelaCents: 5000, FinalCents: 5000}
	if _, err := cr.SaveAtiva(ctx, c1); err != nil {
		t.Fatalf("SaveAtiva 1: %v", err)
	}
	c2 := &Cobranca{TicketID: id, TotalAbertoCents: 20000, NumeroVezes: 4, ParceladoCents: 5000, UltimaParcelaCents: 5000, FinalCents: 5000}
	if _, err := cr.SaveAtiva(ctx, c2); err != nil {
		t.Fatalf("SaveAtiva 2: %v", err)
	}

	ativa, err := cr.GetAtiva(ctx, id)
	if err != nil {
		t.Fatalf("GetAtiva: %v", err)
	}
	if ativa == nil || ativa.TotalAbertoCents != 20000 {
		t.Errorf("cobrança ativa deveria ser a segunda, got %+v", ativa)
	}

	hist, err := cr.History(ctx, id)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("esperava 2 cobranças no histórico, got %d", len(hist))
	}
	nAtivas := 0
	for _, c := range hist {
		if c.Ativa {
			nAtivas++
		}
	}
	if nAtivas != 1 {
		t.Errorf("esperava exatamente 1 ativa, got %d", nAtivas)
	}
}

func TestGetAtivaSemAcordo(t *testing.T) {
	_, tr, cr := newTestDB(t)
	ctx := context.Background()

	id, err := tr.Save(ctx, ticketExemploPtr())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	c, err := cr.GetAtiva(ctx, id)
	if err != nil {
		t.Fatalf("GetAtiva: %v", err)
	}
	if c != nil {
		t.Errorf("esperava nil, got %+v", c)
	}
}

func TestUpdateStatus(t *testing.T) {
	_, tr, cr := newTestDB(t)
	ctx := context.Background()

	id, err := tr.Save(ctx, ticketExemploPtr())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	cid, err := cr.SaveAtiva(ctx, &Cobranca{TicketID: id, TotalAbertoCents: 10000, NumeroVezes: 1, ParceladoCents: 10000, UltimaParcelaCents: 10000, FinalCents: 10000})
	if err != nil {
		t.Fatalf("SaveAtiva: %v", err)
	}
	if err := cr.UpdateStatus(ctx, cid, "aceita"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	ativa, err := cr.GetAtiva(ctx, id)
	if err != nil {
		t.Fatalf("GetAtiva: %v", err)
	}
	if ativa.Status != "aceita" {
		t.Errorf("status deveria ser aceita, got %s", ativa.Status)
	}
	if err := cr.UpdateStatus(ctx, 9999, "aceita"); !errors.Is(err, ErrCobrancaNaoEncontrada) {
		t.Errorf("id inexistente deveria dar ErrCobrancaNaoEncontrada, got %v", err)
	}
}

func ticketExemploPtr() *Ticket {
	tk := ticketExemplo()
	return &tk
}
