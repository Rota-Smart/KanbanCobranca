package params

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestExtraiTexto(t *testing.T) {
	casos := []struct {
		nome                 string
		conteudo             []byte
		querConter, querErro string
	}{
		{"regras.txt", []byte("entrada mínima 20%"), "entrada mínima", ""},
		{"regras.md", []byte("# Regras\n12x máximo"), "12x máximo", ""},
		{"regras.exe", []byte("x"), "", "formato não suportado"},
	}
	for _, c := range casos {
		txt, err := ExtraiTexto(c.nome, c.conteudo)
		if c.querErro != "" {
			if err == nil || !strings.Contains(err.Error(), c.querErro) {
				t.Errorf("%s: esperava erro %q, veio %v", c.nome, c.querErro, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: erro inesperado: %v", c.nome, err)
			continue
		}
		if !strings.Contains(txt, c.querConter) {
			t.Errorf("%s: texto sem %q: %q", c.nome, c.querConter, txt)
		}
	}
}

func TestExtraiTextoDocx(t *testing.T) {
	xml := `<?xml version="1.0"?><w:document xmlns:w="w"><w:body>` +
		`<w:p><w:r><w:t>Entrada mínima de 20%</w:r></w:p>` +
		`<w:p><w:r><w:t>Máximo de &amp; até 12x</w:r></w:p>` +
		`</w:body></w:document>`
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte(xml))
	zw.Close()

	txt, err := ExtraiTexto("parametros.docx", buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(txt, "Entrada mínima de 20%") || !strings.Contains(txt, "Máximo de & até 12x") {
		t.Errorf("docx extraído errado: %q", txt)
	}
	if strings.Contains(txt, "<") {
		t.Errorf("docx com tags residuais: %q", txt)
	}
}

func TestExtraiTextoPdfSemTexto(t *testing.T) {
	// Conteúdo arbitrário: não é um PDF válido — espera erro claro.
	_, err := ExtraiTexto("x.pdf", []byte("não é pdf"))
	if err == nil {
		t.Fatal("esperava erro para pdf inválido")
	}
}
