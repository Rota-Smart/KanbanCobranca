// Command probe imprime o que o Shift+F2 capturaria da janela em foco.
// Uso: com o Chrome na página alvo, rode `go run ./cmd/probe`.
package main

import (
	"fmt"

	"rota-smart/internal/tela"
)

func main() {
	c := tela.Capturar()
	fmt.Printf("processo=%q\n titulo=%q\n url=%q\n clipboardLen=%d\n", c.Processo, c.Titulo, c.URL, len(c.Clipboard))
}
