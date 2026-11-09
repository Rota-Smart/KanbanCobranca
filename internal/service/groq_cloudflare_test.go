package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func clienteAutoCloudflare(srvGroq, srvCF *httptest.Server) *GroqCliente {
	return &GroqCliente{
		HTTP:                      &http.Client{},
		Endpoint:                  srvGroq.URL,
		EndpointCloudflare:        srvCF.URL,
		ChaveInjetavel:            func() string { return "gsk" },
		ProvedorInjetavel:         func() string { return ProvedorAuto },
		CloudflareChaveInjetavel:  func() string { return "cfut" },
		CloudflareContaInjetavel:  func() string { return "conta" },
		DeepInfraChaveInjetavel:   func() string { return "di-nao-usar" },
		EndpointFallback:          "http://127.0.0.1:1",
	}
}

func TestAutoGroqLentaVaiProCloudflareEm5s(t *testing.T) {
	srvGroq := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(20 * time.Second):
		}
	}))
	t.Cleanup(srvGroq.Close)
	var modeloCF string
	srvCF := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modeloCF = r.Header.Get("Authorization")
		jsonResposta(w, "da cloudflare")
	}))
	t.Cleanup(srvCF.Close)
	g := clienteAutoCloudflare(srvGroq, srvCF)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	inicio := time.Now()
	res, err := g.Completar(ctx, "s", "u")
	if err != nil {
		t.Fatalf("deveria cair na Cloudflare: %v", err)
	}
	if res != "da cloudflare" || modeloCF != "Bearer cfut" {
		t.Fatalf("resposta %q auth %q", res, modeloCF)
	}
	if d := time.Since(inicio); d > 8*time.Second {
		t.Fatalf("failover demorou %v (esperado ~5s)", d)
	}
}

func TestAutoGroq429VaiProCloudflare(t *testing.T) {
	srvGroq := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srvGroq.Close)
	srvCF := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResposta(w, "da cloudflare")
	}))
	t.Cleanup(srvCF.Close)
	g := clienteAutoCloudflare(srvGroq, srvCF)

	inicio := time.Now()
	res, err := g.Completar(context.Background(), "s", "u")
	if err != nil || res != "da cloudflare" {
		t.Fatalf("res %q err %v", res, err)
	}
	if d := time.Since(inicio); d > 2*time.Second {
		t.Fatalf("429 com janela longa deveria ir na hora; levou %v", d)
	}
	// Cooldown: a próxima já vai direto pra Cloudflare.
	if g.alvoAtual().provedor != ProvedorCloudflare {
		t.Fatalf("cooldown não armou")
	}
}
