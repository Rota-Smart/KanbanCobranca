package service

import (
	"errors"
	"math/rand"
	"testing"
)

func TestCalculateReparcelamento(t *testing.T) {
	cases := []struct {
		name    string
		in      ReparcelamentoInput
		want    ReparcelamentoResult
		wantErr error
	}{
		{"Exemplo Taciele", ReparcelamentoInput{58400, 14000, 12, 10400},
			ReparcelamentoResult{SaldoCents: 44400, ParceladoCents: 3700, RestoCents: 0, UltimaParcelaCents: 3700, FinalCents: 14100, UltimaFinalCents: 14100}, nil},
		{"Divisão exata", ReparcelamentoInput{12000, 0, 4, 0},
			ReparcelamentoResult{SaldoCents: 12000, ParceladoCents: 3000, RestoCents: 0, UltimaParcelaCents: 3000, FinalCents: 3000, UltimaFinalCents: 3000}, nil},
		{"Resto de 1 centavo", ReparcelamentoInput{10000, 0, 3, 0},
			ReparcelamentoResult{SaldoCents: 10000, ParceladoCents: 3333, RestoCents: 1, UltimaParcelaCents: 3334, FinalCents: 3333, UltimaFinalCents: 3334}, nil},
		{"Resto de 4 centavos", ReparcelamentoInput{10000, 0, 7, 0},
			ReparcelamentoResult{SaldoCents: 10000, ParceladoCents: 1428, RestoCents: 4, UltimaParcelaCents: 1432, FinalCents: 1428, UltimaFinalCents: 1432}, nil},
		{"Uma parcela só", ReparcelamentoInput{10000, 2000, 1, 5000},
			ReparcelamentoResult{SaldoCents: 8000, ParceladoCents: 8000, RestoCents: 0, UltimaParcelaCents: 8000, FinalCents: 13000, UltimaFinalCents: 13000}, nil},
		{"Entrada = total", ReparcelamentoInput{10000, 10000, 1, 5000},
			ReparcelamentoResult{SaldoCents: 0, ParceladoCents: 0, RestoCents: 0, UltimaParcelaCents: 0, FinalCents: 5000, UltimaFinalCents: 5000}, nil},

		{"Entrada maior que total", ReparcelamentoInput{10000, 20000, 2, 0}, ReparcelamentoResult{}, ErrEntradaMaiorQueTotal},
		{"Entrada negativa", ReparcelamentoInput{10000, -1, 2, 0}, ReparcelamentoResult{}, ErrEntradaNegativa},
		{"Total zero", ReparcelamentoInput{0, 0, 2, 0}, ReparcelamentoResult{}, ErrTotalInvalido},
		{"Total negativo", ReparcelamentoInput{-100, 0, 2, 0}, ReparcelamentoResult{}, ErrTotalInvalido},
		{"Vezes zero", ReparcelamentoInput{10000, 0, 0, 0}, ReparcelamentoResult{}, ErrNumeroVezes},
		{"Vezes acima de 120", ReparcelamentoInput{10000, 0, 121, 0}, ReparcelamentoResult{}, ErrNumeroVezes},
		{"Parcela base negativa", ReparcelamentoInput{10000, 0, 2, -1}, ReparcelamentoResult{}, ErrParcelaBaseNegativa},
		{"Total acima de MaxCents", ReparcelamentoInput{MaxCents + 1, 0, 2, 0}, ReparcelamentoResult{}, ErrValorMuitoAlto},
		{"Parcela base acima de MaxCents", ReparcelamentoInput{10000, 0, 2, MaxCents + 1}, ReparcelamentoResult{}, ErrValorMuitoAlto},
		{"Saldo de 2 centavos em 5 vezes", ReparcelamentoInput{2, 0, 5, 0}, ReparcelamentoResult{}, ErrParcelaZerada},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CalculateReparcelamento(tc.in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("erro = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("resultado = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestInvarianteUltimaParcela garante que nenhum centavo se perde:
// Parcelado × (N-1) + Última == Saldo, sempre.
func TestInvarianteUltimaParcela(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 10_000; i++ {
		total := rng.Int63n(MaxCents) + 1
		entrada := rng.Int63n(total + 1)
		vezes := rng.Intn(MaxParcelas) + 1
		base := rng.Int63n(MaxCents)

		res, err := CalculateReparcelamento(ReparcelamentoInput{
			TotalAbertoCents: total, EntradaCents: entrada, NumeroVezes: vezes, ParcelaBaseCents: base,
		})
		if err != nil {
			t.Fatalf("entrada válida rejeitada: total=%d entrada=%d vezes=%d base=%d: %v", total, entrada, vezes, base, err)
		}
		soma := res.ParceladoCents*int64(vezes-1) + res.UltimaParcelaCents
		if soma != res.SaldoCents {
			t.Fatalf("invariante violada: total=%d entrada=%d vezes=%d: soma=%d saldo=%d",
				total, entrada, vezes, soma, res.SaldoCents)
		}
	}
}
