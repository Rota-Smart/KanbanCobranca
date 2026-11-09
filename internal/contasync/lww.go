// Package contasync sincroniza o quadro local com a API do RotaSuite.
// O SQLite continua sendo o banco do dia a dia: a rede só entra em segundo plano.
// Conflito nunca pergunta nada — vale a alteração mais recente (atualizado_em, depois versao).
package contasync

import (
	"errors"
	"time"
)

const (
	// URLPadrao é a API no ar. O app não fala com o portal 360 nesta etapa.
	URLPadrao = "https://site-back-10cd.onrender.com"

	EstadoOk            = "ok"
	EstadoSincronizando = "sincronizando"
	EstadoAguardando    = "aguardando"
	EstadoReconectar    = "reconectar"
)

// ErrPareamentoIndisponivel: o site ainda não oferece o código de uso único.
var ErrPareamentoIndisponivel = errors.New("O site ainda não gera o código de pareamento. Quando a nuvem liberar, digite o código aqui.")

// Alteracao é um registro do lote (envio ou recebimento).
type Alteracao struct {
	Tabela       string         `json:"tabela"`
	ID           string         `json:"id"`
	Apagado      bool           `json:"apagado"`
	AtualizadoEm string         `json:"atualizado_em"`
	Versao       int64          `json:"versao"`
	Dados        map[string]any `json:"dados,omitempty"`
}

// Resultado é o que a nuvem decidiu sobre uma alteração enviada.
type Resultado struct {
	Tabela       string         `json:"tabela"`
	ID           string         `json:"id"`
	Resultado    string         `json:"resultado"` // aplicada, igual, antiga, erro
	AtualizadoEm string         `json:"atualizado_em"`
	Versao       int64          `json:"versao"`
	Apagado      bool           `json:"apagado"`
	Dados        map[string]any `json:"dados,omitempty"`
}

// Pacote é a resposta de GET /sync?desde=.
type Pacote struct {
	Agora      string      `json:"agora"`
	Alteracoes []Alteracao `json:"alteracoes"`
}

// Relatorio resume um ciclo para o indicador da tela.
type Relatorio struct {
	Estado    string
	Aplicou   int
	Pendentes int
}

// Comparar devolve 1 se A é mais novo que B, -1 se B é mais novo, 0 se empatam.
// O horário vem primeiro; se for o mesmo, a versão maior ganha.
func Comparar(emA string, verA int64, emB string, verB int64) int {
	ta := parseCarimbo(emA)
	tb := parseCarimbo(emB)
	if ta.After(tb) {
		return 1
	}
	if tb.After(ta) {
		return -1
	}
	if verA > verB {
		return 1
	}
	if verB > verA {
		return -1
	}
	return 0
}

func parseCarimbo(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

func carimboAgora(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}
