package main

import (
	"strconv"
	"testing"

	"rota-smart/internal/db"
)

func TestParseReaisParaCents(t *testing.T) {
	casos := []struct {
		in   string
		out  int64
		erro bool
	}{
		{"5000", 500000, false},
		{"5.000", 500000, false},
		{"5.000,50", 500050, false},
		{"1234.56", 123456, false},
		{"R$ 1.234,56", 123456, false},
		{"R$5000", 500000, false},
		{"0,5", 50, false},
		{"1.234,5678", 123456, false}, // centavos extras truncados
		{"", 0, true},
		{"abc", 0, true},
	}
	for _, c := range casos {
		got, err := parseReaisParaCents(c.in)
		if c.erro {
			if err == nil {
				t.Errorf("parseReaisParaCents(%q) deveria falhar, devolveu %d", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseReaisParaCents(%q): %v", c.in, err)
			continue
		}
		if got != c.out {
			t.Errorf("parseReaisParaCents(%q) = %d, queria %d", c.in, got, c.out)
		}
	}
}

func TestComissaoDe(t *testing.T) {
	// serviço 10.000,00 → 500,00 | parcela 1.000,00 → 750,00 | total 1.250,00
	if got := db.ComissaoDe(1000000, 100000); got != 125000 {
		t.Fatalf("ComissaoDe = %d, queria 125000", got)
	}
	if got := db.ComissaoDe(0, 0); got != 0 {
		t.Fatalf("ComissaoDe zerado = %d", got)
	}
}

func TestSalvarComissaoMoveParaUpgrade(t *testing.T) {
	a := newTestApp(t)
	tk, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}

	c, err := a.SalvarComissao(tk.ID, 1000000, 200000, "upgrade de câmeras", "2026-10-28")
	if err != nil {
		t.Fatalf("SalvarComissao: %v", err)
	}
	// 5% de 10.000,00 = 500,00 + 75% de 2.000,00 = 1.500,00 → 2.000,00
	if c.ComissaoCents != 200000 {
		t.Fatalf("comissão = %d, queria 200000", c.ComissaoCents)
	}
	movido, err := a.tickets.Get(a.ctx, tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if movido.ColunaAtual != ColunaUpgrade {
		t.Fatalf("card deveria ter ido para %s, está em %s", ColunaUpgrade, movido.ColunaAtual)
	}

	// upsert: sobrescreve, não duplica
	if _, err := a.SalvarComissao(tk.ID, 2000000, 0, "", "2026-10-29"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	cs, err := a.ListarComissoes()
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].ValorServicoCents != 2000000 || cs[0].ValorParcelaCents != 0 {
		t.Fatalf("upsert falhou: %+v", cs)
	}

	// recebida → histórico
	rec, err := a.MarcarComissaoRecebida(tk.ID)
	if err != nil {
		t.Fatalf("MarcarComissaoRecebida: %v", err)
	}
	if !rec.Recebido || rec.RecebidoEm == "" {
		t.Fatalf("não marcou recebida: %+v", rec)
	}
}

func TestUpgradeUltimaColuna(t *testing.T) {
	a := newTestApp(t)
	cs, err := a.colunas.List(a.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cs[len(cs)-1] != ColunaUpgrade {
		t.Fatalf("Upgrade deveria ser a última coluna, ordem: %v", cs)
	}

	// "Ticket Fechado" criada depois não pode passar na frente do Upgrade
	if _, err := a.colunas.Add(a.ctx, ColunaTicketFechado); err != nil {
		t.Fatal(err)
	}
	a.garantirUpgradeUltimo(a.ctx)
	cs, err = a.colunas.List(a.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cs[len(cs)-1] != ColunaUpgrade {
		t.Fatalf("Upgrade deixou de ser a última: %v", cs)
	}
	if cs[len(cs)-2] != ColunaTicketFechado {
		t.Fatalf("Ticket Fechado deveria vir antes do Upgrade: %v", cs)
	}
}

func TestRegistrarComissaoTool(t *testing.T) {
	a := newTestApp(t)
	tk, err := a.SaveTicket(exemploTicket())
	if err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}
	args := `{"ticketId":` + itoa64(tk.ID) + `,"valorServico":"5000","valorParcela":"800,00","observacoes":"teste","dataRegistro":"2026-10-28"}`
	res, resumo, err := a.ExecutarAgente(a.ctx, "registrar_comissao", args)
	if err != nil {
		t.Fatalf("registrar_comissao: %v", err)
	}
	if res == "" || resumo == "" {
		t.Fatalf("respostas vazias: %q / %q", res, resumo)
	}
	movido, err := a.tickets.Get(a.ctx, tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if movido.ColunaAtual != ColunaUpgrade {
		t.Fatalf("card deveria estar em %s, está em %s", ColunaUpgrade, movido.ColunaAtual)
	}

	if _, _, err := a.ExecutarAgente(a.ctx, "comissao_recebida", `{"ticketId":`+itoa64(tk.ID)+`}`); err != nil {
		t.Fatalf("comissao_recebida: %v", err)
	}
	cs, err := a.ListarComissoes()
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || !cs[0].Recebido {
		t.Fatalf("histórico: %+v", cs)
	}
}

func itoa64(v int64) string {
	return strconv.FormatInt(v, 10)
}
