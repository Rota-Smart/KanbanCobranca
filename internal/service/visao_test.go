package service

import "testing"

func TestParseClienteTela(t *testing.T) {
	res := parseClienteTela("CODIGO=23125 | NOME=ROFFIX PRODUTOS AUTOMOTIVOS LTDA | CNPJ=43.624.968/0001-00 | MENSALIDADE=R$155,00 | MODALIDADE=LOCAÇÃO")
	if res.Codigo != "23125" || res.Nome != "ROFFIX PRODUTOS AUTOMOTIVOS LTDA" || res.CNPJ != "43.624.968/0001-00" || res.Mensalidade != "R$155,00" {
		t.Fatalf("parse inesperado: %+v", res)
	}
	if res.Modalidade != "LOCAÇÃO" {
		t.Fatalf("modalidade: %q", res.Modalidade)
	}
	res = parseClienteTela("Cliente ROFFIX, cnpj 43.624.968/0001-00, código 23125")
	if res.Nome != "" || res.CNPJ != "43.624.968/0001-00" {
		t.Fatalf("fallback cnpj: %+v", res)
	}
}
