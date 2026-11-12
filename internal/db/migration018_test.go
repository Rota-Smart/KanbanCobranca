package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// migration018: os gatilhos da fila de sync carimbam a fila com o horário
// REAL da mudança (updated_at da linha) nas tabelas que o têm. Se a fila
// fosse carimbada com "agora", uma linha só reenfileirada (ex.: reajuste de
// posição dos outros cards ao mover um card) subia com horário fresco e a
// cópia velha dela vencia na nuvem uma alteração mais recente — o card
// voltava de coluna na conferência seguinte.
func TestGatilhoSyncCarimbaComHorarioDaLinha(t *testing.T) {
	db, err := Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO tickets (nome_cliente, tipo_ticket, coluna_atual) VALUES ('Maria', 'Cobrança', 'Fazer contato')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var id int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM tickets`).Scan(&id); err != nil {
		t.Fatalf("id: %v", err)
	}

	// Move com updated_at bem antigo (simula linha escrita há muito tempo).
	if _, err := db.ExecContext(ctx, `UPDATE tickets SET coluna_atual = 'Aguardando Assinatura',
		updated_at = '2026-01-01T12:00:00Z' WHERE id = ?`, id); err != nil {
		t.Fatalf("update: %v", err)
	}
	var carimbo string
	if err := db.QueryRowContext(ctx,
		`SELECT atualizado_em FROM sync_fila WHERE tabela = 'tickets' AND registro_id = ?`, id).Scan(&carimbo); err != nil {
		t.Fatalf("ler fila: %v", err)
	}
	if carimbo != "2026-01-01T12:00:00Z" {
		t.Fatalf("fila carimbada com %q; queria o updated_at real da linha", carimbo)
	}

	// Mudança nova atualiza o carimbo para o updated_at mais recente.
	if _, err := db.ExecContext(ctx, `UPDATE tickets SET coluna_atual = 'Cobrança',
		updated_at = ? WHERE id = ?`, time.Now().UTC().Format("2006-01-02T15:04:05Z"), id); err != nil {
		t.Fatalf("update 2: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT atualizado_em FROM sync_fila WHERE tabela = 'tickets' AND registro_id = ?`, id).Scan(&carimbo); err != nil {
		t.Fatalf("ler fila 2: %v", err)
	}
	if carimbo == "2026-01-01T12:00:00Z" {
		t.Fatalf("carimbo da fila não acompanhou a segunda mudança")
	}
}
