package rota360

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFunisCRM(t *testing.T) {
	c, _ := servidorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/crm/funis" {
			t.Errorf("caminho errado: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"funis":  []FunilCRM{{ID: "f1", Nome: "Relacionamento", Situacao: "ativo"}},
			"etapas": []EtapaCRM{{ID: "e1", FunilID: "f1", Nome: "Não atendido", Ordem: 1, Tipo: ""}},
			"motivos_perda": []MotivoPerdaCRM{
				{ID: "m1", Nome: "(OUT) Sem interesse nos serviços"},
			},
		})
	})
	f, e, m, err := c.FunisCRM(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 || f[0].Nome != "Relacionamento" || len(e) != 1 || e[0].FunilID != "f1" || len(m) != 1 {
		t.Fatalf("payload decodificado errado: %+v %+v %+v", f, e, m)
	}
}

func TestNegociacoesCRMQuery(t *testing.T) {
	c, _ := servidorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("funil_id") != "f1" || q.Get("status") != "aberta" {
			t.Errorf("query errada: %v", r.URL.RawQuery)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"itens": []NegociacaoCRM{{
				ID: "n1", FunilID: "f1", EtapaID: "e1", Titulo: "Ticket #68764 · Cancelamento",
			}},
			"total": 1,
		})
	})
	p, err := c.NegociacoesCRM(context.Background(), "f1", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 1 || len(p.Itens) != 1 || p.Itens[0].Titulo == "" {
		t.Fatalf("página errada: %+v", p)
	}
}

func TestNegociacoesCRMStatusPadrao(t *testing.T) {
	c, _ := servidorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("status") != "aberta" {
			t.Errorf("status default esperado, veio %q", r.URL.Query().Get("status"))
		}
		json.NewEncoder(w).Encode(map[string]any{"itens": []NegociacaoCRM{}})
	})
	if _, err := c.NegociacoesCRM(context.Background(), "", ""); err != nil {
		t.Fatal(err)
	}
}

func TestCardCrmTicketVazio(t *testing.T) {
	c, _ := servidorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/helpdesk/tickets/uuid-1/card-crm") {
			t.Errorf("caminho errado: %s", r.URL.Path)
		}
		w.Write([]byte("null"))
	})
	corpo, tem, err := c.CardCrmTicket(context.Background(), "uuid-1")
	if err != nil || tem {
		t.Fatalf("esperava sem card: tem=%v err=%v corpo=%s", tem, err, corpo)
	}
}

func TestCriarNegociacaoExigeFunil(t *testing.T) {
	c, _ := servidorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("não deveria chamar o servidor sem funil")
	})
	if _, err := c.CriarNegociacao(context.Background(), map[string]any{"titulo": "x"}); err == nil {
		t.Fatal("funil_id vazio deveria falhar sem sair daqui")
	}
}

func TestCriarNegociacao(t *testing.T) {
	c, _ := servidorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/crm/negociacoes" {
			t.Errorf("pedido errado: %s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), `"funil_id":"f1"`) {
			t.Errorf("corpo sem funil_id: %s", b)
		}
		json.NewEncoder(w).Encode(NegociacaoCRM{ID: "novo-1", Titulo: "Ticket #1 · Cancelamento"})
	})
	n, err := c.CriarNegociacao(context.Background(), map[string]any{"funil_id": "f1", "titulo": "Ticket #1 · Cancelamento"})
	if err != nil {
		t.Fatal(err)
	}
	if n.ID != "novo-1" {
		t.Fatalf("negociação errada: %+v", n)
	}
}

func TestMoverNegociacao(t *testing.T) {
	c, _ := servidorFalso(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/crm/negociacoes/n1/mover" {
			t.Errorf("caminho errado: %s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), `"para_etapa_id":"e2"`) {
			t.Errorf("corpo errado: %s", b)
		}
		w.WriteHeader(http.StatusOK)
	})
	if err := c.MoverNegociacao(context.Background(), "n1", "e2"); err != nil {
		t.Fatal(err)
	}
	if err := c.MoverNegociacao(context.Background(), "", "e2"); err == nil {
		t.Fatal("id vazio deveria falhar")
	}
}
