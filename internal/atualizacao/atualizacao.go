// Package atualizacao implementa o auto-update do app: a versão publicada
// vive em um repositório GitHub (branch de release) com dois arquivos na raiz:
//   - versao.txt      (ex.: "1.1.0")
//   - rotasuite.exe   (binário novo; aceita também o nome antigo
//     rota-smart.exe enquanto existirem instalações antigas)
//
// A URL-base é configurável (padrão: branch release de Rota-Smart/KanbanCobranca).
package atualizacao

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// VersaoApp é a versão deste binário; bump a cada release.
const VersaoApp = "1.6.0"

// BasePadrao é a fonte de atualização padrão (branch release do repo).
const BasePadrao = "https://raw.githubusercontent.com/Rota-Smart/KanbanCobranca/release"

const maxExe = 96 << 20 // 96 MB (exe atual ~79 MB com o modelo Vosk embutido)

// Cliente consulta a fonte de atualização.
type Cliente struct {
	Base string
	HTTP *http.Client
}

func NovoCliente(base string) *Cliente {
	if strings.TrimSpace(base) == "" {
		base = BasePadrao
	}
	return &Cliente{Base: strings.TrimRight(base, "/"), HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// get baixa um arquivo da fonte com limite de tamanho.
func (c *Cliente) get(ctx context.Context, arquivo string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/"+arquivo, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sem acesso à fonte de atualização: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fonte de atualização respondeu HTTP %d para %s", resp.StatusCode, arquivo)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxExe))
}

// Info descreve o resultado da verificação.
type Info struct {
	VersaoAtual string `json:"versaoAtual"`
	VersaoNova  string `json:"versaoNova"`
	TemNova     bool   `json:"temNova"`
}

// Verificar compara a versão local com a publicada.
func (c *Cliente) Verificar(ctx context.Context) (Info, error) {
	raw, err := c.get(ctx, "versao.txt")
	if err != nil {
		return Info{}, err
	}
	nova := strings.TrimSpace(string(raw))
	info := Info{VersaoAtual: VersaoApp, VersaoNova: nova}
	info.TemNova = nova != "" && nova != VersaoApp
	return info, nil
}

// Baixar grava o exe novo em destino (caminho completo do arquivo).
func (c *Cliente) Baixar(ctx context.Context, destino string) error {
	return c.BaixarProgresso(ctx, destino, nil)
}

// BaixarProgresso baixa o exe novo em streaming (sem carregar os ~79 MB na
// memória) e chama fn a cada pedaço com (baixados, total); total vem do
// Content-Length e pode ser 0 se o servidor não informar.
func (c *Cliente) BaixarProgresso(ctx context.Context, destino string, fn func(baixados, total int64)) error {
	baixar := func(arquivo string) (io.ReadCloser, int64, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/"+arquivo, nil)
		if err != nil {
			return nil, 0, err
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, 0, fmt.Errorf("sem acesso à fonte de atualização: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, 0, fmt.Errorf("fonte de atualização respondeu HTTP %d para %s", resp.StatusCode, arquivo)
		}
		return resp.Body, resp.ContentLength, nil
	}

	body, total, err := baixar("rotasuite.exe")
	if err != nil {
		// Compatibilidade: releases antigas publicavam só o nome antigo.
		body, total, err = baixar("rota-smart.exe")
		if err != nil {
			return err
		}
	}
	defer body.Close()

	tmp := destino + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	var baixados int64
	buf := make([]byte, 256<<10)
	for {
		n, errRead := body.Read(buf)
		if n > 0 {
			if _, errWrite := f.Write(buf[:n]); errWrite != nil {
				f.Close()
				os.Remove(tmp)
				return errWrite
			}
			baixados += int64(n)
			if fn != nil {
				fn(baixados, total)
			}
		}
		if errRead == io.EOF {
			break
		}
		if errRead != nil {
			f.Close()
			os.Remove(tmp)
			return errRead
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	st, err := os.Stat(tmp)
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if st.Size() < 1<<20 {
		os.Remove(tmp)
		return fmt.Errorf("arquivo baixado pequeno demais (%d bytes) — release inválida", st.Size())
	}
	return os.Rename(tmp, destino)
}

// Instalar troca o executável em execução pelo arquivo novo e reinicia o app.
// No Windows não é possível sobrescrever o exe rodando, mas RENOMEAR é
// permitido: renomeia o atual para .old, move o novo para o lugar e
// relança o processo.
func Instalar(exeAtual, exeNovo string) error {
	velho := exeAtual + ".old"
	_ = os.Remove(velho)
	if err := os.Rename(exeAtual, velho); err != nil {
		return fmt.Errorf("renomear executável atual: %w", err)
	}
	if err := os.Rename(exeNovo, exeAtual); err != nil {
		// desfaz para não deixar o app sem binário
		_ = os.Rename(velho, exeAtual)
		return fmt.Errorf("instalar novo executável: %w", err)
	}
	return nil
}

// LimparVelho apaga o backup .old de uma atualização anterior (no startup).
func LimparVelho(exeAtual string) {
	_ = os.Remove(exeAtual + ".old")
}

// CaminhoExe devolve o caminho do executável em execução.
func CaminhoExe() (string, error) {
	return os.Executable()
}

// DirExe devolve a pasta do executável.
func DirExe() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(p), nil
}
