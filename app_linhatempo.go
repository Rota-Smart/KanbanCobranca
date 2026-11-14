package main

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Linha do tempo do card: junta, em ordem cronológica (mais recente primeiro),
// tudo que aconteceu com o cliente — criação, movimentações de coluna,
// tentativas de contato, acordos (cobrancas), observações e visita realizada.
// Somente leitura: nada aqui grava no banco.

// EventoLinhaDoTempo é um acontecimento do card. Data vem em ISO (UTC) —
// o frontend formata em pt-BR.
type EventoLinhaDoTempo struct {
	Data  string `json:"data"`
	Tipo  string `json:"tipo"` // criacao | movimento | tentativa | acordo | observacao | visita
	Texto string `json:"texto"`
}

// reais formata centavos como "R$ 1.234,56" (mesmo estilo da proposta).
func reais(cents int64) string {
	reais := cents / 100
	cent := cents % 100
	if cents < 0 {
		return fmt.Sprintf("-R$ %d,%02d", -reais, -cent)
	}
	// separador de milhar simples
	d := fmt.Sprintf("%d", reais)
	var out []byte
	for i, c := range []byte(d) {
		if i > 0 && (len(d)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, c)
	}
	return fmt.Sprintf("R$ %s,%02d", string(out), cent)
}

// textoTentativa monta a descrição da tentativa de contato: canal opcional
// e nota livre — ex.: "Tentativa de contato (sem resposta)".
func textoTentativa(canal, nota string) string {
	t := "Tentativa de contato"
	switch {
	case canal != "" && nota != "":
		t += fmt.Sprintf(" via %s — %s", canal, nota)
	case canal != "":
		t += fmt.Sprintf(" via %s", canal)
	case nota != "":
		t += fmt.Sprintf(" (%s)", nota)
	}
	return t
}

// LinhaDoTempo devolve os eventos do card ordenados do mais recente para o
// mais antigo.
func (a *App) LinhaDoTempo(ticketID int64) ([]EventoLinhaDoTempo, error) {
	if a.db == nil {
		return nil, fmt.Errorf("banco indisponível nesta instalação")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
	defer cancel()

	var nome string
	if err := a.db.QueryRowContext(ctx,
		`SELECT nome_cliente FROM tickets WHERE id = ?`, ticketID).Scan(&nome); err != nil {
		return nil, fmt.Errorf("card %d não encontrado", ticketID)
	}

	eventos := []EventoLinhaDoTempo{}
	add := func(data, tipo, texto string) {
		if data == "" {
			return
		}
		eventos = append(eventos, EventoLinhaDoTempo{Data: data, Tipo: tipo, Texto: texto})
	}

	// Criação do card e observações (observações não têm data própria; usam
	// o updated_at do card e só entram quando há texto).
	var criado, atualizado, observacoes, visitaRealizada string
	err := a.db.QueryRowContext(ctx,
		`SELECT created_at, updated_at, observacoes, visita_realizada_em FROM tickets WHERE id = ?`,
		ticketID).Scan(&criado, &atualizado, &observacoes, &visitaRealizada)
	if err != nil {
		return nil, fmt.Errorf("ler card %d: %w", ticketID, err)
	}
	add(criado, "criacao", fmt.Sprintf("Card criado — %s", nome))
	if observacoes != "" {
		resumo := observacoes
		if len(resumo) > 120 {
			resumo = resumo[:120] + "…"
		}
		add(atualizado, "observacao", "Observações: "+resumo)
	}
	if visitaRealizada != "" {
		add(visitaRealizada, "visita", "Visita marcada como realizada")
	}

	// Movimentações de coluna (a primeira não tem coluna de origem).
	rows, err := a.db.QueryContext(ctx,
		`SELECT created_at, de_coluna, para_coluna FROM movimentacoes WHERE ticket_id = ?`, ticketID)
	if err != nil {
		return nil, fmt.Errorf("listar movimentações: %w", err)
	}
	for rows.Next() {
		var data, de, para string
		if err := rows.Scan(&data, &de, &para); err != nil {
			rows.Close()
			return nil, err
		}
		if de == "" {
			add(data, "movimento", fmt.Sprintf("Card entrou em %s", para))
		} else {
			add(data, "movimento", fmt.Sprintf("Card movido de %s para %s", de, para))
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Tentativas de contato.
	rows, err = a.db.QueryContext(ctx,
		`SELECT created_at, canal, nota FROM tentativas_contato WHERE ticket_id = ?`, ticketID)
	if err != nil {
		return nil, fmt.Errorf("listar tentativas de contato: %w", err)
	}
	for rows.Next() {
		var data, canal, nota string
		if err := rows.Scan(&data, &canal, &nota); err != nil {
			rows.Close()
			return nil, err
		}
		add(data, "tentativa", textoTentativa(canal, nota))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Acordos (cobrancas): proposta/aceita/quebrada… com entrada e parcelas.
	rows, err = a.db.QueryContext(ctx,
		`SELECT created_at, status, entrada_cents, numero_vezes, parcela_base_cents
		 FROM cobrancas WHERE ticket_id = ?`, ticketID)
	if err != nil {
		return nil, fmt.Errorf("listar acordos: %w", err)
	}
	for rows.Next() {
		var data, status string
		var entrada, parcela int64
		var vezes int
		if err := rows.Scan(&data, &status, &entrada, &vezes, &parcela); err != nil {
			rows.Close()
			return nil, err
		}
		txt := fmt.Sprintf("Acordo %s: ", status)
		if entrada > 0 {
			txt += fmt.Sprintf("entrada %s, ", reais(entrada))
		}
		txt += fmt.Sprintf("%dx de %s", vezes, reais(parcela))
		add(data, "acordo", txt)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Mais recente primeiro; empate mantém a ordem de inserção estável.
	sort.SliceStable(eventos, func(i, j int) bool { return eventos[i].Data > eventos[j].Data })
	return eventos, nil
}
