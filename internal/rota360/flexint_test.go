package rota360

import (
	"encoding/json"
	"testing"
)

func TestFlexIntAceitaFloatEInteiro(t *testing.T) {
	var d struct {
		Q FlexInt `json:"quantidadeAtual"`
	}
	casos := []struct {
		in   string
		quer FlexInt
	}{
		{`{"quantidadeAtual": 8.400000005960464}`, 8},
		{`{"quantidadeAtual": 3}`, 3},
		{`{"quantidadeAtual": "12.9"}`, 12},
		{`{"quantidadeAtual": null}`, 0},
	}
	for _, c := range casos {
		if err := json.Unmarshal([]byte(c.in), &d); err != nil {
			t.Fatalf("unmarshal %s: %v", c.in, err)
		}
		if d.Q != c.quer {
			t.Errorf("%s: veio %d, queria %d", c.in, d.Q, c.quer)
		}
	}
}
