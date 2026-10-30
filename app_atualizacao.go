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

// Eventos emitidos ao frontend durante a atualização em background.
const (
	evAtualizaProgresso  = "atualizacao:progresso"  // {baixados, total, percento}
	evAtualizaPronto     = "atualizacao:pronto"     // {versao} — instalado, o app reinicia
	evAtualizaErro       = "atualizacao:erro"       // {mensagem}
	evAtualizaDisponivel = "atualizacao:disponivel" // {versao} — monitor achou versão nova
)

// MonitorAtualizacao checa a fonte no arranque (após 30s, para não disputar
// banda com o boot) e a cada 10 minutos; ao achar versão nova, emite
// atualizacao:disponivel UMA vez por versão — o usuário decide quando
// atualizar pelo botão do aviso. Chamado do startup.
func (a *App) MonitorAtualizacao() {
	go func() {
		time.Sleep(30 * time.Second)
		avisada := ""
		checar := func() {
			ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
			defer cancel()
			info, err := atualizacao.NovoCliente(a.fonteAtualizacao()).Verificar(ctx)
			if err != nil || !info.TemNova || info.VersaoNova == avisada {
				return
			}
			avisada = info.VersaoNova
			wailsRuntime.EventsEmit(a.ctx, evAtualizaDisponivel, map[string]string{"versao": info.VersaoNova})
		}
		checar()
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-t.C:
				checar()
			}
		}
	}()
}

// percento devolve 0–100, ou -1 quando o total é desconhecido.
func percento(baixados, total int64) int {
	if total <= 0 {
		return -1
	}
	return int(100 * baixados / total)
}

// InstalarAtualizacao valida a versão nova e devolve na hora; o download
// (dezenas de MB) roda em goroutine — sair da tela de Configurações, ou até
// navegar no board, não interrompe. Ao terminar, instala e reinicia sozinho,
// anunciando cada fase pelos eventos atualizacao:*.
func (a *App) InstalarAtualizacao() (string, error) {
	a.muAtualiza.Lock()
	if a.baixandoAtualizacao {
		a.muAtualiza.Unlock()
		return "", fmt.Errorf("já há uma atualização baixando em segundo plano")
	}
	a.baixandoAtualizacao = true
	a.muAtualiza.Unlock()

	info, err := a.VerificarAtualizacao()
	if err != nil {
		a.muAtualiza.Lock()
		a.baixandoAtualizacao = false
		a.muAtualiza.Unlock()
		return "", friendly("InstalarAtualizacao", err)
	}
	if !info.TemNova {
		a.muAtualiza.Lock()
		a.baixandoAtualizacao = false
		a.muAtualiza.Unlock()
		return "", fmt.Errorf("você já está na versão mais recente (%s)", atualizacao.VersaoApp)
	}

	go a.baixarEInstalar(info)
	return info.VersaoNova, nil
}

// baixarEInstalar roda em background: baixa o exe com progresso, troca o
// binário e reinicia o app.
func (a *App) baixarEInstalar(info atualizacao.Info) {
	defer func() {
		a.muAtualiza.Lock()
		a.baixandoAtualizacao = false
		a.muAtualiza.Unlock()
	}()

	exeAtual, err := atualizacao.CaminhoExe()
	if err != nil {
		wailsRuntime.EventsEmit(a.ctx, evAtualizaErro, map[string]string{"mensagem": friendly("InstalarAtualizacao", err).Error()})
		return
	}
	dir := filepath.Dir(exeAtual)
	novo := filepath.Join(dir, "rotasuite.novo.exe")

	cliente := atualizacao.NovoCliente(a.fonteAtualizacao())
	// o Timeout de 20s do cliente serve para metadados (versao.txt); o exe
	// tem ~79 MB — com Timeout=0, quem limita é o ctx: 15 min bastam para
	// conexões bem lentas (o download é streming; 120s matava links lentos
	// no meio do caminho).
	cliente.HTTP.Timeout = 0
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Minute)
	defer cancel()
	ultimo := time.Time{}
	err = cliente.BaixarProgresso(ctx, novo, info.VersaoNova, func(baixados, total int64) {
		// emite no máximo ~2x por segundo para não inundar a WebView —
		// inclusive quando total=0 (sem Content-Length), senão a tela fica
		// parada sem resposta nenhuma
		if total > 0 && time.Since(ultimo) < 500*time.Millisecond && baixados < total {
			return
		}
		if total <= 0 && time.Since(ultimo) < 500*time.Millisecond {
			return
		}
		ultimo = time.Now()
		wailsRuntime.EventsEmit(a.ctx, evAtualizaProgresso, map[string]any{
			"baixados": baixados, "total": total,
			"percento": percento(baixados, total),
		})
	})
	if err != nil {
		os.Remove(novo)
		wailsRuntime.EventsEmit(a.ctx, evAtualizaErro, map[string]string{"mensagem": friendly("InstalarAtualizacao", err).Error()})
		return
	}

	if err := atualizacao.Instalar(exeAtual, novo); err != nil {
		wailsRuntime.EventsEmit(a.ctx, evAtualizaErro, map[string]string{"mensagem": friendly("InstalarAtualizacao", err).Error()})
		return
	}
	wailsRuntime.EventsEmit(a.ctx, evAtualizaPronto, map[string]string{"versao": info.VersaoNova})

	// Relança o app novo e encerra o atual.
	args := os.Args[1:]
	cmd := exec.Command(exeAtual, args...)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		wailsRuntime.EventsEmit(a.ctx, evAtualizaErro, map[string]string{"mensagem": friendly("InstalarAtualizacao", err).Error()})
		return
	}
	wailsRuntime.Quit(a.ctx)
}
