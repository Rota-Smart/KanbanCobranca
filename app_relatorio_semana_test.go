package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rota-smart/internal/db"
)

// TestLinhaComparativaSemanal: a linha final compara os contadores da semana
// com a janela anterior, mencionando só o que mudou (ordem fixa).
func TestLinhaComparativaSemanal(t *testing.T) {
	a := newTestApp(t)
	a.caminhoRelatorios = filepath.Join(t.TempDir(), "relatorios.json")

	if _, err := a.colunas.Add(a.ctx, ColunaTicketFechado); err != nil {
		t.Fatalf("criar coluna: %v", err)
	}
	// 2 acordos concluídos HOJE (semana atual) e 1 na semana anterior
	// (8 dias atrás) — cobrancas tem UNIQUE por ticket, um acordo por card.
	hoje := time.Now().Format("2006-01-02T15:04:05")
	antigo := time.Now().AddDate(0, 0, -8).Format("2006-01-02T15:04:05")
	for _, quando := range []string{hoje, hoje, antigo} {
		card, err := a.SaveTicket(exemploTicket())
		if err != nil {
			t.Fatalf("SaveTicket: %v", err)
		}
		if _, err := a.db.Exec(`INSERT INTO cobrancas (ticket_id, total_aberto_cents, entrada_cents, numero_vezes, parcela_base_cents, parcelado_cents, ultima_parcela_cents, final_cents, status, ativa, updated_at)
			VALUES (?, 10000, 0, 1, 10000, 10000, 10000, 10000, 'concluida', 1, ?)`, card.ID, quando); err != nil {
			t.Fatalf("cobranca: %v", err)
		}
		// Cards criados fora das duas janelas: assim só "acordos" muda.
		if _, err := a.db.Exec(`UPDATE tickets SET created_at = '2020-01-01T10:00:00Z' WHERE id = ?`, card.ID); err != nil {
			t.Fatalf("envelhecer card: %v", err)
		}
	}

	linha := a.linhaComparativaSemanal()
	if !strings.Contains(linha, "acordos +1") {
		t.Fatalf("esperava 'acordos +1' na comparação, veio: %q", linha)
	}
	if !strings.Contains(linha, "vs semana anterior:") {
		t.Fatalf("linha deveria começar com 'vs semana anterior:': %q", linha)
	}
	// Visitas/tentativas/cards iguais não entram.
	if strings.Contains(linha, "visitas") || strings.Contains(linha, "novos cards") {
		t.Fatalf("só o que mudou deveria aparecer: %q", linha)
	}
}

// TestRelatorioSemanalAuto: primeira chamada gera e guarda histórico + carimbo;
// chamada em seguida não gera de novo; carimbo velho (8 dias) gera de novo.
func TestRelatorioSemanalAuto(t *testing.T) {
	a := newTestApp(t)
	a.caminhoRelatorios = filepath.Join(t.TempDir(), "relatorios.json")
	a.config = db.NewConfigRepo(a.db)

	if _, err := a.colunas.Add(a.ctx, ColunaTicketFechado); err != nil {
		t.Fatalf("criar coluna: %v", err)
	}
	if _, err := a.SaveTicket(exemploTicket()); err != nil {
		t.Fatalf("SaveTicket: %v", err)
	}

	// Sem carimbo: gera na hora.
	r, err := a.RelatorioSemanalAuto()
	if err != nil {
		t.Fatalf("RelatorioSemanalAuto: %v", err)
	}
	if !r.Novo || !strings.Contains(r.Texto, "Novos cards criados: 1") {
		t.Fatalf("esperava resumo novo com 1 card, veio: %+v", r)
	}

	// Gravou histórico com o comparativo.
	hist := a.HistoricoRelatorios()
	if len(hist) != 1 || !strings.Contains(hist[0].Texto, "vs semana anterior") {
		t.Fatalf("histórico inesperado: %+v", hist)
	}

	// Carimbo de hoje: não gera de novo.
	r2, err := a.RelatorioSemanalAuto()
	if err != nil {
		t.Fatalf("segunda chamada: %v", err)
	}
	if r2.Novo || r2.Texto != "" {
		t.Fatalf("não deveria gerar de novo no mesmo dia: %+v", r2)
	}

	// Carimbo com 8 dias: gera de novo e o histórico fica com 2 (recente primeiro).
	antigo := time.Now().AddDate(0, 0, -8).Format("2006-01-02")
	if err := a.config.Set(a.ctx, cfgRelatorioSemanalUltimo, antigo); err != nil {
		t.Fatalf("carimbo antigo: %v", err)
	}
	r3, err := a.RelatorioSemanalAuto()
	if err != nil {
		t.Fatalf("chamada com carimbo antigo: %v", err)
	}
	if !r3.Novo {
		t.Fatalf("carimbo com 8 dias deveria regerar: %+v", r3)
	}
	hist = a.HistoricoRelatorios()
	if len(hist) != 2 || hist[0].Texto != r3.Texto {
		t.Fatalf("histórico deveria ter 2 entradas, mais recente primeiro: %+v", hist)
	}
}

// TestHistoricoRelatoriosTeto: o histórico guarda no máximo 12 resumos.
func TestHistoricoRelatoriosTeto(t *testing.T) {
	a := newTestApp(t)
	a.caminhoRelatorios = filepath.Join(t.TempDir(), "relatorios.json")

	var ultimoQuando string
	for i := 0; i < maxRelatoriosHistorico+3; i++ {
		quando := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		ultimoQuando = quando // gravado por último → deve vir primeiro
		if err := a.salvarResumoSemanal(ResumoSalvo{Quando: quando, Texto: "resumo " + quando}); err != nil {
			t.Fatalf("salvar resumo %d: %v", i, err)
		}
	}
	hist := a.HistoricoRelatorios()
	if len(hist) != maxRelatoriosHistorico {
		t.Fatalf("esperava teto de %d, veio %d", maxRelatoriosHistorico, len(hist))
	}
	if hist[0].Quando != ultimoQuando {
		t.Fatalf("mais recente deveria vir primeiro: %+v", hist[0])
	}
}

// TestSetWhatsappDono: limpa para só dígitos, valida tamanho, vazio é permitido
// (wa.me sem número deixa escolher o contato).
func TestSetWhatsappDono(t *testing.T) {
	a := newTestApp(t)
	a.config = db.NewConfigRepo(a.db)

	if err := a.SetWhatsappDono(" +55 (11) 99999-8888 "); err != nil {
		t.Fatalf("SetWhatsappDono: %v", err)
	}
	if got := a.configGet(a.ctx, cfgWhatsappDono); got != "5511999998888" {
		t.Fatalf("esperava só dígitos, veio %q", got)
	}
	if err := a.SetWhatsappDono("123"); err == nil {
		t.Fatalf("número curto deveria dar erro")
	}
	if err := a.SetWhatsappDono(""); err != nil {
		t.Fatalf("limpar deveria funcionar: %v", err)
	}
	if got := a.configGet(a.ctx, cfgWhatsappDono); got != "" {
		t.Fatalf("esperava vazio, veio %q", got)
	}
}

// TestAbrirRelatorioWhatsappSemTexto: sem texto não abre nada.
func TestAbrirRelatorioWhatsappSemTexto(t *testing.T) {
	a := &App{}
	if err := a.AbrirRelatorioWhatsapp("  "); err == nil {
		t.Fatalf("texto vazio deveria dar erro")
	}
}
