// Package backup exporta cópias consistentes do banco SQLite via VACUUM INTO
// e mantém no máximo maxBackups arquivos no diretório de backups.
package backup

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxBackups = 14

// Export grava uma cópia consistente do banco (mesmo aberto) em destPath.
// O caminho entra embutido no SQL, então apóstrofos são escapados.
func Export(db *sql.DB, destPath string) error {
	if db == nil {
		return fmt.Errorf("backup: banco não aberto")
	}
	if strings.TrimSpace(destPath) == "" {
		return fmt.Errorf("backup: caminho de destino vazio")
	}
	escaped := strings.ReplaceAll(destPath, "'", "''")
	if _, err := db.Exec("VACUUM INTO '" + escaped + "'"); err != nil {
		return fmt.Errorf("backup: vacuum into: %w", err)
	}
	return nil
}

// dailyName é o nome do arquivo do backup do dia: rotasmart-AAAA-MM-DD.db.
func dailyName(t time.Time) string {
	return "rotasmart-" + t.Format("2006-01-02") + ".db"
}

// DailyBackup copia o banco para dir uma vez por dia: se o backup de hoje já
// existe, não faz nada. Depois remove os mais antigos, mantendo maxBackups.
// Retorna o caminho gravado ou "" quando o backup de hoje já existia.
func DailyBackup(db *sql.DB, dir string, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("backup: criar diretório: %w", err)
	}
	dest := filepath.Join(dir, dailyName(now))
	if _, err := os.Stat(dest); err == nil {
		if err := prune(dir); err != nil {
			return "", err
		}
		return "", nil // backup de hoje já feito
	}
	if err := Export(db, dest); err != nil {
		return "", err
	}
	if err := prune(dir); err != nil {
		return dest, err
	}
	return dest, nil
}

// prune mantém apenas os maxBackups arquivos mais recentes do diretório.
func prune(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("backup: ler diretório: %w", err)
	}
	var files []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".db") {
			files = append(files, e)
		}
	}
	if len(files) <= maxBackups {
		return nil
	}
	sort.Slice(files, func(i, j int) bool {
		li, _ := files[i].Info()
		lj, _ := files[j].Info()
		return li.ModTime().After(lj.ModTime())
	})
	for _, f := range files[maxBackups:] {
		if err := os.Remove(filepath.Join(dir, f.Name())); err != nil {
			return fmt.Errorf("backup: remover antigo: %w", err)
		}
	}
	return nil
}
