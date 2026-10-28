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
const VersaoApp = "1.21.1"

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

// Baixar grava o exe novo em destino (caminho completo do arquivo). A versão
// (ex.: "1.10.0") é usada para montar o nome do arquivo com versão na URL —
// o raw.githubusercontent.com faz cache por URL e já serviu exe ATRASADO no
// cache (versao.txt fresco + rotasuite.exe velho = atualização instalando
// versão antiga); nome com versão é URL nova a cada release, nunca stale.
func (c *Cliente) Baixar(ctx context.Context, destino, versao string) error {
	return c.BaixarProgresso(ctx, destino, versao, nil)
}

// BaixarProgresso baixa o exe novo em streaming (sem carregar os ~79 MB na
// memória) e chama fn a cada pedaço com (baixados, total); total vem do
// Content-Length e pode ser 0 se o servidor não informar (neste caso fn
// também é chamado, com total=0, só com o volume baixado).
//
// Conexões com o raw.githubusercontent.com podem cair no meio dos ~79 MB;
// tenta até 3 vezes (recomeçando do zero) antes de desistir.
func (c *Cliente) BaixarProgresso(ctx context.Context, destino, versao string, fn func(baixados, total int64)) error {
	var ultimoErr error
	for tentativa := 1; tentativa <= 3; tentativa++ {
		if tentativa > 1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
		err := c.baixarUmaVez(ctx, destino, versao, fn)
		if err == nil {
			return nil
		}
		ultimoErr = err
		// destino já virou o arquivo final (sucesso parcial não existe aqui);
		// remove sobras do .tmp para a próxima tentativa começar limpa.
		_ = os.Remove(destino + ".tmp")
	}
	return ultimoErr
}

func (c *Cliente) baixarUmaVez(ctx context.Context, destino, versao string, fn func(baixados, total int64)) error {
	// baixar abre o corpo de um arquivo da fonte (404 vira erro).
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

	// Nomes candidatos, do mais específico ao mais antigo: o com versão na
	// URL (imune a cache stale do CDN) e os legados, pra releases antigas.
	candidatos := []string{"rotasuite.exe", "rota-smart.exe"}
	if v := strings.Map(func(r rune) rune {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '.', r == '-', r == '_':
			return r
		}
		return -1
	}, strings.TrimSpace(versao)); v != "" {
		candidatos = append([]string{"rotasuite-" + v + ".exe"}, candidatos...)
	}

	var body io.ReadCloser
	var total int64
	var err error
	for _, arquivo := range candidatos {
		body, total, err = baixar(arquivo)
		if err == nil {
			break
		}
	}
	if err != nil {
		return err
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
	if total > 0 && st.Size() != total {
		os.Remove(tmp)
		return fmt.Errorf("download incompleto: %d de %d bytes", st.Size(), total)
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
// relança o processo. O antivírus (Kaspersky, Defender) costuma segurar o exe
// recém-baixado/encerrado por alguns segundos, então os renames tentam
// algumas vezes com pausa antes de falhar de verdade.
func Instalar(exeAtual, exeNovo string) error {
	velho := exeAtual + ".old"
	_ = os.Remove(velho)
	if err := renomearComPaciencia(exeAtual, velho); err != nil {
		return fmt.Errorf("renomear executável atual: %w", err)
	}
	if err := renomearComPaciencia(exeNovo, exeAtual); err != nil {
		// desfaz para não deixar o app sem binário
		_ = os.Rename(velho, exeAtual)
		return fmt.Errorf("instalar novo executável: %w", err)
	}
	return nil
}

// renomearComPaciencia tenta o rename por até ~10s (antivírus/liberação do
// arquivo costumam resolver nesse intervalo).
func renomearComPaciencia(origem, destino string) error {
	var err error
	for i := 0; i < 10; i++ {
		if err = os.Rename(origem, destino); err == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return err
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
