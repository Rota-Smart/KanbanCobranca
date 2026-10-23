package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"rota-smart/internal/appdata"

	_ "modernc.org/sqlite"
)

// DefaultPath devolve o caminho padrão do banco:
// appdata.Dir()/rotasmart.db (portátil quando há RotaSmart-dados ao lado do exe).
func DefaultPath() (string, error) {
	dir, err := appdata.Dir()
	if err != nil {
		return "", fmt.Errorf("obter pasta de dados: %w", err)
	}
	return filepath.Join(dir, "rotasmart.db"), nil
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
