package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DefaultPath devolve o caminho padrão do banco:
// os.UserConfigDir()/RotaSmart/rotasmart.db
func DefaultPath() (string, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("obter config dir: %w", err)
	}
	return filepath.Join(cfgDir, "RotaSmart", "rotasmart.db"), nil
}

// Init abre o banco em path, aplica as migrações versionadas e devolve a conexão.
// Os pragmas ficam no DSN porque valem por conexão, não por banco.
func Init(path string) (*sql.DB, error) {
	// 0o700: os dados do banco só podem ser lidos pelo usuário atual.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("criar diretório do banco: %w", err)
	}
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("abrir sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("conectar ao banco: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
