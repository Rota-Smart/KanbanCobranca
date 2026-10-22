package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"rota-smart/internal/atualizacao"
)

// Chave da fonte de atualização na tabela config.
const cfgFonteAtualizacao = "update_base"

// InfoAtualizacao é o estado devolvido ao frontend.
type InfoAtualizacao = atualizacao.Info

// fonteAtualizacao devolve a URL configurada (ou a padrão).
func (a *App) fonteAtualizacao() string {
	if base := a.configGet(a.ctx, cfgFonteAtualizacao); base != "" {
		return base
	}
	return atualizacao.BasePadrao
}

// VerificarAtualizacao compara a versão local com a publicada na fonte.
func (a *App) VerificarAtualizacao() (atualizacao.Info, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 25*time.Second)
	defer cancel()
	return atualizacao.NovoCliente(a.fonteAtualizacao()).Verificar(ctx)
}

// SetFonteAtualizacao grava a URL-base da fonte (vazio volta ao padrão).
func (a *App) SetFonteAtualizacao(base string) error {
	if a.config == nil {
		return fmt.Errorf("configuração indisponível")
	}
	return a.config.Set(a.ctx, cfgFonteAtualizacao, base)
}

// GetFonteAtualizacao devolve a URL-base em uso.
func (a *App) GetFonteAtualizacao() string { return a.fonteAtualizacao() }

// InstalarAtualizacao baixa o exe novo, troca o binário em execução e
// reinicia o app. Em caso de sucesso o processo atual termina.
func (a *App) InstalarAtualizacao() (string, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 120*time.Second)
	defer cancel()

	info, err := a.VerificarAtualizacao()
	if err != nil {
		return "", friendly("InstalarAtualizacao", err)
	}
	if !info.TemNova {
		return "", fmt.Errorf("você já está na versão mais recente (%s)", atualizacao.VersaoApp)
	}

	exeAtual, err := atualizacao.CaminhoExe()
	if err != nil {
		return "", friendly("InstalarAtualizacao", err)
	}
	dir := filepath.Dir(exeAtual)
	novo := filepath.Join(dir, "rota-smart.novo.exe")

	cliente := atualizacao.NovoCliente(a.fonteAtualizacao())
	if err := cliente.Baixar(ctx, novo); err != nil {
		return "", friendly("InstalarAtualizacao", err)
	}
	if err := atualizacao.Instalar(exeAtual, novo); err != nil {
		return "", friendly("InstalarAtualizacao", err)
	}

	// Relança o app novo e encerra o atual.
	comando := exeAtual
	args := os.Args[1:]
	cmd := exec.Command(comando, args...)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		return "", friendly("InstalarAtualizacao", err)
	}
	wailsRuntime.Quit(a.ctx)
	return info.VersaoNova, nil
}
