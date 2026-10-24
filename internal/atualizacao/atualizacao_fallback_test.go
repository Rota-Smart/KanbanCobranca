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

// Sem o exe legado no nome atual: prefere o nome com versão na URL (imune a
// cache stale do CDN) e cai pro legado quando só ele existe.
func TestBaixarPreferirNomeComVersao(t *testing.T) {
	exeFake := strings.Repeat("M", 2<<20)
	var pedidos []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pedidos = append(pedidos, r.URL.Path)
		// Só o GENÉRICO rotasuite.exe existe: o nome com versão é 404 e o
		// cliente deve cair pra ele em vez de desistir.
		if r.URL.Path == "/rotasuite.exe" {
			w.Write([]byte(exeFake))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	destino := filepath.Join(t.TempDir(), "novo.exe")
	if err := NovoCliente(srv.URL).Baixar(context.Background(), destino, "9.9.9"); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(destino); err != nil || len(raw) != 2<<20 {
		t.Errorf("download com versão falhou: %d bytes, %v", len(raw), err)
	}
	if len(pedidos) == 0 || pedidos[0] != "/rotasuite-9.9.9.exe" {
		t.Errorf("deveria tentar o nome com versão primeiro; pedidos: %v", pedidos)
	}
}

// Só o nome antigo na release (releases anteriores a 1.4.4) ainda baixa.
func TestBaixarFallbackNomeAntigo(t *testing.T) {
	exeFake := strings.Repeat("M", 2<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rota-smart.exe" {
			w.Write([]byte(exeFake))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	destino := filepath.Join(t.TempDir(), "novo.exe")
	if err := NovoCliente(srv.URL).Baixar(context.Background(), destino, ""); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(destino); err != nil || len(raw) != 2<<20 {
		t.Errorf("fallback falhou: %d bytes, %v", len(raw), err)
	}
}
