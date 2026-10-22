// Package params extrai texto de arquivos de parâmetros de negociação
// (txt, docx e pdf com texto selecionável) para alimentar as propostas.
package params

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
)

// MaxArquivo limita o tamanho aceito (8 MB) para não travar a UI.
const MaxArquivo = 8 << 20

// LerEExtrair lê o arquivo do caminho e extrai o texto conforme a extensão.
func LerEExtrair(caminho string) (string, error) {
	conteudo, err := os.ReadFile(caminho)
	if err != nil {
		return "", fmt.Errorf("não foi possível ler o arquivo: %w", err)
	}
	nome := caminho
	if i := strings.LastIndexAny(caminho, `\/`); i >= 0 {
		nome = caminho[i+1:]
	}
	txt, err := ExtraiTexto(nome, conteudo)
	if err != nil {
		return "", err
	}
	if txt == "" {
		return "", fmt.Errorf("arquivo sem texto extraível")
	}
	return txt, nil
}

// ExtraiTexto devolve o texto do arquivo conforme a extensão.
func ExtraiTexto(nomeArquivo string, conteudo []byte) (string, error) {
	if len(conteudo) > MaxArquivo {
		return "", fmt.Errorf("arquivo maior que %d MB", MaxArquivo/(1<<20))
	}
	switch strings.ToLower(filepath.Ext(nomeArquivo)) {
	case ".txt", ".md", ".csv", "":
		return strings.TrimSpace(string(conteudo)), nil
	case ".docx":
		return docxTexto(conteudo)
	case ".pdf":
		return pdfTexto(conteudo)
	default:
		return "", fmt.Errorf("formato não suportado: %s (use txt, docx ou pdf)", filepath.Ext(nomeArquivo))
	}
}

// docxTexto abre o .docx (zip) e concatena os parágrafos de word/document.xml.
func docxTexto(conteudo []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(conteudo), int64(len(conteudo)))
	if err != nil {
		return "", fmt.Errorf("docx inválido: %w", err)
	}
	var doc *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			doc = f
			break
		}
	}
	if doc == nil {
		return "", fmt.Errorf("docx sem word/document.xml")
	}
	rc, err := doc.Open()
	if err != nil {
		return "", err
	}
	xml, err := io.ReadAll(io.LimitReader(rc, MaxArquivo))
	rc.Close()
	if err != nil {
		return "", err
	}
	return xmlParaTexto(string(xml)), nil
}

var (
	reParagrafo = regexp.MustCompile(`(?i)</w:p>`)
	reTab       = regexp.MustCompile(`(?i)<w:tab[^>]*/>`)
	reTag       = regexp.MustCompile(`<[^>]+>`)
	reEspacos   = regexp.MustCompile(`[ \t]+`)
)

// xmlParaTexto converte o document.xml em texto: cada `</w:p>` vira quebra
// de linha e as demais tags são removidas.
func xmlParaTexto(s string) string {
	s = reParagrafo.ReplaceAllString(s, "\n")
	s = reTab.ReplaceAllString(s, "\t")
	s = reTag.ReplaceAllString(s, "")
	s = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'").Replace(s)
	linhas := strings.Split(s, "\n")
	for i, l := range linhas {
		linhas[i] = strings.TrimSpace(reEspacos.ReplaceAllString(l, " "))
	}
	return strings.TrimSpace(strings.Join(linhas, "\n"))
}

// pdfTexto extrai texto selecionável; PDF escaneado (imagem) devolve erro claro.
func pdfTexto(conteudo []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(conteudo), int64(len(conteudo)))
	if err != nil {
		return "", fmt.Errorf("pdf inválido: %w", err)
	}
	var b strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		txt, err := p.GetPlainText(nil)
		if err != nil {
			continue
		}
		b.WriteString(txt)
		b.WriteString("\n")
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", fmt.Errorf("pdf sem texto selecionável (provavelmente escaneado) — cole os parâmetros como texto")
	}
	return out, nil
}
