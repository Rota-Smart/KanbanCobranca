package backup

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// testDB abre um banco em memória com uma tabela e uma linha conhecida.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("abrir banco: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE charges (id INTEGER PRIMARY KEY, amount_cents INTEGER NOT NULL); INSERT INTO charges (amount_cents) VALUES (58400);`); err != nil {
		t.Fatalf("preparar banco: %v", err)
	}
	return db
}

func TestExportCriaCopiaLegivel(t *testing.T) {
	db := testDB(t)
	dest := filepath.Join(t.TempDir(), "copia.db")

	if err := Export(db, dest); err != nil {
		t.Fatalf("Export: %v", err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("arquivo de backup não existe: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("backup vazio")
	}

	aberta, err := sql.Open("sqlite", dest)
	if err != nil {
		t.Fatalf("abrir backup: %v", err)
	}
	defer aberta.Close()
	var total int64
	if err := aberta.QueryRow(`SELECT SUM(amount_cents) FROM charges`).Scan(&total); err != nil {
		t.Fatalf("consultar backup: %v", err)
	}
	if total != 58400 {
		t.Fatalf("total no backup = %d, quer 58400", total)
	}
}

func TestExportRejeitaCaminhoInvalido(t *testing.T) {
	db := testDB(t)
	if err := Export(db, ""); err == nil {
		t.Fatal("caminho vazio deveria falhar")
	}
	if err := Export(nil, "x.db"); err == nil {
		t.Fatal("banco nil deveria falhar")
	}
	// apóstrofo no caminho não pode quebrar o SQL nem sumir na cópia
	dest := filepath.Join(t.TempDir(), "banco'de teste.db")
	if err := Export(db, dest); err != nil {
		t.Fatalf("Export com apóstrofo: %v", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("backup com apóstrofo não criado: %v", err)
	}
}

func TestDailyBackupUmaVezPorDia(t *testing.T) {
	db := testDB(t)
	dir := t.TempDir()
	hoje := time.Date(2026, 10, 21, 9, 0, 0, 0, time.Local)

	dest, err := DailyBackup(db, dir, hoje)
	if err != nil {
		t.Fatalf("DailyBackup: %v", err)
	}
	if dest == "" || !strings.HasSuffix(dest, "rotasmart-2026-10-21.db") {
		t.Fatalf("destino inesperado: %q", dest)
	}

	// segunda chamada no mesmo dia: não refaz, não sobrescreve
	if err := os.WriteFile(dest, []byte("marcado"), 0o644); err != nil {
		t.Fatal(err)
	}
	repetido, err := DailyBackup(db, dir, hoje.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("DailyBackup 2ª chamada: %v", err)
	}
	if repetido != "" {
		t.Fatalf("esperava pular o backup do dia, gravou %q", repetido)
	}
	content, err := os.ReadFile(dest)
	if err != nil || string(content) != "marcado" {
		t.Fatal("backup do dia foi sobrescrito na segunda chamada")
	}
}

func TestPruneMantem14Backups(t *testing.T) {
	db := testDB(t)
	dir := t.TempDir()
	base := time.Date(2026, 10, 1, 8, 0, 0, 0, time.Local)

	// 20 dias de backups: sobra 14, os 6 mais antigos saem
	for i := 0; i < 20; i++ {
		if _, err := DailyBackup(db, dir, base.Add(time.Duration(i)*24*time.Hour)); err != nil {
			t.Fatalf("DailyBackup dia %d: %v", i, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != maxBackups {
		t.Fatalf("arquivos restantes = %d, quer %d", len(entries), maxBackups)
	}
	// o mais antigo restante é o dia 7 (0-indexado), não o dia 0
	if _, err := os.Stat(filepath.Join(dir, "rotasmart-2026-10-01.db")); !os.IsNotExist(err) {
		t.Fatal("backup mais antigo deveria ter sido removido")
	}
	if _, err := os.Stat(filepath.Join(dir, "rotasmart-2026-10-20.db")); err != nil {
		t.Fatal("backup mais recente deveria existir")
	}
}
