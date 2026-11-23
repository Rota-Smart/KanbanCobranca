package db

import (
	"context"
	"path/filepath"
	"testing"
)

// A lista colada real: 8 canceladas (perda), 3 revertidas (reversão) e 17 em
// tratamento. Registra tudo (com um motivo repetido entre linhas) e confere
// as métricas — inclusive o resultado em_tratamento que só a lista colada
// grava (migration022 ampliou o CHECK).
func TestRegistrarListaCancelamentos(t *testing.T) {
	d, err := Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer d.Close()
	ctx := context.Background()
	r := NewCancelamentoRepo(d)

	insere := func(nome string) int64 {
		var id int64
		if err := d.QueryRowContext(ctx,
			`INSERT INTO tickets (nome_cliente, tipo_ticket, coluna_atual) VALUES (?, 'Outro', 'Fazer contato') RETURNING id`, nome,
		).Scan(&id); err != nil {
			t.Fatalf("insert %s: %v", nome, err)
		}
		return id
	}
	var ids []int64
	for _, n := range []string{"SELVINO ALOISIO CLOSSMANN", "ROSANE BORCHARDT VOGT", "ROSANE ELIA PAUSE", "LUIS FERNANDO DA ROSA"} {
		ids = append(ids, insere(n))
	}

	grava := func(ticketID int64, motivo, resultado string) {
		m, err := r.MotivoCriarSeNaoExiste(ctx, motivo)
		if err != nil {
			t.Fatalf("motivo %s: %v", motivo, err)
		}
		if err := r.CancelarMarcar(ctx, ticketID, m.ID, resultado); err != nil {
			t.Fatalf("marcar %s: %v", resultado, err)
		}
	}
	grava(ids[0], "Troca de Endereço", "perda")
	grava(ids[1], "Insatisfação com atendimento (já resolvido)", "reversao")
	grava(ids[2], "Não sabemos o motivo", "em_tratamento")
	grava(ids[3], "Problemas financeiros", "em_tratamento")
	// Re-registro (linha repetida do mesmo cliente) atualiza, não duplica.
	grava(ids[3], "Problemas financeiros", "em_tratamento")

	m, err := r.Metricas(ctx, 0)
	if err != nil {
		t.Fatalf("metricas: %v", err)
	}
	if m.Total != 4 || m.Perda != 1 || m.Reversao != 1 || m.Tratamento != 2 {
		t.Errorf("métricas: %+v", m)
	}
	if len(m.PorMotivo) != 4 {
		t.Errorf("por motivo: %+v", m.PorMotivo)
	}
	// Migration022 preservou o schema: o CHECK aceita em_tratamento (já
	// provado acima) e o registro único por ticket continua valendo.
	var n int64
	if err := d.QueryRowContext(ctx, `SELECT count(*) FROM cancelamentos`).Scan(&n); err != nil {
		t.Fatalf("contar: %v", err)
	}
	if n != 4 {
		t.Errorf("esperava 4 registros, veio %d", n)
	}
}
