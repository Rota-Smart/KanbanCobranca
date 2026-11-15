package main

// ContratosResumo: leitura leve do PRIMEIRO contrato de cada cliente do 360,
// usada pela ordenação por fidelidade no dashboard de Fidelidade. O frontend
// chama isto em loop (2-3 em paralelo) porque é 1 request por cliente.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"rota-smart/internal/rota360"
)

// ContratoResumo é o resumo de fidelidade de um cliente: primeira assinatura,
// tempo de contrato e contrato atual (nome/prazo/valor).
type ContratoResumo struct {
	CodCliente         int64   `json:"CodCliente"`
	PrimeiraAssinatura string  `json:"PrimeiraAssinatura"` // "02/01/2006" ou "" se sem contrato
	MesesDeContrato    int     `json:"MesesDeContrato"`    // meses desde a primeira assinatura
	ContratoAtual      string  `json:"ContratoAtual"`      // nome do contrato vigente
	ContratoPrazo      int     `json:"ContratoPrazo"`      // prazo em meses
	ContratoValor      float64 `json:"ContratoValor"`      // mensalidade (ValorME)
	Termino            string  `json:"Termino"`            // término do contrato vigente
}

// resumoContratos calcula o resumo a partir da lista de contratos: primeira
// assinatura de todos os contratos (a mais antiga vence) e contrato vigente
// para nome/prazo/valor. agoraf permite teste determinístico.
func resumoContratos(cod int64, contratos []rota360.Contrato360, agora time.Time) ContratoResumo {
	res := ContratoResumo{CodCliente: cod}
	var primeira time.Time
	for _, ct := range contratos {
		if ct.DataAssinatura.IsZero() {
			continue
		}
		if primeira.IsZero() || ct.DataAssinatura.Before(primeira) {
			primeira = ct.DataAssinatura
		}
	}
	if primeira.IsZero() {
		return res
	}
	res.PrimeiraAssinatura = primeira.Format("02/01/2006")
	res.MesesDeContrato = mesesDesde(primeira, agora)
	if ct := contratoVigente(contratos); ct != nil {
		res.ContratoAtual = ct.Nome
		res.ContratoPrazo = ct.Prazo
		res.ContratoValor = ct.ValorME
		if !ct.TerminoContrato.IsZero() {
			res.Termino = ct.TerminoContrato.Format("02/01/2006")
		}
	}
	return res
}

// mesesDesde devolve meses inteiros entre inicio e agora (calendário: mesmo
// dia do mês não arredonda para cima).
func mesesDesde(inicio, agora time.Time) int {
	if inicio.IsZero() || agora.Before(inicio) {
		return 0
	}
	meses := (agora.Year()-inicio.Year())*12 + int(agora.Month()-inicio.Month())
	if agora.Day() < inicio.Day() {
		meses--
	}
	if meses < 0 {
		meses = 0
	}
	return meses
}

// ContratosResumo lê os contratos do cliente no 360 e devolve o resumo de
// fidelidade (primeira assinatura, meses de contrato, contrato atual).
func (a *App) ContratosResumo(codService string) (ContratoResumo, error) {
	codService = strings.TrimSpace(codService)
	cod, err := strconv.ParseInt(codService, 10, 64)
	if err != nil {
		return ContratoResumo{}, fmt.Errorf("código service inválido: %s", codService)
	}
	c := a.rotaAtual()
	if c == nil {
		return ContratoResumo{}, rota360.ErrSemSessao
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	contratos, err := c.ContratosCliente(ctx, codService)
	if err != nil {
		return ContratoResumo{}, friendly("ContratosResumo", err)
	}
	return resumoContratos(cod, contratos, time.Now()), nil
}
