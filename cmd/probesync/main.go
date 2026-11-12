// Command probesync roda um ciclo do sync de conta contra o banco real e
// imprime o resultado (ou o erro que o app engole). Uso: go run ./cmd/probesync
package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"rota-smart/internal/atualizacao"
	"rota-smart/internal/contasync"
	"rota-smart/internal/db"

	"github.com/danieljoos/wincred"
	"github.com/zalando/go-keyring"
	_ "modernc.org/sqlite"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	caminho := filepath.Join(os.Getenv("APPDATA"), "RotaSmart", "rotasmart.db")
	banco, err := sql.Open("sqlite", "file:"+caminho+"?_pragma=busy_timeout(5000)")
	if err != nil {
		fatal("abrir banco", err)
	}
	defer banco.Close()

	token, err := keyring.Get("RotaSuite", "token-pareamento")
	if err != nil || token == "" {
		// mesmo fallback do app consertado: alvo legado sem ":usuário"
		if cred, errC := wincred.GetGenericCredential("RotaSuite"); errC == nil && cred.UserName == "token-pareamento" {
			token = textoDoBlob(cred.CredentialBlob)
		}
	}
	if token == "" {
		fatal("token", fmt.Errorf("vazio — conta não pareada neste PC"))
	}
	fmt.Println("token: ok (", len(token), "chars )")

	m := &contasync.Motor{
		DB: banco,
		API: &contasync.HTTP{
			Base:   contasync.URLPadrao,
			Client: nil,
			Diag:   "probe v" + atualizacao.VersaoApp + " " + contasync.Diagnostico(context.Background(), banco),
		},
		Token:   func() (string, error) { return token, nil },
		Agora:   time.Now,
		Reparar: db.RepararGatilhosSync,
	}
	// dianteira: testa Buscar direto para mostrar o erro que o Ciclo engole
	ctxBusca, cancelBusca := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancelBusca()
	api := &contasync.HTTP{Base: contasync.URLPadrao}
	if _, err := api.Buscar(ctxBusca, token, "2026-11-09T22:01:54.981951507Z"); err != nil {
		fmt.Printf("buscar direto: ERRO %T: %v\n", err, err)
	} else {
		fmt.Println("buscar direto: ok")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	rel, err := m.Ciclo(ctx)
	if err != nil {
		fatal("ciclo", err)
	}
	fmt.Printf("ciclo: estado=%s aplicou=%d pendentes=%d\n", rel.Estado, rel.Aplicou, rel.Pendentes)

	var n int
	_ = banco.QueryRow(`SELECT count(*) FROM tickets`).Scan(&n)
	fmt.Println("tickets no banco agora:", n)
	rows, err := banco.Query(`SELECT chave, valor FROM sync_meta`)
	if err == nil {
		for rows.Next() {
			var k, v string
			_ = rows.Scan(&k, &v)
			fmt.Println("meta:", k, "=", v)
		}
		rows.Close()
	}
}

func fatal(operacao string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", operacao, err)
	os.Exit(1)
}

func textoDoBlob(b []byte) string {
	if bytes.IndexByte(b, 0) >= 0 {
		units := make([]uint16, len(b)/2)
		for i := range units {
			units[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
		}
		return strings.TrimRight(string(utf16.Decode(units)), "\x00 \t\r\n")
	}
	return strings.TrimSpace(string(b))
}
