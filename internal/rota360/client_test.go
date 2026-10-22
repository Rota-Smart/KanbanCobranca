package rota360

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func servidorFalso(t *testing.T, handler http.HandlerFunc) (*Cliente, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	c := NovoCliente(&Cookie{Nome: "session", Valor: "abc"})
	c.base = srv.URL
	t.Cleanup(srv.Close)
	return c, srv
}

func TestNovoClienteExigeSessao(t *testing.T) {
	c := NovoCliente(nil)
	if c.Autenticado() {
		t.Fatal("cliente sem cookie não deveria estar autenticado")
	}
	var out any
	if err := c.get(context.Background(), "/api/x", &out); err != ErrSemSessao {
		t.Fatalf("esperava ErrSemSessao, veio %v", err)
	}
}

func TestBuscarClientes(t *testing.T) {
	c, _ := servidorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Cookie"); !strings.Contains(got, "session=abc") {
			t.Errorf("cookie ausente: %q", got)
		}
		if r.URL.Query().Get("filtro") != "codcliente" {
			t.Errorf("filtro errado: %q", r.URL.Query().Get("filtro"))
		}
		json.NewEncoder(w).Encode([]ResumoCliente{{CodCliente: 24755, Nome: "RESOLUTE"}})
	})
	res, err := c.BuscarClientes(context.Background(), "24755", "codcliente")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Nome != "RESOLUTE" {
		t.Fatalf("resultado errado: %+v", res)
	}
}

func TestFinanceiroClienteCodigoInvalido(t *testing.T) {
	c, _ := servidorFalso(t, func(w http.ResponseWriter, r *http.Request) {})
	if _, err := c.FinanceiroCliente(context.Background(), "abc"); err == nil {
		t.Fatal("código não numérico deveria falhar")
	}
}

func TestSessaoExpirada401(t *testing.T) {
	c, _ := servidorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	var out any
	if err := c.get(context.Background(), "/api/x", &out); err != ErrSessaoExpirada {
		t.Fatalf("esperava ErrSessaoExpirada, veio %v", err)
	}
}

func TestRetryEm500(t *testing.T) {
	chamadas := 0
	c, _ := servidorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		chamadas++
		if chamadas == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]int{"ok": 1})
	})
	var out map[string]int
	if err := c.get(context.Background(), "/api/x", &out); err != nil {
		t.Fatal(err)
	}
	if chamadas != 2 {
		t.Fatalf("esperava 2 chamadas (retry), veio %d", chamadas)
	}
}

func TestParseCookieColado(t *testing.T) {
	casos := []struct {
		txt, querNome string
		querErro      bool
	}{
		{"gskvalormuitolongo", "session", false},
		{"session=valor123", "session", false},
		{"outro=x; session=valor123; a=b", "session", false},
		{"a=1; b=2; c=3", "", true},
		{"  ", "", true},
	}
	for _, c := range casos {
		got, err := ParseCookieColado(c.txt)
		if c.querErro {
			if err == nil {
				t.Errorf("%q: esperava erro", c.txt)
			}
			continue
		}
		if err != nil || got.Nome != c.querNome {
			t.Errorf("%q: veio %+v, %v", c.txt, got, err)
		}
	}
}

func TestDescriptografaValorAppBound(t *testing.T) {
	_, err := descriptografaValor([]byte("v20blob"), []byte("chave"))
	if err == nil || !strings.Contains(err.Error(), "App-Bound") {
		t.Fatalf("esperava erro de App-Bound, veio %v", err)
	}
}
