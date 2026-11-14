package main

import (
	"math"
	"testing"
	"time"
)

// TestTempoPorColuna: histórico conhecido de dois cards com movimentações
// com carimbos controlados (relativos a agora), confere a média de cada
// coluna e o intervalo aberto dos cards que ainda estão lá.
//
// Linha do tempo (agora = 0):
//   card A: criado há 40h em "Fazer contato" -> "Em contato" há 30h -> "Negociando" há 6h
//   card B: criado há 40h em "Fazer contato" -> "Em contato" há 10h (aberto)
//
// Esperado:
//   Fazer contato: (10h + 30h) / 2 = 20h, 2 cards
//   Em contato:    (24h + 10h) / 2 = 17h, 2 cards
//   Negociando:    ~6h (aberto), 1 card
func TestTempoPorColuna(t *testing.T) {
	a := newTestApp(t)

	cardA, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket A: %v", err)
	}
	cardB, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket B: %v", err)
	}

	hAtras := func(h float64) string {
		return time.Now().UTC().Add(-time.Duration(h * float64(time.Hour))).Format(formatoCarimbo)
	}
	mov := func(ticket int64, de, para string, horas float64) {
		t.Helper()
		if _, err := a.db.Exec(`INSERT INTO movimentacoes (ticket_id, de_coluna, para_coluna, created_at) VALUES (?, ?, ?, ?)`,
			ticket, de, para, hAtras(horas)); err != nil {
			t.Fatalf("inserir movimentacao: %v", err)
		}
	}
	criado := func(ticket int64, coluna string, horas float64) {
		t.Helper()
		if _, err := a.db.Exec(`UPDATE tickets SET coluna_atual = ?, created_at = ? WHERE id = ?`,
			coluna, hAtras(horas), ticket); err != nil {
			t.Fatalf("ajustar ticket: %v", err)
		}
	}

	criado(cardA.ID, "Fazer contato", 40)
	mov(cardA.ID, "Fazer contato", "Em contato", 30)
	mov(cardA.ID, "Em contato", "Negociando", 6)
	criado(cardB.ID, "Fazer contato", 40)
	mov(cardB.ID, "Fazer contato", "Em contato", 10)

	linhas, err := a.TempoPorColuna()
	if err != nil {
		t.Fatalf("TempoPorColuna: %v", err)
	}

	// Negociando é aberto e depende do agora: tolerância de 2 minutos.
	tolerancia := 2 * time.Minute
	esperado := map[string]struct {
		media time.Duration
		cards int
	}{
		"Fazer contato": {20 * time.Hour, 2},
		"Em contato":    {17 * time.Hour, 2},
		"Negociando":    {6 * time.Hour, 1},
	}
	visto := map[string]bool{}
	for _, l := range linhas {
		e, ok := esperado[l.Coluna]
		if !ok {
			t.Fatalf("coluna inesperada: %s", l.Coluna)
		}
		d := time.Duration(l.MediaSegundos * float64(time.Second))
		if math.Abs(d.Seconds()-e.media.Seconds()) > tolerancia.Seconds() {
			t.Fatalf("%s: media %v, esperava ~%v", l.Coluna, d, e.media)
		}
		if l.Cards != e.cards {
			t.Fatalf("%s: cards=%d, esperava %d", l.Coluna, l.Cards, e.cards)
		}
		visto[l.Coluna] = true
	}
	for c := range esperado {
		if !visto[c] {
			t.Fatalf("faltou a coluna %s: %+v", c, linhas)
		}
	}
}

// Quadro sem movimentações: card novo conta o intervalo aberto na coluna de
// criação, com a média igual ao tempo de vida do card.
func TestTempoPorColunaCardSemMovimento(t *testing.T) {
	a := newTestApp(t)

	card, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	criadoEm := time.Now().UTC().Add(-3 * time.Hour).Format(formatoCarimbo)
	if _, err := a.db.Exec(`UPDATE tickets SET coluna_atual = ?, created_at = ? WHERE id = ?`,
		"Fazer contato", criadoEm, card.ID); err != nil {
		t.Fatalf("ajustar ticket: %v", err)
	}

	linhas, err := a.TempoPorColuna()
	if err != nil {
		t.Fatalf("TempoPorColuna: %v", err)
	}
	if len(linhas) != 1 || linhas[0].Coluna != "Fazer contato" || linhas[0].Cards != 1 {
		t.Fatalf("deveria ter so Fazer contato com 1 card: %+v", linhas)
	}
	d := time.Duration(linhas[0].MediaSegundos * float64(time.Second))
	if d < 2*time.Hour+59*time.Minute || d > 3*time.Hour+time.Minute {
		t.Fatalf("media deveria ser ~3h, veio %v", d)
	}
}

// Sem banco, responde erro claro (mesma mensagem dos outros bindings).
func TestTempoPorColunaSemBanco(t *testing.T) {
	a := &App{}
	if _, err := a.TempoPorColuna(); err == nil {
		t.Fatalf("deveria dar erro de banco indisponível")
	}
}
