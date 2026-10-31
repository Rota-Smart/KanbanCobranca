package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

	res, err := g.Conversar(context.Background(), nil, "crie a coluna Renegociação", "", exec, nil)
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

	res, err := g.Conversar(context.Background(), nil, "oi", "", exec, nil)
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
	if len(tools) != 32 {
		t.Fatalf("esperava 32 ferramentas, veio %d", len(tools))
	}
	nomes := map[string]bool{}
	for _, tl := range tools {
		fn := tl["function"].(map[string]any)
		nomes[fn["name"].(string)] = true
	}
	for _, esperada := range []string{"buscar_cliente_360", "importar_cliente", "criar_coluna", "mover_card", "gerar_proposta", "vincular_ticket", "financeiro_cliente", "equipamentos_cliente", "atendimentos_cliente"} {
		if !nomes[esperada] {
			t.Errorf("ferramenta ausente: %s (%v)", esperada, nomes)
		}
	}
}

// TestAgentToolsEnxutas trava a economia de TPM: os schemas são reenviados
// em toda rodada do loop, então descrição comprida aqui custa caro. Era 5506
// bytes antes do enxugamento (e subiu ~430 com as 2 tools novas de dados
// fatiados); o boilerplate dos schemas (type/properties/required) é o piso.
func TestAgentToolsEnxutas(t *testing.T) {
	b, err := json.Marshal(AgentTools())
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > 6260 {
		t.Errorf("tools com %d bytes (teto 6260) — enxugue as descrições", len(b))
	}
}

func TestMensagemSemFerramentas(t *testing.T) {
	casos := []struct {
		msg  string
		want bool
	}{
		{"crie a coluna Renegociação", false},
		{"melhore o texto do ticket 67220 e envie", false},             // cita ticket → pode precisar ler
		{"busque o cliente Padaria Central", false},                    // ação
		{"qual o saldo do cliente 1234?", false},                       // ação
		{"o que você faz?", true},                                      // pergunta sobre o app
		{"como funciona o orçamento?", false},                          // fala de orçamento → deixa o agente decidir
		{"como você funciona?", true},                                  // meta, sem palavra de ação
		{"melhore este texto: " + strings.Repeat("texto longo aqui. ", 40), true}, // reescrita de texto colado
		{"melhore este texto", false},                                  // curto: provavelmente pede para ler algo antes
	}
	for _, c := range casos {
		if got := MensagemSemFerramentas(c.msg); got != c.want {
			t.Errorf("MensagemSemFerramentas(%q) = %v, queria %v", c.msg, got, c.want)
		}
	}
}

func TestConversarFastPathSemFerramentas(t *testing.T) {
	var teveTools bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools []map[string]any `json:"tools"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		teveTools = len(req.Tools) > 0
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "texto melhorado"}}},
			"usage":   map[string]any{"prompt_tokens": 500, "completion_tokens": 100},
		})
	}))
	t.Cleanup(srv.Close)
	g := groqParaTeste(t, srv)
	exec := &executorDeTeste{}

	texto := "reescreva com tom cordial: " + strings.Repeat("prezado cliente, ", 30)
	res, err := g.Conversar(context.Background(), nil, texto, "", exec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if teveTools {
		t.Error("fast-path não deveria enviar tools")
	}
	if len(exec.chamadas) != 0 {
		t.Errorf("fast-path executou ferramentas: %v", exec.chamadas)
	}
	if res.Resposta != "texto melhorado" {
		t.Errorf("resposta: %q", res.Resposta)
	}
}

func TestHistoricoCompacto(t *testing.T) {
	historico := make([]MensagemDeChat, 0, 10)
	for i := 0; i < 8; i++ {
		historico = append(historico,
			MensagemDeChat{Papel: "user", Conteudo: strings.Repeat("u", 1000)},
			MensagemDeChat{Papel: "assistant", Conteudo: strings.Repeat("a", 1000)})
	}
	out := historicoCompacto(historico)
	if len(out) != MaxMensagensHistorico {
		t.Fatalf("esperava %d mensagens, veio %d", MaxMensagensHistorico, len(out))
	}
	for _, m := range out {
		if runes := len([]rune(m.Conteudo)); runes > MaxRunasMensagem+1 { // +1 pelo "…"
			t.Errorf("mensagem com %d runes (limite %d)", runes, MaxRunasMensagem)
		}
	}
}

func TestOnUsageRecebido(t *testing.T) {
	srv := servidorGroq(t, []map[string]any{{"content": "ok"}})
	// sobrescreve para incluir usage
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "ok"}}},
			"usage":   map[string]any{"prompt_tokens": 120, "completion_tokens": 30},
		})
	})
	g := groqParaTeste(t, srv)
	var prompt, completion int
	g.OnUsage = func(p, c int) { prompt, completion = p, c }
	if _, err := g.Completar(context.Background(), "s", "u"); err != nil {
		t.Fatal(err)
	}
	if prompt != 120 || completion != 30 {
		t.Errorf("usage: prompt=%d completion=%d", prompt, completion)
	}
}

// captura os corpos das requisições (para inspecionar as mensagens enviadas).
type capturaDeCorpos struct {
	mu     sync.Mutex
	corpos []map[string]any
}

func (c *capturaDeCorpos) servidor(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var corpo map[string]any
		json.NewDecoder(r.Body).Decode(&corpo)
		c.mu.Lock()
		c.corpos = append(c.corpos, corpo)
		c.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "ok"}}},
			"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (c *capturaDeCorpos) mensagens() []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.corpos) == 0 {
		return nil
	}
	return c.corpos[0]["messages"].([]any)
}

// (b): o bloco de contexto vai como mensagem system PRÓPRIA — o system
// prompt principal fica byte-idêntico entre mensagens (cache de prefixo).
func TestBlocoContextoMensagemSeparada(t *testing.T) {
	cap := &capturaDeCorpos{}
	g := groqParaTeste(t, cap.servidor(t))
	ops := &OpcoesConversa{BlocoContexto: "Contexto pré-coletado: financeiro do cliente X"}
	if _, err := g.ConversarCom(context.Background(), nil, "e o resumo?", "", &executorDeTeste{}, nil, ops); err != nil {
		t.Fatal(err)
	}
	msgs := cap.mensagens()
	if len(msgs) < 2 {
		t.Fatalf("mensagens: %d", len(msgs))
	}
	primeiro := msgs[0].(map[string]any)
	if primeiro["role"] != "system" || strings.Contains(primeiro["content"].(string), "pré-coletado") {
		t.Errorf("system prompt principal contém o bloco: %v", primeiro)
	}
	if msgs[1].(map[string]any)["content"] != ops.BlocoContexto {
		t.Errorf("bloco não é a 2ª mensagem: %v", msgs[1])
	}
}

// (c): saldo de TPM baixo → histórico cai para o modo econômico (2 msgs).
func TestModoEconomicoHistorico(t *testing.T) {
	cap := &capturaDeCorpos{}
	g := groqParaTeste(t, cap.servidor(t))
	g.saldoTPM.Store(1500) // janela quase no fim
	var hist []MensagemDeChat
	for i := 0; i < 6; i++ {
		hist = append(hist, MensagemDeChat{Papel: "user", Conteudo: fmt.Sprintf("msg %d", i)})
	}
	if _, err := g.ConversarCom(context.Background(), hist, "nova mensagem", "", &executorDeTeste{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	// system + 2 do histórico + user = 4
	if got := len(cap.mensagens()); got != 4 {
		t.Errorf("modo econômico: %d mensagens no request; quero 4", got)
	}
	// saldo desconhecido (-1): histórico cheio (system + 6 + user = 8)
	g.saldoTPM.Store(-1)
	if _, err := g.ConversarCom(context.Background(), hist, "outra", "", &executorDeTeste{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	cap.mu.Lock()
	ultimo := cap.corpos[len(cap.corpos)-1]["messages"].([]any)
	cap.mu.Unlock()
	if got := len(ultimo); got != 8 {
		t.Errorf("saldo desconhecido: %d mensagens; quero 8", got)
	}
}

// (c): o header x-ratelimit-remaining-tokens alimenta o saldo.
func TestRegistrarSaldoTPM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Ratelimit-Remaining-Tokens", "5664")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "oi"}}},
		})
	}))
	defer srv.Close()
	g := groqParaTeste(t, srv)
	if _, err := g.Completar(context.Background(), "s", "u"); err != nil {
		t.Fatal(err)
	}
	if got := g.SaldoTPM(); got != 5664 {
		t.Errorf("SaldoTPM = %d; quero 5664", got)
	}
}
