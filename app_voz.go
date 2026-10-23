package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Fala→texto local (Vosk via vosk-browser no WebView2). O modelo
// (vosk-model-small-pt-br, ~46 MB, Apache-2.0) não vem com o app: o usuário
// baixa e escolhe a pasta nas Configurações; o app copia para a pasta de
// dados e serve os arquivos ao frontend em /vosk/.

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
	Caminho     string `json:"caminho"`
	Mensagem    string `json:"mensagem"`
}

// ModeloVozStatus informa se já há modelo de fala→texto configurado.
func (a *App) ModeloVozStatus() VozStatus {
	caminho := a.configGet(a.ctx, configModeloVoz)
	if caminho == "" {
		caminho = pastaModeloVoz() // caminho-padrão para onde o modelo é copiado
	}
	if caminho == "" || !ehPastaModeloVosk(caminho) {
		return VozStatus{Mensagem: "Nenhum modelo configurado — baixe o vosk-model-small-pt-br e escolha a pasta em Configurações."}
	}
	return VozStatus{Configurado: true, Caminho: caminho}
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
// pasta de dados do app e grava o caminho na config. Devolve o resumo.
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

// servirModeloVosk serve a pasta do modelo em /vosk/… para o frontend
// (vosk-browser busca os arquivos do modelo por HTTP). Só caminhos dentro
// da pasta do modelo são aceitos.
func (a *App) servirModeloVosk(w http.ResponseWriter, r *http.Request) {
	base := a.configGet(a.ctx, configModeloVoz)
	if base == "" {
		base = pastaModeloVoz()
	}
	if base == "" || !ehPastaModeloVosk(base) {
		http.Error(w, "modelo de voz não configurado", http.StatusNotFound)
		return
	}
	rel, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/vosk"))
	rel = strings.TrimPrefix(rel, "/")
	if err != nil || rel == "" || strings.Contains(rel, "..") {
		http.Error(w, "caminho inválido", http.StatusBadRequest)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, filepath.Join(base, filepath.FromSlash(rel)))
}
