package contasync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErroHTTP é uma resposta da API que não é falha de rede.
type ErroHTTP struct {
	Status   int
	Mensagem string
}

func (e *ErroHTTP) Error() string {
	if e.Mensagem != "" {
		return e.Mensagem
	}
	return fmt.Sprintf("resposta %d", e.Status)
}

// EhRede diz se vale a pena esperar e tentar de novo sem avisar a pessoa.
// Qualquer falha que não seja uma resposta HTTP da API conta como rede.
func EhRede(err error) bool {
	if err == nil {
		return false
	}
	var he *ErroHTTP
	if errors.As(err, &he) {
		return he.Status == http.StatusTooManyRequests || he.Status >= 500
	}
	return true
}

// EhNaoAutorizado: o token da conta não vale mais.
func EhNaoAutorizado(err error) bool {
	var he *ErroHTTP
	return errors.As(err, &he) && he.Status == http.StatusUnauthorized
}

// HTTP fala com a API do RotaSuite. Não envia senha do 360.
type HTTP struct {
	Base   string
	Client *http.Client
}

func (h *HTTP) cliente() *http.Client {
	if h.Client != nil {
		return h.Client
	}
	return &http.Client{Timeout: 2 * time.Minute}
}

func (h *HTTP) base() string {
	b := strings.TrimRight(h.Base, "/")
	if b == "" {
		b = URLPadrao
	}
	return b
}

// ContaPareada é o que o código de uso único devolve. O token não vai para o SQLite.
type ContaPareada struct {
	Token string
	Nome  string
	Email string
}

// Parear troca o código de uso único pelo token longo.
func Parear(ctx context.Context, base, codigo string) (ContaPareada, error) {
	codigo = strings.TrimSpace(codigo)
	if codigo == "" {
		return ContaPareada{}, errors.New("digite o código que aparece no site")
	}
	if len(codigo) > 64 {
		return ContaPareada{}, errors.New("esse código não parece válido")
	}
	h := &HTTP{Base: base}
	var resp struct {
		Token   string `json:"token"`
		Erro    string `json:"erro"`
		Usuario struct {
			Nome  string `json:"nome"`
			Email string `json:"email"`
		} `json:"usuario"`
	}
	status, err := h.chamar(ctx, http.MethodPost, "/auth/parear", "", map[string]string{"codigo": codigo}, &resp)
	if err != nil {
		if EhRede(err) {
			return ContaPareada{}, errors.New("Sem internet agora. O quadro continua neste computador; tente conectar de novo quando a rede voltar.")
		}
		return ContaPareada{}, err
	}
	if status == http.StatusNotFound {
		return ContaPareada{}, ErrPareamentoIndisponivel
	}
	if status == http.StatusUnauthorized || status == http.StatusGone || status == http.StatusBadRequest || status >= 400 {
		msg := resp.Erro
		if msg == "" || len(msg) > 180 {
			msg = "não foi possível conectar a conta"
		}
		return ContaPareada{}, errors.New(msg)
	}
	if strings.TrimSpace(resp.Token) == "" {
		return ContaPareada{}, errors.New("a nuvem não devolveu a conexão")
	}
	return ContaPareada{Token: resp.Token, Nome: resp.Usuario.Nome, Email: resp.Usuario.Email}, nil
}

func (h *HTTP) Enviar(ctx context.Context, token string, alts []Alteracao) ([]Resultado, error) {
	var resp struct {
		Resultados []Resultado `json:"resultados"`
		Erro       string      `json:"erro"`
	}
	status, err := h.chamar(ctx, http.MethodPost, "/sync", token, map[string]any{"alteracoes": alts}, &resp)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		return nil, &ErroHTTP{Status: status, Mensagem: "não autorizado"}
	}
	if status >= 400 {
		msg := resp.Erro
		if msg == "" {
			msg = "não foi possível enviar"
		}
		return nil, &ErroHTTP{Status: status, Mensagem: msg}
	}
	return resp.Resultados, nil
}

func (h *HTTP) Buscar(ctx context.Context, token, desde string) (Pacote, error) {
	caminho := "/sync?desde=" + url.QueryEscape(desde)
	var pacote Pacote
	var bruto struct {
		Agora      string      `json:"agora"`
		Alteracoes []Alteracao `json:"alteracoes"`
		Erro       string      `json:"erro"`
	}
	status, err := h.chamar(ctx, http.MethodGet, caminho, token, nil, &bruto)
	if err != nil {
		return pacote, err
	}
	if status == http.StatusUnauthorized {
		return pacote, &ErroHTTP{Status: status, Mensagem: "não autorizado"}
	}
	if status >= 400 {
		msg := bruto.Erro
		if msg == "" {
			msg = "não foi possível receber"
		}
		return pacote, &ErroHTTP{Status: status, Mensagem: msg}
	}
	pacote.Agora = bruto.Agora
	pacote.Alteracoes = bruto.Alteracoes
	return pacote, nil
}

func (h *HTTP) chamar(ctx context.Context, metodo, caminho, token string, corpo any, destino any) (int, error) {
	var leitor io.Reader
	if corpo != nil {
		b, err := json.Marshal(corpo)
		if err != nil {
			return 0, err
		}
		leitor = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, h.base()+caminho, leitor)
	if err != nil {
		return 0, err
	}
	if corpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := h.cliente().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	limite := io.LimitReader(resp.Body, 32<<20)
	b, err := io.ReadAll(limite)
	if err != nil {
		return resp.StatusCode, err
	}
	if destino != nil && len(bytes.TrimSpace(b)) > 0 && (b[0] == '{' || b[0] == '[') {
		_ = json.Unmarshal(b, destino)
	}
	return resp.StatusCode, nil
}
