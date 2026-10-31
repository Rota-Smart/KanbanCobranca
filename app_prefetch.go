package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"rota-smart/internal/db"
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
	// Board é 100% banco local: independe de sessão do 360 e roda antes do
	// check de conexão.
	if classe == service.ConsultaBoard {
		return a.prebuscaBoard(msg), nil
	}
	// Ação (mover card, responder ticket, orçamento...): não pré-coleta
	// nada — o ganho é só o roteamento de tools: as que não servem para
	// essa operação saem do request inteiro (economia de TPM por rodada).
	if service.EhAcao(classe) {
		return &prebusca{classe: classe, cache: map[string]string{},
			removidas: service.FerramentasForaDaClasse(classe)}, nil
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
		// ticket capturado pelo Shift+F2, se houver — mas como CHUTE
		// declarado: o bloco vem rotulado e NADA sai do request, porque a
		// pergunta pode ser sobre outros clientes ou o board inteiro.
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
	cabecalho := "Contexto pré-coletado pelo app direto do 360 (dado oficial e atual — NÃO chame ferramentas para buscar de novo o que está aqui; use para responder):\n"
	if alvo.Tipo == "" {
		cabecalho = fmt.Sprintf("Contexto pré-coletado pelo app direto do 360, do ÚLTIMO ticket capturado (cliente código service %s). Se a pergunta for sobre OUTRO cliente ou sobre o quadro em geral, ignore este bloco e use as ferramentas:\n", cod)
	}
	sb.WriteString(cabecalho)
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
	if bloco := a.blocoCardsCliente(cod); bloco != "" {
		sb.WriteString("\n" + bloco)
	}
	p.bloco = strings.TrimRight(sb.String(), "\n")
	p.removidas = service.FerramentasRemovidas(classe, alvo.Tipo != "")
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
	if det.CodService != "" {
		if bloco := a.blocoCardsCliente(det.CodService); bloco != "" {
			sb.WriteString("\n" + bloco)
		}
	}

	p := &prebusca{classe: classe, cache: map[string]string{
		"detalhe_ticket_360": sb.String(),
	}}
	p.bloco = "Contexto pré-coletado pelo app direto do 360 (dado oficial e atual — NÃO chame ferramentas para buscar de novo o que está aqui; use para responder):\n\n" + sb.String()
	p.removidas = service.FerramentasRemovidas(classe, true)
	return p, nil
}

// respostaBoardSemIA responde um pedido de listagem direto do banco local,
// sem chave de IA: é o mesmo bloco da pré-busca, sem o cabeçalho dirigido ao
// modelo. Vazio quando a mensagem não é consulta de quadro.
func (a *App) respostaBoardSemIA(msg string) string {
	if service.ClassificarConsulta(msg) != service.ConsultaBoard {
		return ""
	}
	pre := a.prebuscaBoard(msg)
	if pre == nil {
		return ""
	}
	if i := strings.Index(pre.bloco, "\n"); i >= 0 {
		return strings.TrimRight(pre.bloco[i+1:], "\n")
	}
	return ""
}

// prebuscaBoard mastiga pedidos agregados sobre o quadro: lista os cards da
// coluna pedida (ou do quadro inteiro) direto do banco local e já anexa o
// que a IA ia perguntar de volta — renegociações e cobrança ativa de cada
// card. Zero rede, zero rodada extra de API: a ferramenta listar_tickets
// sai do request e é servida do cache.
func (a *App) prebuscaBoard(msg string) *prebusca {
	ts, err := a.tickets.List(a.ctx)
	if err != nil {
		slog.Warn("pré-busca board: listagem falhou", "err", err)
		return nil
	}
	colunas, _ := a.colunas.List(a.ctx)

	coluna := service.ExtrairColunaBoard(msg)
	var alvo string
	if coluna != "" {
		if casada, err := service.CasarColuna(coluna, colunas); err == nil {
			alvo = casada
		} else {
			slog.Warn("pré-busca board: coluna não casada", "citada", coluna)
		}
	}

	var sb strings.Builder
	if alvo != "" {
		fmt.Fprintf(&sb, "Cards da coluna %q pré-coletados do banco local (dado oficial — NÃO chame listar_tickets para repetir isto):\n", alvo)
	} else {
		sb.WriteString("Todos os cards do quadro pré-coletados do banco local (dado oficial — NÃO chame listar_tickets para repetir isto):\n")
	}
	n := 0
	for _, t := range ts {
		if alvo != "" && t.ColunaAtual != alvo {
			continue
		}
		n++
		fmt.Fprintf(&sb, "- card id %d | %s | coluna %q", t.ID, t.NomeCliente, t.ColunaAtual)
		if t.CodService != "" {
			fmt.Fprintf(&sb, " | cod %s", t.CodService)
		}
		if t.Ticket360Numero != "" {
			fmt.Fprintf(&sb, " | ticket 360 #%s", t.Ticket360Numero)
		}
		if reneg, err := a.clientes.ContaRenegociacoesTicket(a.ctx, t.ID); err == nil && reneg > 0 {
			fmt.Fprintf(&sb, " | %d renegociação(ões)", reneg)
		}
		if cob, err := a.cobrancas.GetAtiva(a.ctx, t.ID); err == nil && cob != nil {
			fmt.Fprintf(&sb, " | cobrança ativa: aberto R$ %.2f", float64(cob.TotalAbertoCents)/100)
		}
		sb.WriteString("\n")
	}
	if n == 0 {
		if alvo != "" {
			sb.WriteString("(nenhum card nesta coluna)\n")
		} else {
			sb.WriteString("(quadro vazio)\n")
		}
	}

	p := &prebusca{classe: service.ConsultaBoard, cache: map[string]string{}}
	p.bloco = strings.TrimRight(sb.String(), "\n")
	p.removidas = service.FerramentasRemovidas(service.ConsultaBoard, true)
	p.cache["listar_tickets"] = p.bloco
	return p
}

// blocoCardsCliente verifica no banco LOCAL (sem rede, antes da IA) se o
// cliente já tem card no Kanban e devolve o bloco de contexto correspondente.
// Vazio só se o banco falhar — a resposta "sem card" também é informação
// útil para o modelo.
func (a *App) blocoCardsCliente(cod string) string {
	ts, err := a.tickets.List(a.ctx)
	if err != nil {
		slog.Warn("pré-busca: listagem de cards falhou", "cod", cod, "err", err)
		return ""
	}
	var encontrados []db.Ticket
	for _, t := range ts {
		if t.CodService == cod {
			encontrados = append(encontrados, t)
		}
	}
	if len(encontrados) == 0 {
		return fmt.Sprintf("Cards no sistema: o cliente (código %s) NÃO tem card no Kanban local.", cod)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Cards no sistema (%d card(s) do cliente no Kanban local; card id é o identificador para mover_card/apagar_card/etc.):\n", len(encontrados))
	for _, t := range encontrados {
		detalhe := t.NomeCliente
		if t.Ticket360Numero != "" {
			detalhe += " | ticket 360 #" + t.Ticket360Numero
		}
		if t.Observacoes != "" {
			detalhe += " | obs: " + limitarTexto(t.Observacoes, 200)
		}
		fmt.Fprintf(&sb, "- card id %d | coluna %q | %s | atualizado em %s\n", t.ID, t.ColunaAtual, detalhe, t.UpdatedAt)
	}
	return strings.TrimRight(sb.String(), "\n")
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
