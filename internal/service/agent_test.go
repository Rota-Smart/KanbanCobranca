package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// executorDeTeste registra as ferramentas chamadas.
type executorDeTeste struct {
	chamadas []string
}

func (e *executorDeTeste) ExecutarAgente(ctx context.Context, ferramenta, argsJSON string) (string, string, error) {
	e.chamadas = append(e.chamadas, ferramenta+"("+argsJSON+")")
	return "ok", ferramenta + " executada", nil
}

// servidorGroq devolve as respostas em ordem: tool_call(s) e depois texto.
func servidorGroq(t *testing.T, respostas []map[string]any) *httptest.Server {
	t.Helper()
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []json.RawMessage `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		resp := respostas[min(i, len(respostas)-1)]
		i++
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": resp}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func groqParaTeste(t *testing.T, srv *httptest.Server) *GroqCliente {
	t.Helper()
	return &GroqCliente{
		HTTP:            srv.Client(),
		ChaveInjetavel:  func() string { return "gsk_teste" },
		ModeloInjetavel: func() string { return GroqModeloPadrao },
		Endpoint:        srv.URL,
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestConversarExecutaFerramenta(t *testing.T) {
	toolCall := map[string]any{
		"tool_calls": []map[string]any{{
			"id":   "call_1",
			"type": "function",
			"function": map[string]any{
				"name":      "criar_coluna",
				"arguments": `{"nome":"Renegociação"}`,
			},
		}},
	}
	srv := servidorGroq(t, []map[string]any{toolCall, {"content": "Coluna criada!"}})
	// aponta o cliente para o servidor falso
	g := groqParaTeste(t, srv)
	exec := &executorDeTeste{}

	res, err := g.Conversar(context.Background(), nil, "crie a coluna Renegociação", exec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Resposta != "Coluna criada!" {
		t.Errorf("resposta: %q", res.Resposta)
	}
	if len(res.Acoes) != 1 || res.Acoes[0].Ferramenta != "criar_coluna" {
		t.Errorf("ações: %+v", res.Acoes)
	}
	if len(exec.chamadas) != 1 || !strings.Contains(exec.chamadas[0], "Renegociação") {
		t.Errorf("chamadas: %v", exec.chamadas)
	}
}

func TestConversarLimiteDeRodadas(t *testing.T) {
	toolCall := map[string]any{
		"tool_calls": []map[string]any{{
			"id":       "call_x",
			"type":     "function",
			"function": map[string]any{"name": "listar_colunas", "arguments": "{}"},
		}},
	}
	// 6 rodadas pedindo ferramenta + a rodada final sem ferramentas.
	respostas := make([]map[string]any, 0, MaxIteracoesAgente+1)
	for i := 0; i < MaxIteracoesAgente; i++ {
		respostas = append(respostas, toolCall)
	}
	respostas = append(respostas, map[string]any{"content": "resumo final"})
	srv := servidorGroq(t, respostas)
	g := groqParaTeste(t, srv)
	exec := &executorDeTeste{}

	res, err := g.Conversar(context.Background(), nil, "oi", exec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(exec.chamadas) != MaxIteracoesAgente {
		t.Errorf("esperava %d execuções, veio %d", MaxIteracoesAgente, len(exec.chamadas))
	}
	if res.Resposta != "resumo final" {
		t.Errorf("resposta final: %q", res.Resposta)
	}
}

func TestAgentToolsSchema(t *testing.T) {
	tools := AgentTools()
	if len(tools) != 12 {
		t.Fatalf("esperava 12 ferramentas, veio %d", len(tools))
	}
	nomes := map[string]bool{}
	for _, tl := range tools {
		fn := tl["function"].(map[string]any)
		nomes[fn["name"].(string)] = true
	}
	for _, esperada := range []string{"buscar_cliente_360", "importar_cliente", "criar_coluna", "mover_card", "gerar_proposta"} {
		if !nomes[esperada] {
			t.Errorf("ferramenta ausente: %s (%v)", esperada, nomes)
		}
	}
}
