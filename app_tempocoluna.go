package main

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Tempo em cada coluna (gargalos): a partir do histórico de movimentações,
// calcula quanto tempo cada card ficou em cada coluna e tira a média —
// pra revelar onde os tickets realmente param ("5 dias em Em contato").
//
// Não precisa de migração: a entrada do card na coluna é o created_at do
// ticket (criação) ou o created_at da movimentação (para_coluna); a saída é
// a movimentação seguinte. Card que ainda está na coluna tem intervalo
// aberto, contado até agora.

// TempoColuna é uma linha da seção "Tempo médio por coluna" do painel 📊.
type TempoColuna struct {
	Coluna        string  `json:"coluna"`
	MediaSegundos float64 `json:"mediaSegundos"`
	Cards         int     `json:"cards"`
}

const formatoCarimbo = "2006-01-02T15:04:05Z"

// intervaloColuna é uma permanência de um card numa coluna.
type intervaloColuna struct {
	coluna string
	inicio time.Time
	fim    time.Time // zero = intervalo aberto (card ainda na coluna)
}

// TempoPorColuna devolve, por coluna, o tempo médio de permanência dos cards
// (incluindo os intervalos abertos dos cards que ainda estão lá) e quantos
// cards entraram na conta.
func (a *App) TempoPorColuna() ([]TempoColuna, error) {
	if a.db == nil {
		return nil, fmt.Errorf("banco indisponível nesta instalação")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Consultas separadas: o LEFT JOIN com movimentacoes fica instável no
	// driver sqlite (linha sem movimento some); o casamento é feito aqui.
	// Cada card começa com um intervalo aberto na coluna de criação; cada
	// movimentação fecha o intervalo corrente e abre o da coluna de destino.
	abertos := map[int64]intervaloColuna{}
	linhasTickets, err := a.db.QueryContext(ctx,
		`SELECT id, created_at, coluna_atual FROM tickets ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("TempoPorColuna: %w", err)
	}
	for linhasTickets.Next() {
		var id int64
		var criado, colunaAtual string
		if err := linhasTickets.Scan(&id, &criado, &colunaAtual); err != nil {
			linhasTickets.Close()
			return nil, fmt.Errorf("TempoPorColuna: %w", err)
		}
		if inicio, ok := parseCarimbo(criado); ok {
			abertos[id] = intervaloColuna{coluna: colunaAtual, inicio: inicio}
		} // carimbo ilegível: não tem como medir; pula o card
	}
	if err := linhasTickets.Close(); err != nil && err != linhasTickets.Err() {
		return nil, fmt.Errorf("TempoPorColuna: %w", err)
	}
	if err := linhasTickets.Err(); err != nil {
		return nil, fmt.Errorf("TempoPorColuna: %w", err)
	}

	soma := map[string]time.Duration{}
	cards := map[string]int{}
	fecha := func(iv intervaloColuna, fim time.Time) {
		if fim.After(iv.inicio) {
			soma[iv.coluna] += fim.Sub(iv.inicio)
			cards[iv.coluna]++
		}
	}

	linhas, err := a.db.QueryContext(ctx,
		`SELECT ticket_id, para_coluna, created_at FROM movimentacoes ORDER BY ticket_id, created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("TempoPorColuna: %w", err)
	}
	for linhas.Next() {
		var ticket int64
		var paraColuna, movEm string
		if err := linhas.Scan(&ticket, &paraColuna, &movEm); err != nil {
			linhas.Close()
			return nil, fmt.Errorf("TempoPorColuna: %w", err)
		}
		atual, ok := abertos[ticket]
		if !ok {
			continue
		}
		movimento, okMov := parseCarimbo(movEm)
		if !okMov {
			continue
		}
		// o card saiu da coluna atual na hora da movimentação…
		fecha(atual, movimento)
		// …e entrou na coluna de destino.
		abertos[ticket] = intervaloColuna{coluna: paraColuna, inicio: movimento}
	}
	if err := linhas.Close(); err != nil {
		return nil, fmt.Errorf("TempoPorColuna: %w", err)
	}
	if err := linhas.Err(); err != nil {
		return nil, fmt.Errorf("TempoPorColuna: %w", err)
	}

	agora := time.Now().UTC()
	for _, iv := range abertos {
		fecha(iv, agora) // card que ainda está na coluna: intervalo até agora
	}

	saida := []TempoColuna{}
	for coluna, total := range soma {
		n := cards[coluna]
		if n == 0 {
			continue
		}
		saida = append(saida, TempoColuna{
			Coluna:        coluna,
			MediaSegundos: (total / time.Duration(n)).Seconds(),
			Cards:         n,
		})
	}
	sort.Slice(saida, func(i, j int) bool { return saida[i].MediaSegundos > saida[j].MediaSegundos })
	return saida, nil
}

func parseCarimbo(s string) (time.Time, bool) {
	t, err := time.Parse(formatoCarimbo, s)
	return t, err == nil
}
