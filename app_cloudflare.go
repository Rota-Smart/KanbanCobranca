package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"rota-smart/internal/service"
)

// Preço oficial do Workers AI: US$ 0,011 por 1.000 neurons. O plano free dá
// 10.000 neurons/dia; a estimativa usa uma rodada típica do assistente
// (prompt com system + ferramentas + histórico, resposta curta).
const (
	cfDolarPorMilNeurons  = 0.011
	cfNeuronsFreeDia      = 10000
	cfTokensEntradaTipica = 2500
	cfTokensSaidaTipica   = 300
)

// ModeloCloudflare é um modelo de texto do catálogo que aceita ferramentas
// (o assistente depende delas para mover cards etc.).
type ModeloCloudflare struct {
	Nome             string  `json:"nome"`
	Descricao        string  `json:"descricao"`
	EntradaUSDporM   float64 `json:"entradaUsdPorM"`
	SaidaUSDporM     float64 `json:"saidaUsdPorM"`
	NeuronsRodada    float64 `json:"neuronsRodada"`    // estimativa por resposta do assistente
	RespostasDiaFree int     `json:"respostasDiaFree"` // estimativa no plano free (0 = sem preço publicado)
}

func (a *App) cloudflareConta() string {
	if v := a.configGet(a.ctx, cfgCloudflareConta); v != "" {
		return v
	}
	return cloudflareContaPadrao()
}

func (a *App) cloudflareToken() string {
	if v := a.configGet(a.ctx, cfgCloudflareChave); v != "" {
		return v
	}
	return cloudflareTokenPadrao()
}

// cloudflareModeloAtual é o modelo efetivo (salvo ou o padrão).
func (a *App) cloudflareModeloAtual() string {
	if v := a.configGet(a.ctx, cfgCloudflareModelo); v != "" {
		return v
	}
	return service.CloudflareModeloPadrao
}

type cfCatalogo struct {
	Success bool `json:"success"`
	Errors  []struct {
		Message string `json:"message"`
	} `json:"errors"`
	Result []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Properties  []struct {
			ID    string          `json:"property_id"`
			Value json.RawMessage `json:"value"`
		} `json:"properties"`
	} `json:"result"`
}

// ListarModelosCloudflare busca no catálogo do Workers AI os modelos de texto
// que aceitam ferramentas, do mais econômico ao mais caro.
func (a *App) ListarModelosCloudflare() ([]ModeloCloudflare, error) {
	conta, token := a.cloudflareConta(), a.cloudflareToken()
	if conta == "" || token == "" {
		return nil, fmt.Errorf("preencha o Account ID e o token da Cloudflare primeiro")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/models/search?task=Text%%20Generation&per_page=200", conta)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var cat cfCatalogo
	if err := json.Unmarshal(raw, &cat); err != nil {
		return nil, fmt.Errorf("cloudflare: HTTP %d, resposta inválida", resp.StatusCode)
	}
	if !cat.Success {
		msg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if len(cat.Errors) > 0 {
			msg = cat.Errors[0].Message
		}
		return nil, fmt.Errorf("cloudflare: %s", msg)
	}
	return modelosComFerramentas(cat), nil
}

func modelosComFerramentas(cat cfCatalogo) []ModeloCloudflare {
	out := []ModeloCloudflare{}
	for _, m := range cat.Result {
		tools := false
		var ent, sai float64
		for _, p := range m.Properties {
			switch p.ID {
			case "function_calling":
				var s string
				var b bool
				if json.Unmarshal(p.Value, &s) == nil {
					tools = s == "true"
				} else if json.Unmarshal(p.Value, &b) == nil {
					tools = b
				}
			case "price":
				var precos []struct {
					Unit  string  `json:"unit"`
					Price float64 `json:"price"`
				}
				if json.Unmarshal(p.Value, &precos) == nil {
					for _, pr := range precos {
						switch {
						case strings.Contains(pr.Unit, "input"):
							ent = pr.Price
						case strings.Contains(pr.Unit, "output"):
							sai = pr.Price
						}
					}
				}
			}
		}
		if !tools {
			continue
		}
		mc := ModeloCloudflare{Nome: m.Name, Descricao: m.Description, EntradaUSDporM: ent, SaidaUSDporM: sai}
		if ent > 0 || sai > 0 {
			usd := ent*cfTokensEntradaTipica/1e6 + sai*cfTokensSaidaTipica/1e6
			mc.NeuronsRodada = usd / cfDolarPorMilNeurons * 1000
			if mc.NeuronsRodada > 0 {
				mc.RespostasDiaFree = int(cfNeuronsFreeDia / mc.NeuronsRodada)
			}
		}
		out = append(out, mc)
	}
	// Mais respostas no free primeiro; sem preço publicado vai pro fim.
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := out[i].RespostasDiaFree, out[j].RespostasDiaFree
		if (ri == 0) != (rj == 0) {
			return rj == 0
		}
		return ri > rj
	})
	return out
}
