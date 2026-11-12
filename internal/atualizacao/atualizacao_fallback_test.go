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

// Com a versão conhecida, o nome com versão na URL é baixado.
func TestBaixarNomeComVersao(t *testing.T) {
	exeFake := strings.Repeat("M", 2<<20)
	var pedidos []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pedidos = append(pedidos, r.URL.Path)
		if r.URL.Path == "/rotasuite-9.9.9.exe" {
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

// REGRESSÃO do incidente "na 1.82 baixou a 1.83": com a versão conhecida, o
// genérico rotasuite.exe NÃO é baixado — o cache do CDN pode estar servindo o
// exe da release anterior sob a mesma URL. 404 no nome com versão tem que
// falhar (o BaixarProgresso tenta de novo 3x), nunca cair pro genérico.
func TestBaixarRecusaGenericoComVersao(t *testing.T) {
	exeFake := strings.Repeat("M", 2<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rotasuite.exe" {
			w.Write([]byte(exeFake)) // genérico "antigo" disponível
			return
		}
		http.NotFound(w, r) // rotasuite-9.9.9.exe não propagou
	}))
	defer srv.Close()
	destino := filepath.Join(t.TempDir(), "novo.exe")
	if err := NovoCliente(srv.URL).Baixar(context.Background(), destino, "9.9.9"); err == nil {
		t.Fatal("deveria recusar o genérico quando a versão é conhecida")
	}
	if _, err := os.Stat(destino); err == nil {
		t.Error("nenhum arquivo deveria ter sido gravado")
	}
}

// Sem versão na chamada (instalações antigas), os nomes legados ainda valem.
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
