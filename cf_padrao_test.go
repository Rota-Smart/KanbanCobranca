package main

import (
	"strings"
	"testing"
)

func TestCloudflarePadraoDesembaralha(t *testing.T) {
	if !strings.HasPrefix(cloudflareTokenPadrao(), "cfut_") || len(cloudflareContaPadrao()) != 32 {
		t.Fatalf("credenciais padrão corrompidas")
	}
}
