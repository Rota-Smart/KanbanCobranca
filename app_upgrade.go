package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"rota-smart/internal/db"
)

// Coluna Upgrade: comissionamento. O card que entra nela registra o valor do
// serviço (5% de comissão) e o valor da parcela do upgrade (75%), com
// observações e data de registro; o botão "Recebido" manda a comissão para o
// histórico. O assistente faz tudo pelo chat (registrar_comissao e afins).

// ColunaUpgrade é o destino dos cards de upgrade — última coluna do board.
const ColunaUpgrade = "Upgrade"

// SalvarComissao grava (ou atualiza) o comissionamento do card e garante que
// ele esteja na coluna Upgrade. Os valores vêm em centavos.
func (a *App) SalvarComissao(ticketID int64, servicoCents, parcelaCents int64, observacoes, dataRegistro string) (db.Comissao, error) {
	if ticketID <= 0 {
		return db.Comissao{}, fmt.Errorf("ticket inválido")
	}
	if servicoCents < 0 || parcelaCents < 0 {
		return db.Comissao{}, fmt.Errorf("valores não podem ser negativos")
	}
	if dataRegistro != "" {
		if _, err := time.Parse("2006-01-02", dataRegistro); err != nil {
			return db.Comissao{}, fmt.Errorf("data de registro inválida (use AAAA-MM-DD)")
		}
	}
	c, err := a.comissoes.Salvar(a.ctx, ticketID, servicoCents, parcelaCents, observacoes, dataRegistro)
	if err != nil {
		return db.Comissao{}, friendly("SalvarComissao", err)
	}
	// Card fora da coluna certa: move (o registro já cria o destino no 360
	// do fluxo — aqui basta o board local).
	if t, err := a.tickets.Get(a.ctx, ticketID); err == nil && t != nil && t.ColunaAtual != ColunaUpgrade {
		if err := a.MoveTicket(ticketID, ColunaUpgrade, a.contagemColuna(ColunaUpgrade)); err != nil {
			return *c, friendly("SalvarComissao", err)
		}
	}
	return *c, nil
}

// MarcarComissaoRecebida marca a comissão do card como recebida (histórico).
func (a *App) MarcarComissaoRecebida(ticketID int64) (db.Comissao, error) {
	c, err := a.comissoes.MarcarRecebida(a.ctx, ticketID)
	if err != nil {
		return db.Comissao{}, friendly("MarcarComissaoRecebida", err)
	}
	return *c, nil
}

// ListarComissoes devolve pendentes + recebidas (histórico de comissões).
func (a *App) ListarComissoes() ([]db.Comissao, error) {
	cs, err := a.comissoes.Listar(a.ctx)
	if err != nil {
		return nil, friendly("ListarComissoes", err)
	}
	return cs, nil
}

// parseReaisParaCents aceita o que o usuário/modelo digitar: "5000",
// "5.000", "5.000,50", "R$ 1.234,56", "1234.56" — e devolve centavos.
func parseReaisParaCents(s string) (int64, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "R$")
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "")
	if s == "" {
		return 0, fmt.Errorf("valor vazio")
	}
	negativo := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	// Separador decimal = o ÚLTIMO separador que aparece (padrão das
	// planilhas do app: pt-BR usa vírgula, mas aceita ponto). Exceção: ponto
	// seguido de exatamente 3 dígitos sem vírgula na string é milhar pt-BR
	// ("5.000" = 5.000,00; "1.5" continua 1,50).
	ultVirg, ultPonto := strings.LastIndex(s, ","), strings.LastIndex(s, ".")
	var dec string
	switch {
	case ultVirg > ultPonto:
		dec = ","
	case ultPonto > ultVirg:
		dec = "."
	}
	intPart, fracPart := s, "00"
	if dec != "" {
		parts := strings.SplitN(s, dec, 2)
		intPart, fracPart = parts[0], parts[1]
		if dec == "." && !strings.Contains(s, ",") && len(fracPart) == 3 {
			intPart, fracPart = s, "00"
		}
	}
	intPart = strings.NewReplacer(".", "", ",", "").Replace(intPart)
	if intPart == "" {
		intPart = "0"
	}
	if fracPart == "" {
		fracPart = "0"
	} else if len(fracPart) == 1 {
		fracPart += "0"
	} else if len(fracPart) > 2 {
		fracPart = fracPart[:2] // centavos extras são truncados
	}
	if _, err := strconv.ParseInt(intPart, 10, 64); err != nil {
		return 0, fmt.Errorf("valor inválido: %q", s)
	}
	if _, err := strconv.ParseInt(fracPart, 10, 64); err != nil {
		return 0, fmt.Errorf("valor inválido: %q", s)
	}
	cents, _ := strconv.ParseInt(intPart+fracPart, 10, 64)
	if negativo {
		cents = -cents
	}
	return cents, nil
}

// garantirUpgradeUltimo deixa a coluna Upgrade sempre como a última do board
// (ex.: "Ticket Fechado" criada depois dela precisa ficar antes).
func (a *App) garantirUpgradeUltimo(ctx context.Context) {
	cs, err := a.colunas.List(ctx)
	if err != nil || len(cs) < 2 {
		return
	}
	idxUpgrade, idxFechado := -1, -1
	for i, c := range cs {
		switch c {
		case ColunaUpgrade:
			idxUpgrade = i
		case ColunaTicketFechado:
			idxFechado = i
		}
	}
	if idxUpgrade < 0 || idxFechado < 0 || idxUpgrade > idxFechado {
		return
	}
	// Ticket Fechado está depois do Upgrade: reordena com o Upgrade no fim.
	nova := make([]string, 0, len(cs))
	for i, c := range cs {
		if i != idxUpgrade {
			nova = append(nova, c)
		}
	}
	nova = append(nova, ColunaUpgrade)
	_ = a.colunas.Reordenar(ctx, nova)
}
