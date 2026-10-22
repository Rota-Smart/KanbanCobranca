package main

import (
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// MinimizarJanela minimiza a janela do app.
func (a *App) MinimizarJanela() {
	runtime.WindowMinimise(a.ctx)
}

// AlternarMaximizarJanela alterna entre maximizada e tamanho normal.
func (a *App) AlternarMaximizarJanela() {
	runtime.WindowToggleMaximise(a.ctx)
}

// JanelaMaximizada informa se a janela está maximizada.
func (a *App) JanelaMaximizada() bool {
	return runtime.WindowIsMaximised(a.ctx)
}

// AlternarTelaCheia alterna entre tela cheia e modo normal.
func (a *App) AlternarTelaCheia() {
	if runtime.WindowIsFullscreen(a.ctx) {
		runtime.WindowUnfullscreen(a.ctx)
	} else {
		runtime.WindowFullscreen(a.ctx)
	}
}

// FecharApp encerra o aplicativo.
func (a *App) FecharApp() {
	runtime.Quit(a.ctx)
}
