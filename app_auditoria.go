package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"rota-smart/internal/atualizacao"
	"rota-smart/internal/db"
	"rota-smart/internal/service"
)

// registrarAuditoria grava uma linha no log append-only do assistente.
// Falha silenciosa: auditoria é acessória e nunca deve atrapalhar o chat.
func (a *App) registrarAuditoria(chatID int64, origem, comando, mensagem, resposta string, res *service.ChatResultado, duracao time.Duration) {
	if a.auditoria == nil {
		return
	}
	reg := db.AuditoriaChat{
		ChatID:    chatID,
		Origem:    origem,
		Comando:   comando,
		Mensagem:  mensagem,
		Resposta:  resposta,
		DuracaoMs: duracao.Milliseconds(),
	}
	if res != nil {
		if bs, err := json.Marshal(res.Ferramentas); err == nil && res.Ferramentas != nil {
			reg.Ferramentas = string(bs)
		}
		reg.PromptTokens = res.PromptTokens
		reg.CompletionTokens = res.CompletionTokens
		reg.Rodadas = res.Rodadas
	}
	// Mensagem/resposta com teto generoso: o log é para análise, não para
	// estourar o banco com colas gigantes.
	reg.Mensagem = limitarBytesAuditoria(reg.Mensagem, 4000)
	reg.Resposta = limitarBytesAuditoria(reg.Resposta, 4000)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = a.auditoria.Registrar(ctx, reg)
}

func limitarBytesAuditoria(s string, max int) string {
	if len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// AuditoriaExportada é o documento JSON que o botão "salvar como" grava.
type AuditoriaExportada struct {
	VersaoApp string             `json:"versaoApp"`
	GeradoEm  string             `json:"geradoEm"`
	Resumo    db.Resumo          `json:"resumo"`
	Trocas    []db.AuditoriaChat `json:"trocas"`
}

// ExportarAuditoria abre o diálogo "salvar como" e grava o histórico completo
// do assistente (log append-only) num arquivo JSON escolhido pelo usuário.
// Devolve o caminho gravado.
func (a *App) ExportarAuditoria() (string, error) {
	if a.auditoria == nil {
		return "", fmt.Errorf("auditoria indisponível")
	}
	trocas, err := a.auditoria.Listar(a.ctx)
	if err != nil {
		return "", friendly("ExportarAuditoria", err)
	}
	resumo, err := a.auditoria.Resumo(a.ctx)
	if err != nil {
		return "", friendly("ExportarAuditoria", err)
	}
	caminho, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultFilename: fmt.Sprintf("rotasuite-auditoria-%s.json", time.Now().Format("2006-01-02")),
		Title:           "Salvar histórico do assistente",
		Filters: []runtime.FileFilter{
			{DisplayName: "JSON (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil {
		return "", friendly("ExportarAuditoria", err)
	}
	if caminho == "" {
		return "", nil // usuário cancelou
	}
	if filepath.Ext(caminho) == "" {
		caminho += ".json"
	}
	doc := AuditoriaExportada{
		VersaoApp: atualizacao.VersaoApp,
		GeradoEm:  time.Now().Format("2006-01-02 15:04:05"),
		Resumo:    resumo,
		Trocas:    trocas,
	}
	bs, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", friendly("ExportarAuditoria", err)
	}
	if err := os.WriteFile(caminho, bs, 0600); err != nil {
		return "", friendly("ExportarAuditoria", err)
	}
	return caminho, nil
}
