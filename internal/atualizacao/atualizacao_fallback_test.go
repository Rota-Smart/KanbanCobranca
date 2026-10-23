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
	if err := NovoCliente(srv.URL).Baixar(context.Background(), destino); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(destino); err != nil || len(raw) != 2<<20 {
		t.Errorf("fallback falhou: %d bytes, %v", len(raw), err)
	}
}
