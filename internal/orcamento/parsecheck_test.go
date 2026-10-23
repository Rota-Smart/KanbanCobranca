package orcamento

import (
	"os"
	"testing"
)

func TestPlanilhaReal(t *testing.T) {
	p := os.Getenv("CALC_XLSX")
	if p == "" {
		t.Skip("sem CALC_XLSX")
	}
	tabela, err := LerTabela(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, tipo := range tabela.Tipos {
		t.Logf("== %s | taxa R$%.2f | faixas %d | orientacao: %.40s", tipo.Nome, tipo.TaxaInstalacao, len(tipo.FaixasMensal), tipo.Orientacoes)
		for _, sec := range tipo.Secoes {
			for _, it := range sec.Itens {
				t.Logf("   [%s] %s | taxa=%v mensal=%v", sec.Titulo, it.Nome, it.Taxa, it.Mensal)
			}
		}
	}
	res, err := Calcular(tabela, Selecao{TipoSistema: "ALARME", Itens: map[string]int{
		"Sensor de Presença": 2, "Controle": 2, "Câmera WIFI Interna": 3, "Câmera WIFI Externa": 1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("taxa=%.2f mensal=%.2f linhas=%v", res.TaxaInstalacao, res.Mensal, res.Linhas)
}
