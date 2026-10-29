package db

import (
	"context"
	"path/filepath"
	"testing"
)

// GetPorNumero360 é o fallback quando a IA passa o número do ticket do 360
// no lugar do id local do card.
func TestGetPorNumero360(t *testing.T) {
	database, err := Init(filepath.Join(t.TempDir(), "num360.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	tickets := NewTicketRepo(database)
	ctx := context.Background()

	id, err := tickets.Save(ctx, &Ticket{NomeCliente: "João", TipoTicket: "cobranca", ColunaAtual: "Cobrança"})
	if err != nil {
		t.Fatal(err)
	}
	if err := tickets.VincularTicket360(ctx, id, "uuid-1", "45231", "1234"); err != nil {
		t.Fatal(err)
	}

	t1, err := tickets.GetPorNumero360(ctx, 45231)
	if err != nil || t1 == nil || t1.ID != id {
		t.Fatalf("GetPorNumero360(45231) = %v, %v; quero card %d", t1, err, id)
	}

	if t2, err := tickets.GetPorNumero360(ctx, 99999); err != nil || t2 != nil {
		t.Fatalf("número inexistente deveria ser nil; veio %v, %v", t2, err)
	}
}
