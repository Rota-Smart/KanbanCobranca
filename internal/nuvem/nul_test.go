package nuvem

import "testing"

func TestConverterTiraNulo(t *testing.T) {
	if got := converter("faturas_json", "a\x00b"); got != "ab" {
		t.Fatalf("string: %q", got)
	}
	if got := converter("faturas_json", []byte("x\x00y")); got != "xy" {
		t.Fatalf("bytes: %q", got)
	}
	if got := converter("ativa", int64(1)); got != true {
		t.Fatalf("bool: %v", got)
	}
}
