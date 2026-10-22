package service

import "errors"

const (
	VersaoRegra = 1
	MaxCents    = 99_999_999_99 // R$ 99.999.999,99
	MaxParcelas = 120

	valorMinimoTotalCents int64 = 1 // total em aberto tem de ser positivo
	minCentavosPorParcela int64 = 1 // cada parcela precisa valer ao menos 1 centavo
)

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

	parcelado := saldo / n
	resto := saldo % n
	ultima := parcelado + resto

	return ReparcelamentoResult{
		SaldoCents:         saldo,
		ParceladoCents:     parcelado,
		RestoCents:         resto,
		UltimaParcelaCents: ultima,
		FinalCents:         in.ParcelaBaseCents + parcelado,
		UltimaFinalCents:   in.ParcelaBaseCents + ultima,
	}, nil
}
