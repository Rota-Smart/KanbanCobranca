package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQuatroVinteENoveHonraRetryAfter(t *testing.T) {
	chamadas := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chamadas++
		if chamadas == 1 {
			w.Header().Set("Retry-After", "0.2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		jsonResposta(w, "depois do 429")
	}))
	t.Cleanup(srv.Close)
	g := groqParaTeste(t, srv)

	res, err := g.Completar(context.Background(), "s", "u")
	if err != nil {
		t.Fatalf("deveria ter repetido após o 429: %v", err)
	}
	if res != "depois do 429" {
		t.Errorf("resposta: %q", res)
	}
	if chamadas != 2 {
		t.Errorf("esperava 2 chamadas, houve %d", chamadas)
	}
}

func TestQuatroVinteENoveDesisteSemTempo(t *testing.T) {
	chamadas := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chamadas++
		w.Header().Set("Retry-After", "45") // janela maior que o ctx
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	g := groqParaTeste(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := g.Completar(ctx, "s", "u"); err == nil {
		t.Error("deveria falhar quando a espera não cabe no ctx")
	}
	if chamadas != 1 {
		t.Errorf("esperava 1 chamada (sem repetir), houve %d", chamadas)
	}
}

func TestQuatroCentoUmNaoRepete(t *testing.T) {
	chamadas := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chamadas++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	g := groqParaTeste(t, srv)

	if _, err := g.Completar(context.Background(), "s", "u"); err == nil {
		t.Error("401 deveria falhar direto")
	}
	if chamadas != 1 {
		t.Errorf("erro permanente não deveria repetir; houve %d chamadas", chamadas)
	}
}

func TestEsperaDeRetry(t *testing.T) {
	h := http.Header{}
	resp := &http.Response{Header: h}

	if d := esperaDeRetry(nil, 0); d != 2*time.Second {
		t.Errorf("backoff sem resposta: %v", d)
	}
	if d := esperaDeRetry(nil, 9); d != 12*time.Second {
		t.Errorf("backoff na 10ª tentativa: %v", d)
	}

	h.Set("Retry-After", "0.5")
	if d := esperaDeRetry(resp, 0); d != 500*time.Millisecond {
		t.Errorf("Retry-After fracionário: %v", d)
	}
	h.Set("Retry-After", "120")
	if d := esperaDeRetry(resp, 0); d != 60*time.Second {
		t.Errorf("Retry-After deveria ser capado em 60s: %v", d)
	}
	h.Del("Retry-After")

	h.Set("X-Ratelimit-Reset-Tokens", "1.2s")
	if d := esperaDeRetry(resp, 0); d != 1450*time.Millisecond {
		t.Errorf("reset de TPM + folga: %v", d)
	}
	h.Set("X-Ratelimit-Reset-Tokens", "lixo")
	if d := esperaDeRetry(resp, 1); d != 5*time.Second {
		t.Errorf("header inválido deveria cair no backoff: %v", d)
	}
}

// jsonResposta escreve uma resposta 200 padrão do chat completions.
func jsonResposta(w http.ResponseWriter, conteudo string) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"choices":[{"message":{"content":"` + conteudo + `"}}]}`))
}

func TestProvedorDeepInfra(t *testing.T) {
	var modeloRecebido, authRecebido string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		modeloRecebido = req.Model
		authRecebido = r.Header.Get("Authorization")
		jsonResposta(w, "ok deepinfra")
	}))
	t.Cleanup(srv.Close)

	g := &GroqCliente{
		HTTP:              srv.Client(),
		Endpoint:          srv.URL,
		ChaveInjetavel:    func() string { return "chave-di" },
		ProvedorInjetavel: func() string { return ProvedorDeepInfra },
	}
	res, err := g.Completar(context.Background(), "s", "u")
	if err != nil {
		t.Fatalf("Completar: %v", err)
	}
	if res != "ok deepinfra" {
		t.Errorf("resposta: %q", res)
	}
	if modeloRecebido != DeepInfraModeloPadrao {
		t.Errorf("modelo: esperava %q, veio %q", DeepInfraModeloPadrao, modeloRecebido)
	}
	if authRecebido != "Bearer chave-di" {
		t.Errorf("authorization: %q", authRecebido)
	}
	if g.endpoint() != srv.URL {
		t.Errorf("Endpoint de teste deveria vencer o do provedor")
	}

	// Sem modelo configurado e sem provedor: volta para a Groq.
	g2 := &GroqCliente{ChaveInjetavel: func() string { return "k" }}
	if g2.modelo() != GroqModeloPadrao {
		t.Errorf("default da Groq: %q", g2.modelo())
	}
	if g2.endpoint() != GroqEndpoint {
		t.Errorf("endpoint da Groq: %q", g2.endpoint())
	}
}
