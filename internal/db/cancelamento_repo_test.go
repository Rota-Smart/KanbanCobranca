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

	m, err := r.Metricas(ctx, 0, true)
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

// Tags duplicadas antes da chave canônica ("Problemas Financeiros" e
// "Problema Financeiro") viram uma: cancelamentos re-atribuídos e a
// duplicada apagada (DeduplicarMotivos roda no boot).
func TestDeduplicarMotivos(t *testing.T) {
	d, err := Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer d.Close()
	ctx := context.Background()
	r := NewCancelamentoRepo(d)

	m1, err := r.MotivoCriarSeNaoExiste(ctx, "Problemas Financeiros")
	if err != nil {
		t.Fatalf("motivo 1: %v", err)
	}
	// Cenário do legado: a mesma tag gravada com chave antiga (plural).
	var m2 int64
	if _, err := d.ExecContext(ctx,
		`INSERT INTO motivos_cancelamento (nome, nome_chave) VALUES ('Problemas Financeiros', 'problemas financeiros')`); err != nil {
		t.Fatalf("motivo legado: %v", err)
	}
	m2 = m1.ID + 1
	_ = m2
	var t1, t2 int64
	for _, tid := range []*int64{&t1, &t2} {
		if err := d.QueryRowContext(ctx,
			`INSERT INTO tickets (nome_cliente, tipo_ticket, coluna_atual) VALUES ('X', 'Outro', 'Fazer contato') RETURNING id`).Scan(tid); err != nil {
			t.Fatalf("ticket: %v", err)
		}
	}
	if err := r.CancelarMarcar(ctx, t1, m1.ID, "perda"); err != nil {
		t.Fatal(err)
	}
	if err := r.CancelarMarcar(ctx, t2, m2, "perda"); err != nil {
		t.Fatal(err)
	}

	n, err := r.DeduplicarMotivos(ctx)
	if err != nil || n != 1 {
		t.Fatalf("dedup: n=%d err=%v", n, err)
	}
	var total int64
	if err := d.QueryRowContext(ctx, `SELECT count(*) FROM motivos_cancelamento`).Scan(&total); err != nil || total != 1 {
		t.Fatalf("deveria restar 1 tag: %d err=%v", total, err)
	}
	if err := d.QueryRowContext(ctx, `SELECT count(*) FROM cancelamentos WHERE motivo_id = ?`, m1.ID).Scan(&total); err != nil || total != 2 {
		t.Fatalf("cancelamentos deveriam apontar pro motivo antigo: %d err=%v", total, err)
	}
	// Nova inserção com qualquer grafia casa a tag canônica existente.
	m3, err := r.MotivoCriarSeNaoExiste(ctx, "PROBLEMA FINANCEIRO")
	if err != nil || m3.ID != m1.ID {
		t.Fatalf("deveria reusar a tag: %+v err=%v", m3, err)
	}
}
