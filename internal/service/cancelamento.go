package service

import (
	"errors"
	"strings"
)

// Regras do cancelamento obrigatório: todo card com ticket de assunto
// "Cancelamento" precisa de motivo + resultado antes de entrar em
// "Ticket Fechado".

// Resultados válidos do cancelamento.
const (
	CancelamentoReversao = "reversao" // cliente reverteu, ficou
	CancelamentoPerda    = "perda"    // cancelamento confirmado, cliente saiu
)

// ErrCancelamentoSemMotivo bloqueia o fechamento: a mensagem carrega o marcador
// CANCELAMENTO_SEM_MOTIVO para o frontend abrir o diálogo de motivo.
var ErrCancelamentoSemMotivo = errors.New(
	"CANCELAMENTO_SEM_MOTIVO: este card tem ticket de cancelamento — escolha o motivo (reversão ou perda) antes de fechar")

// CancelamentoResultadoValido confere o resultado marcado no fechamento.
func CancelamentoResultadoValido(r string) bool {
	return r == CancelamentoReversao || r == CancelamentoPerda
}

// AssuntoCancelamento diz se o assunto do vínculo é de cancelamento — sem
// olhar caixa/acento e tolerando texto em volta ("cancelamento de internet").
func AssuntoCancelamento(assunto string) bool {
	return strings.Contains(normalizar(assunto), "cancelamento")
}
