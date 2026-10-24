package service

import "testing"

func TestParseIntencaoCard(t *testing.T) {
	cases := []struct {
		msg    string
		quer   bool
		apagar bool
		alvo   string
		coluna string
	}{
		{"apague o card do João", true, true, "do João", ""},
		{"Apagar card 12345.", true, true, "12345", ""},
		{"exclua o card da Maria", true, true, "da Maria", ""},
		{"mova o card do João para Cobrança", true, false, "do João", "Cobrança"},
		{"mover card #456 para a coluna Orçamento", true, false, "#456", "Orçamento"},
		{"passe o card 123 pra Negociação", true, false, "123", "Negociação"},
		{"mova o card para Cobrança", false, false, "", ""}, // sem alvo
		{"busque o cliente João", false, false, "", ""},
		{"como você funciona?", false, false, "", ""},
	}
	for _, tc := range cases {
		in, ok := ParseIntencaoCard(tc.msg)
		if ok != tc.quer {
			t.Errorf("%q: ok=%v, quer %v", tc.msg, ok, tc.quer)
			continue
		}
		if !ok {
			continue
		}
		if in.Apagar != tc.apagar || in.Alvo != tc.alvo || in.Coluna != tc.coluna {
			t.Errorf("%q: veio %+v, quer apagar=%v alvo=%q coluna=%q", tc.msg, in, tc.apagar, tc.alvo, tc.coluna)
		}
	}
}

func TestResolverCard(t *testing.T) {
	cards := []CardResumo{
		{1, "João da Silva", "123"},
		{2, "Maria João", "456"},
		{3, "Joaquim", ""},
	}
	if c, err := ResolverCard("123", cards); err != nil || c.ID != 1 {
		t.Errorf("por código: %+v err=%v", c, err)
	}
	if c, err := ResolverCard("#456", cards); err != nil || c.ID != 2 {
		t.Errorf("por #código: %+v err=%v", c, err)
	}
	if _, err := ResolverCard("99", cards); err == nil {
		t.Error("número que não é código nem id deveria falhar")
	}
	if c, err := ResolverCard("joaquim", cards); err != nil || c.ID != 3 {
		t.Errorf("por nome: %+v err=%v", c, err)
	}
	if _, err := ResolverCard("jo", cards); err == nil {
		t.Error("dois matches (João/Maria João/Joaquim) deveria ser ambíguo")
	}
	if _, err := ResolverCard("zé", cards); err == nil {
		t.Error("sem match deveria falhar")
	}
}
