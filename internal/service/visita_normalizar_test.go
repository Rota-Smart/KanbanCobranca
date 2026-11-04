package service

import "testing"

func TestNormalizarQuando(t *testing.T) {
	casos := []struct {
		entrada     string
		hora, turno string
	}{
		{"08:45", "08:45", ""},
		{"8:45", "08:45", ""},
		{"8h45", "08:45", ""},
		{"8h45min", "08:45", ""},
		{"8h", "08:00", ""},
		{"9 horas", "09:00", ""},
		{"Manhã", "", "Manhã"},
		{"manhã", "", "Manhã"},
		{"TARDE", "", "Tarde"},
		{" noite ", "", "Noite"},
		{"manhã 8:45", "08:45", ""}, // hora específica vence o turno
		{"14:00", "14:00", ""},
	}
	for _, c := range casos {
		h, tn := NormalizarQuando(c.entrada)
		if h != c.hora || tn != c.turno {
			t.Errorf("NormalizarQuando(%q) = (%q, %q); quero (%q, %q)",
				c.entrada, h, tn, c.hora, c.turno)
		}
	}
	// Lixo volta como veio (o chamador valida e responde o erro de turno).
	if h, tn := NormalizarQuando("depois do almoço"); h != "" || tn != "depois do almoço" {
		t.Errorf("NormalizarQuando de texto livre = (%q, %q); quero preservar o texto", h, tn)
	}
	if h, tn := NormalizarQuando(""); h != "" || tn != "" {
		t.Errorf("NormalizarQuando(\"\") = (%q, %q); quero vazio", h, tn)
	}
	// Fora da faixa não vira hora.
	if h, _ := NormalizarQuando("25h70"); h != "" {
		t.Errorf("hora inválida 25h70 virou %q", h)
	}
}
