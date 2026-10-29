// Command probefat (somente leitura) busca o financeiro do 360 de um
// código service e imprime as faturas em aberto cruas, incluindo categoria.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"rota-smart/internal/rota360"

	_ "modernc.org/sqlite"
)

func main() {
	cod := "14535"
	if len(os.Args) > 1 {
		cod = os.Args[1]
	}
	home, _ := os.UserConfigDir()
	dbp := filepath.Join(home, "RotaSmart", "rotasmart.db")
	dbr, err := sql.Open("sqlite", dbp+"?mode=ro")
	if err != nil {
		fmt.Println("abrir db:", err)
		os.Exit(1)
	}
	var cookie *rota360.Cookie
	if cd, err := rota360.DetectaCookieCDP(); err == nil {
		cookie = cd
		fmt.Println("usando cookie do CDP (perfil do app)")
	} else {
		var bruto string
		if err := dbr.QueryRow(`SELECT valor FROM config WHERE chave='rota360_cookie'`).Scan(&bruto); err != nil {
			fmt.Println("cookie:", err)
			os.Exit(1)
		}
		cookie, err = rota360.ParseCookieColado(bruto)
		if err != nil {
			fmt.Println("parse cookie:", err)
			os.Exit(1)
		}
		fmt.Println("usando cookie salvo")
	}
	cli := rota360.NovoCliente(cookie)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	fin, err := cli.FinanceiroCliente(ctx, cod)
	if err != nil {
		fmt.Println("financeiro:", err)
		os.Exit(1)
	}
	for _, f := range fin.Sources.InsideService.Data.FaturasAbertas {
		b, _ := json.Marshal(f)
		fmt.Println(string(b))
	}
}
