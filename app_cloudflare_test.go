package main

import (
	"encoding/json"
	"testing"
)

func TestModelosComFerramentasFiltraEOrdena(t *testing.T) {
	raw := `{"success":true,"result":[
	 {"name":"@cf/caro","properties":[{"property_id":"function_calling","value":"true"},{"property_id":"price","value":[{"unit":"per M input tokens","price":1.4},{"unit":"per M output tokens","price":4.4},{"unit":"per M cached input tokens","price":0.001}]}]},
	 {"name":"@cf/sem-tools","properties":[{"property_id":"price","value":[{"unit":"per M input tokens","price":0.01}]}]},
	 {"name":"@cf/sem-preco","properties":[{"property_id":"function_calling","value":"true"}]},
	 {"name":"@cf/barato","properties":[{"property_id":"function_calling","value":"true"},{"property_id":"price","value":[{"unit":"per M input tokens","price":0.0509},{"unit":"per M output tokens","price":0.335}]}]}]}`
	var cat cfCatalogo
	if err := json.Unmarshal([]byte(raw), &cat); err != nil {
		t.Fatal(err)
	}
	got := modelosComFerramentas(cat)
	if len(got) != 3 || got[0].Nome != "@cf/barato" || got[1].Nome != "@cf/caro" || got[2].Nome != "@cf/sem-preco" {
		t.Fatalf("ordem/filtro errados: %+v", got)
	}
	if got[1].EntradaUSDporM != 1.4 {
		t.Fatalf("preço de cache sobrescreveu a entrada: %v", got[1].EntradaUSDporM)
	}
	if got[0].RespostasDiaFree < 400 || got[0].RespostasDiaFree > 600 {
		t.Fatalf("estimativa barato fora do esperado: %d", got[0].RespostasDiaFree)
	}
}

func TestProvedorEfetivoViraAuto(t *testing.T) {
	casos := []struct {
		salvo    string
		groq, cf bool
		esperado string
	}{
		{"", true, true, "auto"},
		{"groq", true, true, "auto"},
		{"groq", true, false, "groq"},
		{"groq", false, true, "groq"},
		{"cerebras", true, true, "cerebras"},
		{"cloudflare", true, true, "cloudflare"},
	}
	for _, c := range casos {
		if got := provedorEfetivo(c.salvo, c.groq, c.cf); got != c.esperado {
			t.Errorf("%+v: %s", c, got)
		}
	}
}
