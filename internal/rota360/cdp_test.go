package rota360

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// servidorCDPFalso simula o Chrome: /json/list devolve as abas e o WebSocket
// responde a cada Runtime.evaluate com o próximo nome de `respostas` (o
// último repete). Devolve a porta do /json/list.
func servidorCDPFalso(t *testing.T, abas []alvoCDP, respostas ...string) int {
	t.Helper()
	upgrader := websocket.Upgrader{}
	var proxima atomic.Int64
	ws := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var msg struct {
			ID int `json:"id"`
		}
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		nome := ""
		if n := len(respostas); n > 0 {
			i := int(proxima.Load())
			if i >= n {
				i = n - 1
			}
			nome = respostas[i]
			proxima.Add(1)
		}
		_ = conn.WriteJSON(map[string]any{
			"id":     msg.ID,
			"result": map[string]any{"result": map[string]any{"type": "string", "value": nome}},
		})
	}))
	t.Cleanup(ws.Close)

	wsURL := "ws" + strings.TrimPrefix(ws.URL, "http")
	// O handler do /json/list lê o slice na hora do request, então dá para
	// apontar cada aba para o WebSocket real depois de criar o servidor.
	list := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := range abas {
			abas[i].WebSocketDebuggerURL = wsURL
		}
		json.NewEncoder(w).Encode(abas)
	}))
	t.Cleanup(list.Close)

	porta, _ := strconv.Atoi(strings.TrimPrefix(list.URL, "http://127.0.0.1:"))
	return porta
}

func TestUsuarioViaCDPAchaNomeNaAba360(t *testing.T) {
	porta := servidorCDPFalso(t, []alvoCDP{
		{Type: "page", URL: "https://www.google.com/"},
		{Type: "page", URL: "https://360.rotagroup.com.br/helpdesk/tickets"},
		{Type: "service_worker", URL: "https://360.rotagroup.com.br/sw.js"},
	}, "Thiago Lopes Pires")

	if nome := usuarioViaCDP(porta); nome != "Thiago Lopes Pires" {
		t.Errorf("nome: %q", nome)
	}
}

func TestUsuarioViaCDPSemAba360(t *testing.T) {
	porta := servidorCDPFalso(t, []alvoCDP{
		{Type: "page", URL: "https://www.google.com/"},
	})
	if nome := usuarioViaCDP(porta); nome != "" {
		t.Errorf("sem aba do 360 deveria devolver vazio, veio %q", nome)
	}
}

func TestUsuarioViaCDPNomeVazioProximaAba(t *testing.T) {
	// Primeira aba do 360 sem o nome (página ainda carregando): a segunda é usada.
	porta := servidorCDPFalso(t, []alvoCDP{
		{Type: "page", URL: "https://360.rotagroup.com.br/"},
		{Type: "page", URL: "https://360.rotagroup.com.br/helpdesk"},
	}, "", "Thiago Lopes Pires")
	if nome := usuarioViaCDP(porta); nome != "Thiago Lopes Pires" {
		t.Errorf("deveria usar a próxima aba com nome: %q", nome)
	}
}

func TestAvaliarUsuarioWSRespostaVazia(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var msg struct {
			ID int `json:"id"`
		}
		_ = conn.ReadJSON(&msg)
		_ = conn.WriteJSON(map[string]any{
			"id":     msg.ID,
			"result": map[string]any{"result": map[string]any{"type": "string", "value": ""}},
		})
	}))
	t.Cleanup(srv.Close)

	if nome, err := avaliarUsuarioWS(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")); err != nil || nome != "" {
		t.Errorf("nome=%q err=%v; esperava vazio sem erro", nome, err)
	}
}
