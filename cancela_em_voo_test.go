package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rota-smart/internal/service"
)

// TestEnviarChatCancelavel: o ⏹ do chat (CancelarChat) aborta um EnviarChat
// em voo — o pedido não pode continuar até o timeout de 90s, e o erro que
// volta precisa ser o de cancelamento (o frontend mostra "⏹ …" para ele).
func TestEnviarChatCancelavel(t *testing.T) {
	a := newTestApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // só responde quando o pedido for cancelado
		w.WriteHeader(http.StatusRequestTimeout)
	}))
	// srv.Close espera os handlers: teto de 10s para o teste não pendurar.
	go func() {
		time.Sleep(10 * time.Second)
		srv.Close()
	}()
	a.groq = &service.GroqCliente{
		HTTP:              srv.Client(),
		ChaveInjetavel:    func() string { return "chave-teste" },
		ModeloInjetavel:   func() string { return "modelo-teste" },
		ProvedorInjetavel: func() string { return "groq" },
		Endpoint:          srv.URL,
	}
	c, err := a.CriarChat()
	if err != nil {
		t.Fatalf("CriarChat: %v", err)
	}
	errCh := make(chan error, 1)
	go func() {
		_, e := a.EnviarChat(c.ID, "ola assistente tudo bem")
		errCh <- e
	}()
	time.Sleep(300 * time.Millisecond)
	a.CancelarChat(c.ID)
	select {
	case e := <-errCh:
		t.Logf("erro devolvido pelo EnviarChat após cancelar: %v", e)
		if e == nil || !strings.Contains(e.Error(), "cancelado") {
			t.Fatalf("EnviarChat deveria abortar com o erro de cancelamento; veio: %v", e)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("CancelarChat NÃO abortou o EnviarChat em voo (seguiu até o timeout)")
	}
}
