package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// EnviarLogNuvem sobe o app.log (e, se houver, as rotações .1) para a pasta
// logs/ do repositório de sincronização — usado para diagnóstico remoto
// quando o erro só acontece em outra máquina. Envia no máximo o fim do log
// (tail de 900 KB) para não estourar a API de conteúdo.
func (a *App) EnviarLogNuvem() (string, error) {
	if err := a.syncConfigurado(); err != nil {
		return "", friendly("EnviarLogNuvem", err)
	}
	dir := filepath.Join(filepath.Dir(a.dbPath), "logs")
	local := filepath.Join(dir, "app.log")
	bruto, err := os.ReadFile(local)
	if err != nil {
		return "", fmt.Errorf("log não encontrado em %s: %w", local, err)
	}
	const tailMax = 900 << 10
	if len(bruto) > tailMax {
		bruto = bruto[len(bruto)-tailMax:]
	}
	tmp := filepath.Join(dir, "app-log-nuvem.tmp")
	if err := os.WriteFile(tmp, bruto, 0o600); err != nil {
		return "", friendly("EnviarLogNuvem", err)
	}
	defer os.Remove(tmp)

	host, _ := os.Hostname()
	caminho := "logs/" + host + "-app.log"
	cli := a.syncCliente()
	cli.Caminho = caminho
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
	defer cancel()
	msg := fmt.Sprintf("log de %s (tail %d bytes)", host, len(bruto))
	if err := cli.Enviar(ctx, tmp, msg); err != nil {
		return "", friendly("EnviarLogNuvem", err)
	}
	slog.Info("log enviado para a nuvem", "repo", cli.Repo, "caminho", caminho, "bytes", len(bruto))
	return caminho, nil
}
