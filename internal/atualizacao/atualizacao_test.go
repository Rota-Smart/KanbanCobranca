package atualizacao

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerificarSemNovaVersao(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/versao.txt" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(VersaoApp + "\n"))
	}))
	defer srv.Close()
	info, err := NovoCliente(srv.URL).Verificar(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.TemNova || info.VersaoNova != VersaoApp {
		t.Errorf("info: %+v", info)
	}
}

func TestVerificarComNovaVersaoE404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("99.0.0"))
	}))
	defer srv.Close()
	info, err := NovoCliente(srv.URL).Verificar(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !info.TemNova || info.VersaoNova != "99.0.0" {
		t.Errorf("info: %+v", info)
	}

	if _, err := NovoCliente("http://127.0.0.1:1").Verificar(context.Background()); err == nil {
		t.Error("fonte inacessível deveria falhar")
	}
}

func TestBaixarEInstalar(t *testing.T) {
	exeFake := strings.Repeat("M", 2<<20) // > 1 MB
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rotasuite.exe" || r.URL.Path == "/rota-smart.exe" {
			w.Write([]byte(exeFake))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	dir := t.TempDir()
	destino := filepath.Join(dir, "novo.exe")
	if err := NovoCliente(srv.URL).Baixar(context.Background(), destino, ""); err != nil {
		t.Fatal(err)
	}

	// Simula a troca: exeAtual é um arquivo existente (não em execução no teste).
	exeAtual := filepath.Join(dir, "rota-smart.exe")
	if err := os.WriteFile(exeAtual, []byte("velho"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Instalar(exeAtual, destino); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(exeAtual)
	if err != nil || len(got) != 2<<20 {
		t.Errorf("exe trocado errado: %d bytes, %v", len(got), err)
	}
	if _, err := os.Stat(exeAtual + ".old"); err != nil {
		t.Errorf("backup .old ausente: %v", err)
	}
	LimparVelho(exeAtual)
	if _, err := os.Stat(exeAtual + ".old"); !os.IsNotExist(err) {
		t.Error(".old deveria ter sido removido")
	}

	// Falha não destrói o binário atual.
	badDestino := filepath.Join(dir, "inexistente.exe")
	if err := Instalar(exeAtual, badDestino); err == nil {
		t.Error("esperava erro ao instalar arquivo inexistente")
	}
	if got, _ := os.ReadFile(exeAtual); len(got) != 2<<20 {
		t.Error("exe atual foi perdido em falha")
	}
}
