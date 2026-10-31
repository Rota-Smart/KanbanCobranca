package main

import (
	"errors"
	"testing"

	"rota-smart/internal/rota360"
	"rota-smart/internal/service"
)

// TestColunaCaseInsensivel: a coluna digitada com caixa/acento diferente do
// board não é rejeitada — "aguardando assinatura" casa com "Aguardando
// Assinatura" e o nome gravado é o canônico do board (v1.38.0: o agente
// recusava mover/importar por diferença de caixa).
func TestColunaCaseInsensivel(t *testing.T) {
	a := newTestApp(t)

	if _, err := a.colunas.Add(a.ctx, "Aguardando Assinatura"); err != nil {
		t.Fatalf("criar coluna: %v", err)
	}

	if !a.colunaValida("aguardando assinatura") {
		t.Fatalf("colunaValida deveria aceitar variação de caixa")
	}
	if !a.colunaValida("aguardando  assinatura ") {
		t.Fatalf("colunaValida deveria aceitar espaços extras")
	}
	canon, ok := a.colunaCanonica("AGUARDANDO ASSINATURA")
	if !ok || canon != "Aguardando Assinatura" {
		t.Fatalf("colunaCanonica deveria devolver o nome do board, veio %q (ok=%v)", canon, ok)
	}
	if a.colunaValida("coluna inexistente") {
		t.Fatalf("coluna inexistente não deveria passar")
	}

	// Fluxo do agente (importar cliente do 360 na coluna): passou da validação
	// de coluna com caixa errada — falha só por falta de sessão no 360.
	if _, err := a.ImportarCliente360("24755", "aguardando assinatura"); !errors.Is(err, rota360.ErrSemSessao) {
		t.Fatalf("coluna com caixa diferente deveria ser aceita (ErrSemSessao), veio %v", err)
	}
	if _, err := a.ImportarCliente360("24755", "coluna que não existe"); !errors.Is(err, service.ErrColunaInvalida) {
		t.Fatalf("coluna inválida deveria continuar dando ErrColunaInvalida, veio %v", err)
	}
}
