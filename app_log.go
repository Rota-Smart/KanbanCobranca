package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"rota-smart/internal/contasync"
)

// EnviarLogNuvem sobe o fim do app.log para a conta do site (tabela logs_app
// do banco de lá) — diagnóstico remoto quando o erro só acontece em outra
// máquina. Autentica com o token da conta pareada; sem pareamento não há
// para onde mandar.
func (a *App) EnviarLogNuvem() (string, error) {
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
	token, err := lerTokenConta()
	if err != nil || token == "" {
		return "", fmt.Errorf("conta não pareada — conecte o app à conta em ⚙ Configurações para enviar o log")
	}
	host, _ := os.Hostname()
	corpo, err := json.Marshal(map[string]string{"host": host, "conteudo": string(bruto)})
	if err != nil {
		return "", friendly("EnviarLogNuvem", err)
	}
	req, err := http.NewRequestWithContext(a.ctx, http.MethodPost, contasync.URLPadrao+"/log", bytes.NewReader(corpo))
	if err != nil {
		return "", friendly("EnviarLogNuvem", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	cli := &http.Client{Timeout: 30 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return "", friendly("EnviarLogNuvem", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return "", fmt.Errorf("o site recusou o log (HTTP %d): %s", resp.StatusCode, string(b))
	}
	slog.Info("log enviado para a conta do site", "host", host, "bytes", len(bruto))
	return fmt.Sprintf("%d bytes", len(bruto)), nil
}

// LimparLogLocal zera o registro de atividades: rotaciona o app.log em uso
// (o arquivo aberto não pode ser apagado no Windows) e remove o atual e os
// backups rotacionados. Devolve quantos bytes estavam ocupados antes.
// O log NÃO cresce infinito — rotaciona sozinho em 10 MB com no máximo 3
// backups —, mas o botão existe para limpar na hora (privacidade, suporte).
func (a *App) LimparLogLocal() (string, error) {
	dir := filepath.Join(filepath.Dir(a.dbPath), "logs")
	entradas, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("pasta de log não encontrada em %s: %w", dir, err)
	}
	var total int64
	for _, e := range entradas {
		if info, err := e.Info(); err == nil {
			total += info.Size()
		}
	}
	if logAtual != nil {
		if err := logAtual.Rotate(); err != nil {
			return "", friendly("LimparLogLocal", err)
		}
	}
	// Depois do Rotate o app.log virou app-<data>.log; apaga tudo que não é
	// o arquivo novo em uso (que ficou com 0 bytes).
	apagados := 0
	for _, e := range entradas {
		nome := e.Name()
		if nome == "app.log" || strings.HasPrefix(nome, "app-") {
			if err := os.Remove(filepath.Join(dir, nome)); err == nil {
				apagados++
			}
		}
	}
	slog.Info("log local limpo", "bytes", total, "arquivos", apagados)
	return fmt.Sprintf("%d bytes apagados (%d arquivos)", total, apagados), nil
}
