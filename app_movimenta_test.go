package main

import (
	"errors"
	"testing"

	"rota-smart/internal/rota360"
	"rota-smart/internal/service"
)

// TestMovimentarTicket360Validacao: a ação manual (Shift+F6) valida entrada e
// coluna na borda e exige sessão no 360 (sem ela não há de onde puxar o
// cliente). O fluxo completo com o 360 é o mesmo de ImportarCliente360 +
// vínculo, já exercidos nos testes desses caminhos.
func TestMovimentarTicket360Validacao(t *testing.T) {
	a := newTestApp(t)

	if _, err := a.MovimentarTicket360("12345", "Coluna que não existe"); !errors.Is(err, service.ErrColunaInvalida) {
		t.Fatalf("coluna inválida deveria dar ErrColunaInvalida, veio %v", err)
	}
	if _, err := a.MovimentarTicket360("   ", "Cobrança"); err == nil {
		t.Fatalf("entrada vazia deveria dar erro")
	}
	if _, err := a.MovimentarTicket360("12345", "Cobrança"); !errors.Is(err, rota360.ErrSemSessao) {
		t.Fatalf("sem sessão no 360 deveria dar ErrSemSessao, veio %v", err)
	}
}
