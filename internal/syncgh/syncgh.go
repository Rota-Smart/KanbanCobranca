// Package syncgh sobe/baixa o banco do Kanban num repositório privado do
// GitHub usando a REST API de conteúdo — não depende do git instalado.
package syncgh

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const apiBase = "https://api.github.com"

// tamanhoMaximo é o limite prudente para a API de conteúdo (o arquivo viaja
// em base64 dentro de um JSON). Bancos bem acima disso já não são o caso de
// uso do Kanban.
const tamanhoMaximo = 90 << 20

// Info descreve o arquivo remoto do banco.
type Info struct {
	Existe  bool
	SHA     string
	Tamanho int64
	Quando  time.Time // data do último commit que tocou o arquivo
}

// Cliente fala com um único arquivo de banco em um único repositório.
type Cliente struct {
	Token   string // PAT clássico com escopo repo
	Repo    string // "dono/repo"
	Caminho string // caminho do arquivo dentro do repo
	HTTP    *http.Client
}

// Novo monta um cliente; caminho vazio vira "rotasmart.db".
func Novo(token, repo, caminho string) *Cliente {
	if caminho == "" {
		caminho = "rotasmart.db"
	}
	return &Cliente{Token: token, Repo: repo, Caminho: caminho, HTTP: &http.Client{Timeout: 5 * time.Minute}}
}

func (c *Cliente) valido() error {
	if c.Token == "" {
		return fmt.Errorf("token do GitHub não configurado")
	}
	if c.Repo == "" {
		return fmt.Errorf("repositório não configurado")
	}
	return nil
}

// erroHTTP guarda o status para o chamador distinguir 404 de falha real.
type erroHTTP struct {
	status int
	corpo  string
}

func (e *erroHTTP) Error() string { return fmt.Sprintf("github %d: %s", e.status, e.corpo) }

func (c *Cliente) do(ctx context.Context, metodo, url string, corpo []byte, aceitar string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, metodo, url, bytes.NewReader(corpo))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", aceitar)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if corpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, &erroHTTP{status: resp.StatusCode, corpo: string(b)}
	}
	return resp, nil
}

// Info lê os metadados atuais do arquivo remoto (se ele existir).
func (c *Cliente) Info(ctx context.Context) (Info, error) {
	var info Info
	if err := c.valido(); err != nil {
		return info, err
	}

	// Último commit que tocou o arquivo: dá SHA e a data da versão remota.
	url := fmt.Sprintf("%s/repos/%s/commits?path=%s&per_page=1", apiBase, c.Repo, c.Caminho)
	resp, err := c.do(ctx, http.MethodGet, url, nil, "application/vnd.github+json")
	if err != nil {
		if he, ok := err.(*erroHTTP); ok && (he.status == http.StatusNotFound || he.status == http.StatusConflict) {
			return info, nil // arquivo nunca enviado (ou repo vazio: 409)
		}
		return info, err
	}
	var commits []struct {
		SHA    string `json:"sha"`
		Commit struct {
			Committer struct {
				Date time.Time `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil {
		resp.Body.Close()
		return info, err
	}
	resp.Body.Close()
	if len(commits) == 0 {
		return info, nil
	}
	info.Existe = true
	info.SHA = commits[0].SHA
	info.Quando = commits[0].Commit.Committer.Date
	return info, nil
}

// Baixar grava o arquivo remoto em destPath.
func (c *Cliente) Baixar(ctx context.Context, destPath string) error {
	if err := c.valido(); err != nil {
		return err
	}
	url := fmt.Sprintf("%s/repos/%s/contents/%s", apiBase, c.Repo, c.Caminho)
	resp, err := c.do(ctx, http.MethodGet, url, nil, "application/vnd.github.raw")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, resp.Body)
	cerr := f.Close()
	if err != nil {
		os.Remove(destPath)
		return err
	}
	if cerr != nil {
		return cerr
	}
	if n > tamanhoMaximo {
		os.Remove(destPath)
		return fmt.Errorf("arquivo remoto grande demais (%d bytes)", n)
	}
	return os.Chmod(destPath, 0o600)
}

// Enviar sobe o arquivo local, criando ou atualizando o blob remoto.
func (c *Cliente) Enviar(ctx context.Context, localPath, mensagem string) error {
	if err := c.valido(); err != nil {
		return err
	}
	conteudo, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}
	if len(conteudo) > tamanhoMaximo {
		return fmt.Errorf("banco com %d bytes excede o limite de %d", len(conteudo), tamanhoMaximo)
	}

	// SHA do blob atual, se existir (necessário para atualizar em vez de criar).
	var sha string
	if info, err := c.Info(ctx); err == nil && info.Existe {
		sha = info.SHA
	}

	payload, err := json.Marshal(map[string]string{
		"message":  mensagem,
		"content":  base64.StdEncoding.EncodeToString(conteudo),
		"sha":      sha,
		"branch":   "main",
	})
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/repos/%s/contents/%s", apiBase, c.Repo, c.Caminho)
	resp, err := c.do(ctx, http.MethodPut, url, payload, "application/vnd.github+json")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
