// Command probe360 consulta (somente leitura) o status dos tickets vinculados
// aos cards em Cobrança, usando o cookie salvo no banco do usuário.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"rota-smart/internal/rota360"

	_ "modernc.org/sqlite"
)

func main() {
	home, _ := os.UserConfigDir()
	dbp := filepath.Join(home, "RotaSmart", "rotasmart.db")
	dbr, err := sql.Open("sqlite", dbp+"?mode=ro")
	if err != nil {
		fmt.Println("abrir db:", err)
		os.Exit(1)
	}
	var bruto string
	if err := dbr.QueryRow(`SELECT valor FROM config WHERE chave='rota360_cookie'`).Scan(&bruto); err != nil {
		fmt.Println("cookie:", err)
		os.Exit(1)
	}
	c, err := rota360.ParseCookieColado(bruto)
	if err != nil {
		fmt.Println("parse cookie:", err)
		os.Exit(1)
	}
	if cd, err := rota360.DetectaCookieCDP(); err == nil {
		fmt.Println("usando cookie do CDP (perfil do app)")
		c = cd
	} else {
		fmt.Println("CDP indisponível:", err)
	}
	cli := rota360.NovoCliente(c)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rows, err := dbr.Query(`SELECT v.ticket360_id, v.ticket360_numero, t.id, t.nome_cliente FROM tickets_vinculos v JOIN tickets t ON t.id=v.ticket_id ORDER BY t.id`)
	if err != nil {
		fmt.Println("vínculos:", err)
		os.Exit(1)
	}
	type v struct{ uuid, num, col, nome string }
	var vs []v
	for rows.Next() {
		var id int64
		var u, n, col, nome string
		rows.Scan(&u, &n, &id, &nome)
		_ = id
		vs = append(vs, v{u, n, col, nome})
	}
	// segunda passada p/ coluna
	for _, x := range vs {
		d, err := cli.DetalheTicket(ctx, x.uuid)
		if err != nil {
			fmt.Printf("card %s (%s) #%s → ERRO: %v\n", x.col, x.nome, x.num, err)
			continue
		}
		fmt.Printf("#%s (%s) → status=%q codservice=%q\n", x.num, x.nome, d.Status, d.CodService)
	}
}
