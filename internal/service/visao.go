package service

// Reconhecimento de cliente na tela do Service (Access): a janela do Access
// não expõe texto via UI Automation, então o Shift+F3 tira um print e pede
// para o modelo de visão da Cloudflare (Workers AI) ler os campos. A IA da
// tela costuma errar um dígito do código, então o briefing manda o agente
// confirmar o código no 360 pelo nome/CNPJ antes de usar.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ModeloVisao é o modelo de visão do Workers AI usado para ler a tela do
// Service. Precisou de aceite de licença uma vez (POST {"prompt":"agree"}).
const ModeloVisao = "@cf/meta/llama-3.2-11b-vision-instruct"

// ClienteTela é o que a visão conseguiu ler da tela do Service.
type ClienteTela struct {
	Codigo      string `json:"codigo"`
	Nome        string `json:"nome"`
	CNPJ        string `json:"cnpj"`
	Mensalidade string `json:"mensalidade"`
	Modalidade  string `json:"modalidade"`
	Bruto       string `json:"bruto"` // resposta inteira, para o log
}

// regexCNPJ deixa passar o CNPJ com ou sem pontuação.
var regexCNPJ = regexp.MustCompile(`\d{2}\.?\d{3}\.?\d{3}/?\d{4}-?\d{2}`)

// regexDigitos tira a pontuação de números lidos da tela.
var regexDigitos = regexp.MustCompile(`\D`)

// promptVisao pede o formato mastigado que o parser espera. Pedidos longos
// custam tokens de resposta, não de imagem. O CNPJ só entra se estiver
// ROTULADO na tela (CGC/CPF:) — o modelo costua pegar telefone, inscrição
// estadual ou qualquer número grande e vestir de CNPJ.
const promptVisao = `Esta tela é de um sistema de monitoramento. Responda APENAS uma linha no formato ` +
	`CODIGO=<código do cliente> | NOME=<nome> | CNPJ=<somente o número após o rótulo CGC/CPF, se existir; senão vazio> | MENSALIDADE=<valor se visível> | MODALIDADE=<se visível>. ` +
	`Não transforme telefone, inscrição estadual ou código em CNPJ. Use exatamente o que está escrito na tela, sem inventar.`

// ClienteDaTela manda o print para o modelo de visão da Cloudflare e devolve
// os campos lidos. conta/chave vazios devolvem erro (sem fallback embutido
// aqui — quem chama decide). Imagem em JPEG já comprimido (o modelo aceita).
func ClienteDaTela(jpeg []byte, conta, chave string) (ClienteTela, error) {
	var res ClienteTela
	if conta == "" || chave == "" {
		return res, fmt.Errorf("sem credenciais da Cloudflare para o modelo de visão")
	}
	corpo, err := json.Marshal(map[string]any{
		"prompt":    promptVisao,
		"image":     jpeg,
		"max_tokens": 150,
	})
	if err != nil {
		return res, err
	}
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run/%s", conta, ModeloVisao)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(corpo))
	if err != nil {
		return res, err
	}
	req.Header.Set("Authorization", "Bearer "+chave)
	req.Header.Set("Content-Type", "application/json")
	cli := &http.Client{Timeout: 90 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return res, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return res, err
	}
	var envelope struct {
		Success bool `json:"success"`
		Errors  []struct{ Message string } `json:"errors"`
		Result  struct {
			Response string `json:"response"`
		} `json:"result"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		return res, fmt.Errorf("resposta ilegível da Cloudflare: %w", err)
	}
	if !envelope.Success {
		msg := "erro desconhecido"
		if len(envelope.Errors) > 0 {
			msg = envelope.Errors[0].Message
		}
		return res, fmt.Errorf("modelo de visão recusou: %s", msg)
	}
	res.Bruto = envelope.Result.Response
	res = parseClienteTela(res.Bruto)
	if res.Nome == "" {
		return res, fmt.Errorf("visão não achou o nome do cliente na resposta: %s", res.Bruto)
	}
	slog.Info("visão leu a tela do service", "codigo", res.Codigo, "nome", res.Nome, "cnpj", res.CNPJ != "")
	return res, nil
}

// parseClienteTela extrai os campos da resposta do modelo (uma linha de
// pares CHAVE=valor ou texto livre com os dados embutidos).
func parseClienteTela(bruto string) ClienteTela {
	res := ClienteTela{Bruto: bruto}
	for _, parte := range strings.Split(bruto, "|") {
		parte = strings.TrimSpace(parte)
		chave, valor, achou := strings.Cut(parte, "=")
		if !achou {
			continue
		}
		chave = strings.ToUpper(strings.TrimSpace(chave))
		valor = strings.TrimSpace(valor)
		switch {
		case strings.Contains(chave, "CODIGO"):
			res.Codigo = valor
		case strings.Contains(chave, "NOME"):
			res.Nome = valor
		case strings.Contains(chave, "CNPJ"):
			res.CNPJ = valor
		case strings.Contains(chave, "MENSALIDADE"):
			res.Mensalidade = valor
		case strings.Contains(chave, "MODALIDADE"):
			res.Modalidade = valor
		}
	}
	if res.CNPJ == "" {
		if c := regexCNPJ.FindString(bruto); c != "" {
			res.CNPJ = c
		}
	}
	// O modelo (e a busca de reserva no texto) erram o rótulo: telefone,
	// inscrição estadual e código viram "CNPJ". Só aceita número com
	// dígito verificador válido (CNPJ 14 ou CPF 11).
	if res.CNPJ != "" && !documentoValido(res.CNPJ) {
		slog.Info("cnpj lido da tela reprovado no dígito verificador", "lido", res.CNPJ)
		res.CNPJ = ""
	}
	return res
}

// documentoValido confere o dígito verificador de um CNPJ (14 dígitos) ou
// CPF (11 dígitos) com ou sem pontuação.
func documentoValido(s string) bool {
	d := regexDigitos.ReplaceAllString(s, "")
	switch len(d) {
	case 14:
		return digitoMod(d[:12], []int{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}) == d[12] &&
			digitoMod(d[:13], []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}) == d[13]
	case 11:
		return digitoMod(d[:9], []int{10, 9, 8, 7, 6, 5, 4, 3, 2}) == d[9] &&
			digitoMod(d[:10], []int{11, 10, 9, 8, 7, 6, 5, 4, 3, 2}) == d[10]
	}
	return false
}

// digitoMod calcula um dígito verificador módulo 11 (padrão Receita) sobre
// base com pesos e devolve o caractere esperado ('0'..'9').
func digitoMod(base string, pesos []int) byte {
	soma := 0
	for i, c := range []byte(base) {
		soma += int(c-'0') * pesos[i]
	}
	resto := soma % 11
	if resto < 2 {
		return '0'
	}
	return byte('0' + 11 - resto)
}
