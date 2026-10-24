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
		{"gskvalormuitolongo", "rota360_auth", false},
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

func TestPesquisarTickets(t *testing.T) {
	itens := []Ticket360{
		{ID: "u1", Numero: "1001", Titulo: "Câmera offline", CodigoService: "24755", Solicitante: "RESOLUTE"},
		{ID: "u2", Numero: "1002", Titulo: "Sem sinal", CodigoService: "111", Solicitante: "OUTRO"},
		{ID: "u3", Numero: "1003", Titulo: "Duplicado", CodigoService: "24755", Solicitante: "RESOLUTE"},
	}
	views := 0
	c, _ := servidorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		views++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"total": len(itens), "itens": itens})
	})
	res, err := c.PesquisarTickets(context.Background(), "24755")
	if err != nil {
		t.Fatal(err)
	}
	if views < 2 {
		t.Errorf("esperava varrer várias views, rodou %d", views)
	}
	// 2 matches × 3 views, dedup por ID ⇒ 2 resultados.
	if len(res) != 2 {
		t.Fatalf("esperava 2 resultados dedup, veio %d: %+v", len(res), res)
	}

	res, err = c.PesquisarTickets(context.Background(), "resolute")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("busca insensível a caixa falhou: %+v", res)
	}

	res, err = c.PesquisarTickets(context.Background(), "1002")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].ID != "u2" {
		t.Fatalf("busca por número falhou: %+v", res)
	}

	if res, _ := c.PesquisarTickets(context.Background(), "   "); len(res) != 0 {
		t.Errorf("termo vazio deveria devolver vazio, veio %+v", res)
	}
}
