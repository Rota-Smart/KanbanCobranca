package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"rota-smart/internal/db"
)

// Contador diário de uso da Groq: cada resposta bem-sucedida soma aqui, para
// o usuário acompanhar os limites do plano free (1.000 req/dia, 200k
// tokens/dia) direto nas Configurações. Persiste na tabela config, numa
// chave por dia — histórico antigo não precisa de tabela própria.

const prefixoChaveUsoGroq = "groq_uso_"

// UsoGroq é o consumo acumulado de um dia.
type UsoGroq struct {
	Data             string `json:"data"`
	Requisicoes      int    `json:"requisicoes"`
	PromptTokens     int    `json:"promptTokens"`
	CompletionTokens int    `json:"completionTokens"`
}

type usoGroqJSON struct {
	Req  int `json:"req"`
	Prom int `json:"prom"`
	Comp int `json:"comp"`
}

// registrarUsoGroq soma a resposta ao total do dia. Falha silenciosa: contador
// é acessório e não deve atrapalhar o chat.
func registrarUsoGroq(ctx context.Context, config *db.ConfigRepo, prompt, completion int) {
	if config == nil {
		return
	}
	hoje := time.Now().Format("2006-01-02")
	chave := prefixoChaveUsoGroq + hoje
	uso := usoGroqJSON{}
	if v, err := config.Get(ctx, chave); err == nil && v != "" {
		json.Unmarshal([]byte(v), &uso)
	}
	uso.Req++
	uso.Prom += prompt
	uso.Comp += completion
	b, err := json.Marshal(uso)
	if err != nil {
		return
	}
	config.Set(ctx, chave, string(b))
}

// UsoGroqHoje devolve o consumo de hoje (para exibir nas Configurações).
func (a *App) UsoGroqHoje() (UsoGroq, error) {
	uso := UsoGroq{Data: time.Now().Format("2006-01-02")}
	if a.config == nil {
		return uso, nil
	}
	v, err := a.config.Get(a.ctx, prefixoChaveUsoGroq+uso.Data)
	if err != nil || v == "" {
		return uso, nil
	}
	var j usoGroqJSON
	if err := json.Unmarshal([]byte(v), &j); err != nil {
		return uso, fmt.Errorf("contador de uso corrompido")
	}
	uso.Requisicoes = j.Req
	uso.PromptTokens = j.Prom
	uso.CompletionTokens = j.Comp
	return uso, nil
}
