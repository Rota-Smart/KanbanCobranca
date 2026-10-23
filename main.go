package main

import (
	"context"
	"embed"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"gopkg.in/natefinch/lumberjack.v2"

	"rota-smart/internal/atualizacao"
	"rota-smart/internal/backup"
	"rota-smart/internal/db"
	"rota-smart/internal/service"
)

//go:embed all:frontend/dist
var frontendFS embed.FS

// setupLogger instala o slog em arquivo rotacionado ao lado do banco.
// Apenas IDs e códigos de erro são registrados — nenhum nome, telefone,
// endereço ou valor financeiro (Etapa 8).
func setupLogger(dbPath string) {
	logDir := filepath.Join(filepath.Dir(dbPath), "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return // sem arquivo de log, o slog cai no stderr padrão
	}
	logger := slog.New(slog.NewJSONHandler(&lumberjack.Logger{
		Filename:   filepath.Join(logDir, "app.log"),
		MaxSize:    10, // MB
		MaxBackups: 3,
		MaxAge:     28, // dias
		Compress:   true,
	}, nil))
	slog.SetDefault(logger)
}

func main() {
	// Remove o backup .old de uma autoatualização anterior.
	if exe, err := os.Executable(); err == nil {
		atualizacao.LimparVelho(exe)
	}
	path, err := db.DefaultPath()
	if err != nil {
		log.Fatalf("caminho do banco: %v", err)
	}
	setupLogger(path)
	aplicarSyncPendente(path) // promove o banco baixado do repo, se houver
	database, err := db.Init(path)
	if err != nil {
		log.Fatalf("db init: %v", err)
	}
	defer database.Close()

	// Backup automático ao abrir: no máximo um por dia, mantendo os últimos 14.
	if _, err := backup.DailyBackup(database, filepath.Join(filepath.Dir(path), "backups"), time.Now()); err != nil {
		slog.Warn("backup diário falhou", "err", err)
	}

	propostas := service.NewPropostaService(
		func() time.Time { return time.Now() },
		func() bool { return os.Getenv("GROQ_API_KEY") != "" },
	)
	app := NewApp(database, propostas)
	app.DefinirCaminhoBanco(path)

	// Integração Rota 360 + Groq + parâmetros de negociação.
	config := db.NewConfigRepo(database)
	groq := service.NovoGroqCliente(
		func() string {
			if v, _ := config.Get(context.Background(), "groq_chave"); v != "" {
				return v
			}
			return os.Getenv("GROQ_API_KEY")
		},
		func() string {
			v, _ := config.Get(context.Background(), "groq_modelo")
			return v
		},
	)
	app.WireIntegracao(
		config,
		db.NewParametroRepo(database),
		db.NewCliente360Repo(database),
		groq,
		service.NovoAvaliador(db.NewCliente360Repo(database), db.NewCobrancaRepo(database), time.Now),
	)

	err = wails.Run(&options.App{
		Title:  "Kanban Cobrança",
		Width:  1024,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets: frontendFS,
		},
		OnStartup:  app.startup,
		OnShutdown: func(context.Context) {},
		Bind:       []interface{}{app},
	})
	if err != nil {
		slog.Error("app run", "err", err)
		log.Fatalf("app run: %v", err)
	}
}
