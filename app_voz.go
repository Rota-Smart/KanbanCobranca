package main

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Fala→texto local (Vosk via vosk-browser no WebView2). O modelo
// (vosk-model-small-pt, ~52 MB, Apache-2.0) vem embutido no app e é servido
// ao frontend em /vosk/; se o usuário escolher outro modelo nas Configurações
// (pasta copiada para a pasta de dados), ele tem precedência.

//go:embed all:modelos/vosk-model-small-pt-0.3
var modeloVozEmbutido embed.FS

const modeloVozEmbutidoRaiz = "modelos/vosk-model-small-pt-0.3"

const configModeloVoz = "vosk_model"

// pastaModeloVoz é onde o modelo copiado vive (sobrevive a mudanças na pasta original).
func pastaModeloVoz() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "RotaSmart", "vosk-model")
}

// VozStatus é o estado do modelo de voz para o frontend.
type VozStatus struct {
	Configurado bool   `json:"configurado"`
	Origem      string `json:"origem"` // "embutido" | "pasta"
	Caminho     string `json:"caminho"`
	Mensagem    string `json:"mensagem"`
}

// ModeloVozStatus informa o modelo de fala→texto em uso: embutido no app
// por padrão, ou a pasta escolhida nas Configurações (que tem precedência).
func (a *App) ModeloVozStatus() VozStatus {
	if caminho := a.configGet(a.ctx, configModeloVoz); caminho != "" && ehPastaModeloVosk(caminho) {
		return VozStatus{Configurado: true, Origem: "pasta", Caminho: caminho}
	}
	return VozStatus{
		Configurado: true,
		Origem:      "embutido",
		Caminho:     modeloVozEmbutidoRaiz,
		Mensagem:    "Modelo embutido no app (vosk-model-small-pt). Para usar outro, escolha a pasta abaixo.",
	}
}

// ehPastaModeloVosk valida o mínimo da estrutura de um modelo Vosk
// (padrão Kaldi: am/, conf/, graph/ ou final.mdl na raiz).
func ehPastaModeloVosk(caminho string) bool {
	st, err := os.Stat(caminho)
	if err != nil || !st.IsDir() {
		return false
	}
	for _, marca := range []string{"conf", "am", "final.mdl", "graph"} {
		if _, err := os.Stat(filepath.Join(caminho, marca)); err == nil {
			return true
		}
	}
	return false
}

// DefinirModeloVoz abre o diálogo de pasta, valida, copia o modelo para a
// pasta de dados do app e grava o caminho na config (passa a ter precedência
// sobre o embutido). Devolve o resumo.
func (a *App) DefinirModeloVoz() (string, error) {
	caminho, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Escolher a pasta do modelo Vosk (vosk-model-small-pt-br)",
	})
	if err != nil {
		return "", friendly("DefinirModeloVoz", err)
	}
	if caminho == "" {
		return "", nil // cancelado
	}
	if !ehPastaModeloVosk(caminho) {
		return "", fmt.Errorf("a pasta escolhida não parece um modelo Vosk (falta am/, conf/ ou final.mdl)")
	}
	destino := pastaModeloVoz()
	if destino == "" {
		return "", fmt.Errorf("não foi possível determinar a pasta de dados do app")
	}
	if err := os.RemoveAll(destino); err != nil {
		return "", friendly("DefinirModeloVoz", err)
	}
	if err := copiarPasta(caminho, destino); err != nil {
		return "", friendly("DefinirModeloVoz", err)
	}
	if a.config != nil {
		if err := a.config.Set(a.ctx, configModeloVoz, destino); err != nil {
			return "", friendly("DefinirModeloVoz", err)
		}
	}
	return "Modelo de voz configurado (copiado para " + destino + ").", nil
}

// LimparModeloVoz volta a usar o modelo embutido: limpa a config e apaga a
// cópia local da pasta escolhida pelo usuário.
func (a *App) LimparModeloVoz() error {
	if a.config != nil {
		if err := a.config.Set(a.ctx, configModeloVoz, ""); err != nil {
			return friendly("LimparModeloVoz", err)
		}
	}
	if destino := pastaModeloVoz(); destino != "" {
		if err := os.RemoveAll(destino); err != nil {
			return friendly("LimparModeloVoz", err)
		}
	}
	return nil
}

// copiarPasta copia a árvore de diretórios de origem para destino.
func copiarPasta(origem, destino string) error {
	return filepath.WalkDir(origem, func(caminho string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(origem, caminho)
		if err != nil {
			return err
		}
		alvo := filepath.Join(destino, rel)
		if d.IsDir() {
			return os.MkdirAll(alvo, 0o700)
		}
		dados, err := os.ReadFile(caminho)
		if err != nil {
			return err
		}
		return os.WriteFile(alvo, dados, 0o600)
	})
}

// servirModeloVosk serve o modelo em /vosk/… para o frontend (vosk-browser
// busca os arquivos do modelo por HTTP): da pasta escolhida pelo usuário se
// houver, senão do embed. Só caminhos dentro da pasta do modelo são aceitos.
func (a *App) servirModeloVosk(w http.ResponseWriter, r *http.Request) {
	rel, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/vosk"))
	rel = strings.TrimPrefix(rel, "/")
	if err != nil || rel == "" || strings.Contains(rel, "..") {
		http.Error(w, "caminho inválido", http.StatusBadRequest)
		return
	}

	if caminho := a.configGet(a.ctx, configModeloVoz); caminho != "" && ehPastaModeloVosk(caminho) {
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, filepath.Join(caminho, filepath.FromSlash(rel)))
		return
	}

	sub, err := fs.Sub(modeloVozEmbutido, modeloVozEmbutidoRaiz)
	if err != nil {
		http.Error(w, "modelo embutido indisponível", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.FileServer(http.FS(sub)).ServeHTTP(w, r)
}
