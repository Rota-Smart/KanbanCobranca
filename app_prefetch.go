package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"rota-smart/internal/rota360"
	"rota-smart/internal/service"
)

// Pré-busca de contexto: antes de chamar a IA, o backend classifica a
// mensagem, identifica o alvo (cliente/ticket) e puxa os dados do 360 em
// UMA requisição (o dashboard360 devolve financeiro + faturas + OS ativas +
// produtos + Octadesk juntos — as "abas" do portal são só visual). A fatia
// pedida vira um bloco no system prompt e as tools de leitura correspondentes
// saem do request: o modelo só redige a resposta. Sessão expirada/ausente
// PARA aqui — a IA não é acionada sem os dados oficiais.

// minContagemAprendido: ferramenta chamada pelo modelo N vezes dentro da
// mesma classe passa a ser pré-coletada junto (a pré-busca aprende o que
// você costuma pedir em sequência).
const minContagemAprendido = 3

type prebusca struct {
	classe    service.ClasseConsulta
	bloco     string
	removidas []string
	cache     map[string]string // ferramenta removida → resultado pré-coletado
}

// opcoes converte para o que o service.ConversarCom consome.
func (p *prebusca) opcoes() *service.OpcoesConversa {
	if p == nil {
		return nil
	}
	return &service.OpcoesConversa{BlocoContexto: p.bloco, FerramentasRemovidas: p.removidas}
}

// prepararPrebusca roda o roteamento completo. Devolve (nil, nil) quando a
// mensagem não é consulta de dados (fluxo normal do agente). Erro de sessão
// (expirada/ausente) sobe para EnviarChat parar antes da IA; qualquer outra
// falha de rede é logada e a mensagem segue no fluxo antigo.
func (a *App) prepararPrebusca(ctx context.Context, msg string) (*prebusca, error) {
	classe := service.ClassificarConsulta(msg)
	if classe == service.ConsultaNenhuma {
		return nil, nil
	}

	c := a.rotaAtual()
	if c == nil {
		// O usuário pediu dados do 360 sem sessão: não adianta mandar a IA
		// adivinhar — para aqui com o aviso direto.
		return nil, rota360.ErrSemSessao
	}

	alvo := service.ExtrairAlvo(msg)
	var cod string
	switch alvo.Tipo {
	case "cod":
		cod = alvo.Valor
	case "nome":
		var err error
		cod, err = a.codDoNome(ctx, c, alvo.Valor)
		if err != nil {
			if errors.Is(err, rota360.ErrSessaoExpirada) {
				return nil, err
			}
			slog.Warn("pré-busca: busca de cliente falhou", "nome", alvo.Valor, "err", err)
			return nil, nil // sem alvo confiável, segue o fluxo antigo
		}
	case "ticket":
		return a.prebuscaTicket(ctx, c, alvo.Valor, classe)
	default:
		// Sem alvo explícito (follow-up tipo "e o financeiro?"): usa o último
		// ticket capturado pelo Shift+F2, se houver.
		cod = strings.TrimSpace(a.ultimoTicketTela().CodService)
		if cod == "" {
			return nil, nil
		}
	}

	// Aprendido: fatias que o modelo costuma pedir junto nesta classe entram
	// sem o usuário pedir de novo.
	ferramentas := map[string]bool{}
	for _, f := range service.FerramentasFatia(classe) {
		ferramentas[f] = true
	}
	if a.stats != nil {
		if tops, err := a.stats.TopFerramentas(ctx, string(classe), minContagemAprendido); err == nil {
			for _, f := range tops {
				if f == "financeiro_cliente" || f == "atendimentos_cliente" || f == "equipamentos_cliente" {
					ferramentas[f] = true
				}
			}
		}
	}

	// UMA requisição ao dashboard cobre as três fatias de cliente.
	var fin *rota360.Dashboard360
	var funil *rota360.OSFunil360
	var obs string
	precisaDash := ferramentas["financeiro_cliente"] || ferramentas["atendimentos_cliente"] || ferramentas["equipamentos_cliente"]
	if precisaDash {
		f, err := c.FinanceiroCliente(ctx, cod)
		if err != nil {
			if errors.Is(err, rota360.ErrSessaoExpirada) {
				return nil, err
			}
			slog.Warn("pré-busca: dashboard360 falhou", "cod", cod, "err", err)
			return nil, nil
		}
		fin = f
	}
	if ferramentas["atendimentos_cliente"] {
		// Histórico completo de O.S. (concluídas/canceladas) vem do os-funil;
		// as ativas já vieram no dashboard.
		if f, err := c.OSFunil(ctx, cod); err == nil {
			funil = f
		} else {
			slog.Warn("pré-busca: os-funil falhou", "cod", cod, "err", err)
		}
	}
	if ferramentas["equipamentos_cliente"] {
		if dc, err := c.DadosCliente(ctx, cod); err == nil {
			obs = strings.TrimSpace(dc.Observacoes)
		}
	}

	p := &prebusca{classe: classe, cache: map[string]string{}}
	var sb strings.Builder
	sb.WriteString("Contexto pré-coletado pelo app direto do 360 (dado oficial e atual — NÃO chame ferramentas para buscar de novo o que está aqui; use para responder):\n")
	if ferramentas["financeiro_cliente"] && fin != nil {
		txt := textoFinanceiro360(cod, fin)
		sb.WriteString("\n" + txt)
		p.cache["financeiro_cliente"] = txt
	}
	if ferramentas["atendimentos_cliente"] {
		txt := textoAtendimentos360(cod, funil, fin)
		sb.WriteString("\n" + txt)
		p.cache["atendimentos_cliente"] = txt
	}
	if ferramentas["equipamentos_cliente"] && fin != nil {
		txt := resumoEquipamentos(fin.Sources.Produtos.Data)
		if obs != "" {
			txt += "Observações do cadastro:\n" + limitarTexto(obs, 500) + "\n"
		}
		sb.WriteString("\n" + txt)
		p.cache["equipamentos_cliente"] = txt
	}
	p.bloco = strings.TrimRight(sb.String(), "\n")
	p.removidas = service.FerramentasRemovidas(classe, alvo.Tipo != "" || cod != "")
	if cod != "" {
		p.cache["buscar_cliente_360"] = fmt.Sprintf("Alvo já identificado pela pré-busca: cliente %s (código service %s). Não é preciso buscar de novo.", nomeDoCliente(fin, cod), cod)
	}
	if classe != service.ConsultaTicket {
		if cs, err := a.colunas.List(a.ctx); err == nil {
			p.cache["listar_colunas"] = "Colunas: " + strings.Join(cs, ", ")
		}
	}
	return p, nil
}

// prebuscaTicket cobre a classe ticket: detalhe do ticket do 360 em uma
// requisição (a fila é varrida para achar o uuid).
func (a *App) prebuscaTicket(ctx context.Context, c *rota360.Cliente, numero string, classe service.ClasseConsulta) (*prebusca, error) {
	item, err := a.resolverTicket360(ctx, numero)
	if err != nil {
		if errors.Is(err, rota360.ErrSessaoExpirada) {
			return nil, err
		}
		slog.Warn("pré-busca: ticket não localizado", "numero", numero, "err", err)
		return nil, nil
	}
	det, err := c.DetalheTicket(ctx, item.ID)
	if err != nil {
		if errors.Is(err, rota360.ErrSessaoExpirada) {
			return nil, err
		}
		slog.Warn("pré-busca: detalhe do ticket falhou", "numero", numero, "err", err)
		return nil, nil
	}
	autor := a.autorSessao360()
	if autor == "" {
		autor = det.ResponsavelNome
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Ticket #%s — %s [%s] | cliente %s (código %s) | assunto %s/%s | responsável %s\n\n",
		det.Numero, det.Titulo, det.Status, det.Solicitante, det.CodService, det.CategoriaNome, det.AssuntoNome, det.ResponsavelNome)
	sb.WriteString(resumoMensagensTicket(det.Mensagens, autor))

	p := &prebusca{classe: classe, cache: map[string]string{
		"detalhe_ticket_360": sb.String(),
	}}
	p.bloco = "Contexto pré-coletado pelo app direto do 360 (dado oficial e atual — NÃO chame ferramentas para buscar de novo o que está aqui; use para responder):\n\n" + sb.String()
	p.removidas = service.FerramentasRemovidas(classe, true)
	return p, nil
}

// codDoNome resolve um nome de cliente para código service: primeiro nos
// cards locais (sem rede); sem card, busca no 360 e usa o primeiro resultado.
func (a *App) codDoNome(ctx context.Context, c *rota360.Cliente, nome string) (string, error) {
	alvo := strings.ToLower(strings.TrimSpace(nome))
	if ts, err := a.tickets.List(a.ctx); err == nil {
		for _, t := range ts {
			tn := strings.ToLower(t.NomeCliente)
			if tn != "" && (strings.Contains(tn, alvo) || strings.Contains(alvo, tn)) && t.CodService != "" {
				return t.CodService, nil
			}
		}
	}
	res, err := c.BuscarClientes(ctx, nome, "todos")
	if err != nil {
		return "", err
	}
	if len(res) == 0 {
		return "", fmt.Errorf("nenhum cliente com o nome %q no 360", nome)
	}
	return strconv.FormatInt(res[0].CodCliente, 10), nil
}

// nomeDoCliente extrai o nome do dashboard, com fallback pro código.
func nomeDoCliente(fin *rota360.Dashboard360, cod string) string {
	if fin != nil {
		if n := strings.TrimSpace(fin.Sources.InsideService.Data.Dados.Nome); n != "" {
			return n
		}
	}
	return cod
}

// registrarStatsPrebusca grava as ferramentas que o modelo efetivamente
// chamou dentro da classe — o insumo do aprendizado. Ferramentas servidas do
// cache da pré-busca (as removidas) NÃO contam: chamá-las com o dado já em
// contexto é redundância, não preferência.
func (a *App) registrarStatsPrebusca(p *prebusca, acoes []service.AcaoDoAgente) {
	if p == nil || a.stats == nil || len(acoes) == 0 {
		return
	}
	removidas := map[string]bool{}
	for _, f := range p.removidas {
		removidas[f] = true
	}
	for _, ac := range acoes {
		if !removidas[ac.Ferramenta] {
			_ = a.stats.Incrementar(context.Background(), string(p.classe), ac.Ferramenta)
		}
	}
}

// cacheServir responde uma ferramenta removida a partir do que a pré-busca
// já coletou: zero HTTP, zero rodada extra.
func (a *App) cacheServir(ferramenta string) (string, bool) {
	a.muPrebusca.Lock()
	defer a.muPrebusca.Unlock()
	if a.cachePrebusca == nil {
		return "", false
	}
	v, ok := a.cachePrebusca[ferramenta]
	return v, ok
}
