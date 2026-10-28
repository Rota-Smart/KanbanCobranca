package service

import "errors"

const (
	VersaoRegra = 1
	MaxCents    = 99_999_999_99 // R$ 99.999.999,99
	MaxParcelas = 120

	valorMinimoTotalCents int64 = 1  // total em aberto tem de ser positivo
	minCentavosPorParcela int64 = 1  // cada parcela precisa valer ao menos 1 centavo
	realEmCents           int64 = 100
	passoEntrada50        int64 = 5000 // entrada cai num múltiplo de R$ 50
)

// EntradaMinima devolve a entrada da Política v7: 20% do total na primeira
// renegociação, 40% quando o cliente já tem renegociação anterior — sempre
// arredondada pra cima pro múltiplo de R$ 50 mais próximo, sem passar do total.
// É o valor que vem pré-preenchido no campo de entrada.
func EntradaMinima(totalCents int64, jaRenegociou bool) int64 {
	return Arredondar50Cima(EntradaMinimaExata(totalCents, jaRenegociou))
}

// EntradaMinimaExata devolve o percentual exato da Política v7 (20% ou 40%
// do total, sem arredondamento) — é o que aparece como referência "mín.".
// Em centavos o percentual é sempre inteiro (total*20/100), sem sobra.
func EntradaMinimaExata(totalCents int64, jaRenegociou bool) int64 {
	pct := int64(20)
	if jaRenegociou {
		pct = 40
	}
	e := totalCents * pct / 100
	if e > totalCents {
		e = totalCents
	}
	return e
}

// Arredondar50Cima arredonda pra cima pro múltiplo de R$ 50 mais próximo.
func Arredondar50Cima(cents int64) int64 {
	if cents <= 0 {
		return 0
	}
	return ((cents + passoEntrada50 - 1) / passoEntrada50) * passoEntrada50
}

// ArredondarRealCima arredonda pra cima pro inteiro de real mais próximo.
func ArredondarRealCima(cents int64) int64 {
	if cents <= 0 {
		return 0
	}
	return ((cents + realEmCents - 1) / realEmCents) * realEmCents
}

var (
	ErrTotalInvalido        = errors.New("valor total em aberto deve ser maior que zero")
	ErrEntradaNegativa      = errors.New("entrada não pode ser negativa")
	ErrEntradaMaiorQueTotal = errors.New("entrada não pode ser maior que o total em aberto")
	ErrParcelaBaseNegativa  = errors.New("parcela base não pode ser negativa")
	ErrNumeroVezes          = errors.New("número de vezes deve estar entre 1 e 120")
	ErrValorMuitoAlto       = errors.New("valor acima do limite permitido")
	ErrParcelaZerada        = errors.New("saldo pequeno demais para esse número de vezes")
)

type ReparcelamentoInput struct {
	TotalAbertoCents int64 `json:"totalAbertoCents"`
	EntradaCents     int64 `json:"entradaCents"`
	NumeroVezes      int   `json:"numeroVezes"`
	ParcelaBaseCents int64 `json:"parcelaBaseCents"`
}

type ReparcelamentoResult struct {
	SaldoCents         int64 `json:"saldoCents"`
	ParceladoCents     int64 `json:"parceladoCents"` // parcelas 1..N-1 do saldo
	RestoCents         int64 `json:"restoCents"`
	UltimaParcelaCents int64 `json:"ultimaParcelaCents"` // parcela N do saldo
	FinalCents         int64 `json:"finalCents"`         // base + parcelado
	UltimaFinalCents   int64 `json:"ultimaFinalCents"`   // base + última
}

func CalculateReparcelamento(in ReparcelamentoInput) (ReparcelamentoResult, error) {
	switch {
	case in.TotalAbertoCents < valorMinimoTotalCents:
		return ReparcelamentoResult{}, ErrTotalInvalido
	case in.TotalAbertoCents > MaxCents || in.ParcelaBaseCents > MaxCents:
		return ReparcelamentoResult{}, ErrValorMuitoAlto
	case in.EntradaCents < 0:
		return ReparcelamentoResult{}, ErrEntradaNegativa
	case in.EntradaCents > in.TotalAbertoCents:
		return ReparcelamentoResult{}, ErrEntradaMaiorQueTotal
	case in.ParcelaBaseCents < 0:
		return ReparcelamentoResult{}, ErrParcelaBaseNegativa
	case in.NumeroVezes < 1 || in.NumeroVezes > MaxParcelas:
		return ReparcelamentoResult{}, ErrNumeroVezes
	}

	n := int64(in.NumeroVezes)
	saldo := in.TotalAbertoCents - in.EntradaCents
	if saldo > 0 && saldo < n*minCentavosPorParcela {
		return ReparcelamentoResult{}, ErrParcelaZerada
	}

	// Parcela cai num real inteiro, arredondada pra cima; a última absorve a
	// diferença (fica menor). Sem casas de centavo pra cliente pagar.
	parcelado := ArredondarRealCima((saldo + n - 1) / n)
	if n == 1 {
		parcelado = saldo // 1x quita o saldo exato, sem arredondar
	}
	ultima := saldo - parcelado*(n-1)
	resto := ultima - parcelado
	if ultima < minCentavosPorParcela || resto > 0 {
		// Saldo pequeno demais pra distribuir em reais cheios: volta pra
		// divisão exata em centavos (última leva o resto).
		parcelado = saldo / n
		resto = saldo % n
		ultima = parcelado + resto
	}

	return ReparcelamentoResult{
		SaldoCents:         saldo,
		ParceladoCents:     parcelado,
		RestoCents:         resto,
		UltimaParcelaCents: ultima,
		FinalCents:         in.ParcelaBaseCents + parcelado,
		UltimaFinalCents:   in.ParcelaBaseCents + ultima,
	}, nil
}
