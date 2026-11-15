package main

import (
	"testing"
	"time"

	"rota-smart/internal/rota360"
)

func data(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// TestResumoContratosPrimeira: a primeira assinatura é a mais antiga entre
// vários contratos, e o contrato atual vem do contratoVigente.
func TestResumoContratosPrimeira(t *testing.T) {
	agora := data("2026-11-14")
	contratos := []rota360.Contrato360{
		{Nome: "Contrato B", DataAssinatura: data("2024-06-01"), Prazo: 24, Situacao: "ativo", ValorME: 100},
		{Nome: "Contrato A", DataAssinatura: data("2022-03-15"), Prazo: 36, Situacao: "ativo", ValorME: 90},
		{Nome: "Contrato C", DataAssinatura: data("2025-01-10"), Prazo: 12, Situacao: "cancelado", ValorME: 0},
	}
	res := resumoContratos(77, contratos, agora)
	if res.PrimeiraAssinatura != "15/03/2022" {
		t.Fatalf("primeira assinatura: %q", res.PrimeiraAssinatura)
	}
	// 2022-03-15 → 2026-11-14: 4 anos (48) + 8 meses = 56, menos 1 porque o
	// dia 15 do mês ainda não chegou (hoje é dia 14) = 55 meses.
	if res.MesesDeContrato != 55 {
		t.Fatalf("meses de contrato: %d", res.MesesDeContrato)
	}
	// O contrato atual usa contratoVigente (mesma heurística do financeiro):
	// primeiro não-cancelado com valor na lista — aqui, "Contrato B".
	if res.ContratoAtual != "Contrato B" || res.ContratoPrazo != 24 || res.ContratoValor != 100 {
		t.Fatalf("contrato atual: %+v", res)
	}
	if res.Termino != "" {
		t.Fatalf("termino devia vazio (sem término no contrato): %q", res.Termino)
	}
}

// TestResumoContratosVazio: sem contratos (ou só sem assinatura), resumo vazio.
func TestResumoContratosVazio(t *testing.T) {
	agora := data("2026-11-14")
	res := resumoContratos(1, nil, agora)
	if res.PrimeiraAssinatura != "" || res.MesesDeContrato != 0 || res.ContratoAtual != "" {
		t.Fatalf("resumo de lista vazia: %+v", res)
	}
	res = resumoContratos(1, []rota360.Contrato360{{Nome: "X", DataAssinatura: time.Time{}}}, agora)
	if res.PrimeiraAssinatura != "" {
		t.Fatalf("resumo com assinatura zero: %+v", res)
	}
}

// TestResumoContratosErro: contrato com término preenchido aparece no resumo.
func TestResumoContratosErro(t *testing.T) {
	ct := rota360.Contrato360{
		Nome:            "Contrato A",
		DataAssinatura:  data("2025-06-10"),
		TerminoContrato: data("2027-06-10"),
		Prazo:           24,
		Situacao:        "ativo",
		ValorME:         55.5,
	}
	res := resumoContratos(9, []rota360.Contrato360{ct}, data("2026-11-14"))
	if res.Termino != "10/06/2027" {
		t.Fatalf("termino: %q", res.Termino)
	}
	if res.ContratoValor != 55.5 {
		t.Fatalf("valor: %v", res.ContratoValor)
	}
}

// TestMesesDesde: mês incompleto não conta; futuro não fica negativo.
func TestMesesDesde(t *testing.T) {
	casos := []struct {
		inicio, agora string
		esperado      int
	}{
		{"2026-01-15", "2026-02-14", 0}, // ainda não completou o mês
		{"2026-01-15", "2026-02-15", 1},
		{"2026-10-20", "2026-11-14", 0},
		{"2025-11-14", "2026-11-14", 12},
		{"2026-12-01", "2026-11-14", 0}, // futuro
	}
	for _, c := range casos {
		got := mesesDesde(data(c.inicio), data(c.agora))
		if got != c.esperado {
			t.Errorf("mesesDesde(%s, %s) = %d; quer %d", c.inicio, c.agora, got, c.esperado)
		}
	}
}
