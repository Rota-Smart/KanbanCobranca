// Command probe imprime o que o Shift+F2 capturaria da janela em foco (e,
// com -texto, o que o Shift+F5 leria via UIA). Uso: com a janela alvo em
// foco, rode `go run ./cmd/probe`.
package main

import (
	"flag"
	"fmt"

	"rota-smart/internal/tela"
)

func main() {
	texto := flag.Bool("texto", false, "também lê o texto da janela (Shift+F5)")
	flag.Parse()
	c := tela.Capturar()
	fmt.Printf("processo=%q\n titulo=%q\n url=%q\n clipboardLen=%d\n", c.Processo, c.Titulo, c.URL, len(c.Clipboard))
	if *texto {
		t := tela.CapturaTextoDaTela()
		fmt.Printf("--- texto (%d runes) ---\n%s\n", len([]rune(t.Texto)), t.Texto)
	}
}
