package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Relatório diário/semanal: resumo em texto do período com números do banco
// local — mesmos critérios do painel de métricas (movimentações para
// "Ticket Fechado" contam fechamentos; visitas realizadas usam
// visita_realizada_em; novos cards usam created_at).

// datasRelatorio resolve o período: vazio = últimos 7 dias (com fim hoje).
// Só um lado vazio: inicio vira o próprio fim; fim vira hoje.
func datasRelatorio(inicio, fim string) (string, string, error) {
	hoje := time.Now().Format("2006-01-02")
	parse := func(v, nome string) (string, error) {
		if _, err := time.Parse("2006-01-02", v); err != nil {
			return "", fmt.Errorf("data de %s inválida (%q): use AAAA-MM-DD", nome, v)
		}
		return v, nil
	}
	if inicio == "" && fim == "" {
		fim = hoje
		inicio = time.Now().AddDate(0, 0, -7).Format("2006-01-02")
		return inicio, fim, nil
	}
	if inicio == "" {
		inicio = fim
	}
	if fim == "" {
		fim = hoje
	}
	var err error
	if inicio, err = parse(inicio, "início"); err != nil {
		return "", "", err
	}
	if fim, err = parse(fim, "fim"); err != nil {
		return "", "", err
	}
	return inicio, fim, nil
}

// GerarRelatorio monta o texto do resumo do período (diário/semanal).
// O texto é simples, em português, pronto para colar num WhatsApp/e-mail.
func (a *App) GerarRelatorio(inicio, fim string) (string, error) {
	if a.db == nil {
		return "", fmt.Errorf("banco indisponível nesta instalação")
	}
	inicio, fim, err := datasRelatorio(inicio, fim)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := a.contadoresRelatorio(ctx, inicio, fim)

	var b strings.Builder
	fmt.Fprintf(&b, "Relatório — %s a %s\n", inicio, fim)
	fmt.Fprintf(&b, "Gerado em %s\n", time.Now().Format("02/01/2006 15:04"))
	fmt.Fprintf(&b, "\nAcordos fechados: %d", c.acordos)
	fmt.Fprintf(&b, "\nVisitas realizadas: %d", c.visitas)
	fmt.Fprintf(&b, "\nTickets encerrados (movidos para %s): %d", ColunaTicketFechado, c.fechados)
	fmt.Fprintf(&b, "\nTentativas de contato: %d", c.tentativas)
	fmt.Fprintf(&b, "\nNovos cards criados: %d", c.novos)
	return b.String(), nil
}

// contadoresRel reúne os números do período (mesmos critérios do painel de
// métricas). Serve tanto para o relatório manual quanto para o semanal
// automático e a linha comparativa com a semana anterior.
type contadoresRel struct {
	novos      int
	visitas    int
	fechados   int
	acordos    int
	tentativas int
}

// contadoresRelatorio conta os números do período no banco local.
func (a *App) contadoresRelatorio(ctx context.Context, inicio, fim string) contadoresRel {
	um := func(q string, args ...any) int {
		var n int
		_ = a.db.QueryRowContext(ctx, q, args...).Scan(&n)
		return n
	}
	return contadoresRel{
		novos:      um(`SELECT count(*) FROM tickets WHERE substr(created_at,1,10) >= ? AND substr(created_at,1,10) <= ?`, inicio, fim),
		visitas:    um(`SELECT count(*) FROM tickets WHERE visita_realizada_em != '' AND substr(visita_realizada_em,1,10) >= ? AND substr(visita_realizada_em,1,10) <= ?`, inicio, fim),
		fechados:   um(`SELECT count(*) FROM movimentacoes WHERE para_coluna = ? AND substr(created_at,1,10) >= ? AND substr(created_at,1,10) <= ?`, ColunaTicketFechado, inicio, fim),
		acordos:    um(`SELECT count(*) FROM cobrancas WHERE status = 'concluida' AND substr(updated_at,1,10) >= ? AND substr(updated_at,1,10) <= ?`, inicio, fim),
		tentativas: um(`SELECT count(*) FROM tentativas_contato WHERE substr(created_at,1,10) >= ? AND substr(created_at,1,10) <= ?`, inicio, fim),
	}
}
