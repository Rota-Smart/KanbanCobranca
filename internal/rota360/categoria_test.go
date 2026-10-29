package rota360

import "testing"

func TestCategoriaEhMensalidade(t *testing.T) {
	sim := []string{"Mensalidade", "mensalidade", "Mensal", "RECORRENTE", "Boleto Mensal", "MÊS mensalidade", "mensalidade_atrasada"}
	for _, c := range sim {
		if !CategoriaEhMensalidade(c) {
			t.Errorf("%q deveria ser mensalidade", c)
		}
	}
	nao := []string{"", "Boleto agrupado", "boleto_agrupado", "Avulso", "Gerado por OS", "multa", "bimestral"}
	for _, c := range nao {
		if CategoriaEhMensalidade(c) {
			t.Errorf("%q NÃO deveria ser mensalidade", c)
		}
	}
}

func TestSlugCategoria(t *testing.T) {
	casos := map[string]string{
		"Mensalidade":       "mensalidade",
		"Boleto Agrupado":   "boleto_agrupado",
		"  Gerado por OS  ": "gerado_por_os",
		"PROPORCIONAL-MÊS":  "proporcional_mes",
	}
	for in, want := range casos {
		if got := slugCategoria(in); got != want {
			t.Errorf("slug(%q)=%q, esperava %q", in, got, want)
		}
	}
}
