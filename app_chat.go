package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"rota-smart/internal/db"
	"rota-smart/internal/orcamento"
	"rota-smart/internal/rota360"
	"rota-smart/internal/service"
	"rota-smart/internal/tela"
)

// ---- Colunas dinâmicas (bindings) ----

// CriarColuna adiciona uma coluna nova ao fim do board.
func (a *App) CriarColuna(nome string) (string, error) {
	n, err := a.colunas.Add(a.ctx, nome)
	if err != nil {
		return "", err // mensagens de validação do repo vão direto ao usuário
	}
	return n, friendly("CriarColuna", err)
}

// RemoverColuna apaga uma coluna vazia do board.
func (a *App) RemoverColuna(nome string) error {
	if err := a.colunas.Remove(a.ctx, nome); err != nil {
		if err == db.ErrColunaNaoVazia {
			return err
		}
		return friendly("RemoverColuna", err)
	}
	return nil
}

// ColunasFixas não podem ser renomeadas: o app as identifica pelo nome
// para disparar automações (fluxo de cobrança, coluna de orçamento).
var ColunasFixas = []string{service.ColunaCobranca, ColunaOrcamento}

func colunaFixa(nome string) bool {
	for _, c := range ColunasFixas {
		if c == nome {
			return true
		}
	}
	return false
}

// RenomearColuna troca o nome de uma coluna e dos cards dela.
func (a *App) RenomearColuna(antigo, novo string) error {
	if colunaFixa(antigo) {
		return fmt.Errorf("a coluna %q participa de automações do app e não pode ser renomeada", antigo)
	}
	if err := a.colunas.Renomear(a.ctx, antigo, novo); err != nil {
		if strings.Contains(err.Error(), "coluna") || err == sql.ErrNoRows {
			return err
		}
		return friendly("RenomearColuna", err)
	}
	return nil
}

// ReordenarColunas grava a nova ordem do board (lista completa na ordem).
func (a *App) ReordenarColunas(nomes []string) error {
	if err := a.colunas.Reordenar(a.ctx, nomes); err != nil {
		if strings.Contains(err.Error(), "colunas") {
			return err
		}
		return friendly("ReordenarColunas", err)
	}
	return nil
}

// ---- Chat com o agente Groq ----

// ListarChats devolve as conversas salvas, do mais recente ao mais antigo.
func (a *App) ListarChats() ([]db.Chat, error) {
	if a.chats == nil {
		return []db.Chat{}, nil
	}
	if err := a.chats.LimparVazios(a.ctx); err != nil {
		return nil, friendly("ListarChats", err)
	}
	cs, err := a.chats.List(a.ctx)
	if err != nil {
		return nil, friendly("ListarChats", err)
	}
	if cs == nil {
		cs = []db.Chat{}
	}
	return cs, nil
}

// CriarChat abre uma conversa nova e devolve o registro criado.
func (a *App) CriarChat() (db.Chat, error) {
	if a.chats == nil {
		return db.Chat{}, fmt.Errorf("conversas indisponíveis")
	}
	id, err := a.chats.Create(a.ctx, "Nova conversa")
	if err != nil {
		return db.Chat{}, friendly("CriarChat", err)
	}
	return db.Chat{ID: id, Titulo: "Nova conversa"}, nil
}

// ApagarChat remove a conversa e o histórico dela.
func (a *App) ApagarChat(id int64) error {
	if err := a.chats.Delete(a.ctx, id); err != nil {
		return friendly("ApagarChat", err)
	}
	return nil
}

// MensagemDoChat é uma linha do histórico já com as ações convertidas.
type MensagemDoChat struct {
	ID       int64                  `json:"id"`
	Papel    string                 `json:"papel"`
	Conteudo string                 `json:"conteudo"`
	Acoes    []service.AcaoDoAgente `json:"acoes"`
}

// MensagensDoChat devolve o histórico persistido de uma conversa.
func (a *App) MensagensDoChat(id int64) ([]MensagemDoChat, error) {
	ms, err := a.chats.Mensagens(a.ctx, id)
	if err != nil {
		return nil, friendly("MensagensDoChat", err)
	}
	out := make([]MensagemDoChat, 0, len(ms))
	for _, m := range ms {
		msg := MensagemDoChat{ID: m.ID, Papel: m.Papel, Conteudo: m.Conteudo, Acoes: []service.AcaoDoAgente{}}
		if m.AcoesJSON != "" {
			if err := json.Unmarshal([]byte(m.AcoesJSON), &msg.Acoes); err != nil {
				return nil, friendly("MensagensDoChat", err)
			}
		}
		out = append(out, msg)
	}
	return out, nil
}

// EnviarChat manda a mensagem para o agente Groq dentro da conversa chatID:
// o histórico vem do banco (persistido), e user+resposta são gravados nele.
// O agente pode executar ferramentas: buscar clientes no 360, adicionar
// cards, criar colunas, mover cards, consultar financeiro etc.
func (a *App) EnviarChat(chatID int64, mensagem string) (service.ChatResultado, error) {
	if a.chats == nil {
		return service.ChatResultado{}, fmt.Errorf("conversas indisponíveis")
	}
	mensagem = strings.TrimSpace(mensagem)
	if mensagem == "" {
		return service.ChatResultado{}, fmt.Errorf("mensagem vazia")
	}

	// Comandos rígidos executados localmente, sem chamar a API — funcionam
	// até sem chave de IA configurada. "/vincular" (varredura de vínculos)
	// vem primeiro; "/move" (ticket do 360 ou card) e "/apagar" depois;
	// "apague o card do João" / "mova o card 123 para Cobrança" por último.
	// Frases fora do molde caem no fluxo normal do agente.
	local, reconhecido := a.executarComandoVincular(mensagem)
	if !reconhecido {
		local, reconhecido = a.executarComandoMove(mensagem, chatID)
	}
	if !reconhecido {
		local, reconhecido = a.executarComandoApagar(mensagem, chatID)
	}
	if !reconhecido {
		local, reconhecido = a.executarComandoRealizada(mensagem, chatID)
	}
	if !reconhecido {
		local, reconhecido = a.executarIntencaoLocal(mensagem)
	}
	if reconhecido {
		if err := a.chats.AddMensagem(a.ctx, chatID, "user", mensagem, ""); err != nil {
			return service.ChatResultado{}, friendly("EnviarChat", err)
		}
		// Caminhos de erro dos comandos locais devolvem Acoes nil, que vira
		// null no JSON e derruba o frontend (res.acoes.length).
		if local.Acoes == nil {
			local.Acoes = []service.AcaoDoAgente{}
		}
		if err := a.gravarResposta(chatID, local); err != nil {
			return service.ChatResultado{}, err
		}
		return *local, nil
	}

	if a.groq == nil || !a.groq.TemChave() {
		// Listagem de quadro é 100% banco local: responde direto, sem IA.
		if resp := a.respostaBoardSemIA(mensagem); resp != "" {
			res := &service.ChatResultado{Resposta: resp,
				Acoes: []service.AcaoDoAgente{{Ferramenta: "listar_tickets",
					Resumo: "listou os cards direto do banco local (sem IA)"}}}
			if err := a.chats.AddMensagem(a.ctx, chatID, "user", mensagem, ""); err != nil {
				return service.ChatResultado{}, friendly("EnviarChat", err)
			}
			if err := a.gravarResposta(chatID, res); err != nil {
				return service.ChatResultado{}, err
			}
			return *res, nil
		}
		return service.ChatResultado{}, fmt.Errorf("configure a chave da IA em ⚙ Configurações para usar o assistente")
	}

	historico, err := a.historicoRecente(chatID)
	if err != nil {
		return service.ChatResultado{}, err
	}
	if err := a.chats.AddMensagem(a.ctx, chatID, "user", mensagem, ""); err != nil {
		return service.ChatResultado{}, friendly("EnviarChat", err)
	}

	ctx, cancel := context.WithTimeout(a.ctx, 90*time.Second)
	defer cancel()

	// Pré-busca: mensagem que pede dados do 360 sai com o contexto já
	// coletado (1 requisição HTTP) e sem as tools de leitura no request.
	// Sessão expirada/ausente PARA AQUI — a IA não é acionada.
	pre, err := a.prepararPrebusca(ctx, mensagem)
	if err != nil {
		if errors.Is(err, rota360.ErrSessaoExpirada) {
			return service.ChatResultado{}, errors.New("sessão do 360 expirada — o portal rejeitou o cookie. Reconecte-se em ⚙ Configurações; o assistente não foi acionado")
		}
		if errors.Is(err, rota360.ErrSemSessao) {
			return service.ChatResultado{}, errors.New("sessão do 360 não configurada — conecte-se em ⚙ Configurações para o assistente consultar dados; o assistente não foi acionado")
		}
		return service.ChatResultado{}, friendly("EnviarChat", err)
	}
	if pre != nil {
		a.muPrebusca.Lock()
		a.cachePrebusca = pre.cache
		a.muPrebusca.Unlock()
		defer func() {
			a.muPrebusca.Lock()
			a.cachePrebusca = nil
			a.muPrebusca.Unlock()
		}()
	}

	res, err := a.groq.ConversarCom(ctx, historico, mensagem, a.textoPadroes(a.ctx), a, nil, pre.opcoes())
	if err != nil {
		if err == service.ErrGroqSemChave {
			return service.ChatResultado{}, err
		}
		return service.ChatResultado{}, friendly("EnviarChat", err)
	}
	a.registrarStatsPrebusca(pre, res.Acoes)
	if res.Acoes == nil {
		res.Acoes = []service.AcaoDoAgente{}
	}
	if err := a.gravarResposta(chatID, res); err != nil {
		return service.ChatResultado{}, err
	}
	return *res, nil
}

// historicoRecente devolve as últimas 10 mensagens persistidas do chat.
func (a *App) historicoRecente(chatID int64) ([]service.MensagemDeChat, error) {
	persistidas, err := a.chats.Mensagens(a.ctx, chatID)
	if err != nil {
		return nil, friendly("EnviarChat", err)
	}
	if len(persistidas) > 10 {
		persistidas = persistidas[len(persistidas)-10:]
	}
	historico := make([]service.MensagemDeChat, 0, len(persistidas))
	for _, m := range persistidas {
		historico = append(historico, service.MensagemDeChat{Papel: m.Papel, Conteudo: m.Conteudo})
	}
	return historico, nil
}

// gravarResposta persiste a resposta do agente com as ações executadas.
func (a *App) gravarResposta(chatID int64, res *service.ChatResultado) error {
	acoesJSON, err := json.Marshal(res.Acoes)
	if err != nil {
		return friendly("EnviarChat", err)
	}
	if err := a.chats.AddMensagem(a.ctx, chatID, "assistant", res.Resposta, string(acoesJSON)); err != nil {
		return friendly("EnviarChat", err)
	}
	return nil
}

// textoPadroes monta o bloco de padrões salvos para o system prompt do agente
// ("- nome: conteudo" por linha); vazio se não houver padrões (ou falha na
// leitura, que não deve impedir a conversa).
func (a *App) textoPadroes(ctx context.Context) string {
	if a.padroes == nil {
		return ""
	}
	ps, err := a.padroes.List(ctx)
	if err != nil || len(ps) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, p := range ps {
		fmt.Fprintf(&sb, "- %s: %s\n", p.Nome, p.Conteudo)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// ListarPadroes devolve os padrões permanentes salvos pelo chat (para a UI).
func (a *App) ListarPadroes() ([]db.Padrao, error) {
	if a.padroes == nil {
		return []db.Padrao{}, nil
	}
	ps, err := a.padroes.List(a.ctx)
	if err != nil {
		return nil, friendly("ListarPadroes", err)
	}
	if ps == nil {
		ps = []db.Padrao{}
	}
	return ps, nil
}

// ApagarPadrao remove um padrão permanente (para a UI).
func (a *App) ApagarPadrao(nome string) error {
	if a.padroes == nil {
		return fmt.Errorf("padrões indisponíveis")
	}
	if err := a.padroes.Delete(a.ctx, nome); err != nil {
		if err == db.ErrPadraoNaoEncontrado {
			return err
		}
		return friendly("ApagarPadrao", err)
	}
	return nil
}

// ExecutarAgente roda a ferramenta pedida pelo modelo. É a implementação
// do service.ToolExecutor — todas as mutações passam pelas mesmas validações
// dos bindings do app.
func (a *App) ExecutarAgente(ctx context.Context, ferramenta, argsJSON string) (string, string, error) {
	var args map[string]any
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", "", fmt.Errorf("argumentos inválidos")
		}
	}
	argStr := func(k string) string { v, _ := args[k].(string); return strings.TrimSpace(v) }
	argInt := func(k string) int64 { v, _ := args[k].(float64); return int64(v) }
	cardID := func() (int64, error) {
		id := argInt("cardId")
		if id <= 0 {
			id = argInt("ticketId")
		}
		if id <= 0 {
			return 0, fmt.Errorf("cardId é obrigatório")
		}
		return a.resolverCardID(id)
	}

	// Ferramenta cuja fatia a pré-busca já injetou no contexto: responde do
	// cache — zero HTTP e zero rodada extra de API.
	if txt, ok := a.cacheServir(ferramenta); ok {
		return txt, "respondeu do contexto pré-coletado (" + ferramenta + ")", nil
	}

	switch ferramenta {
	case "buscar_cliente_360":
		q := argStr("query")
		if q == "" {
			return "", "", fmt.Errorf("query vazia")
		}
		res, err := a.agentBuscarClientes(ctx, q)
		if err != nil {
			return "", "", err
		}
		if len(res) == 0 {
			return "Nenhum cliente encontrado.", "busca sem resultados", nil
		}
		return res, fmt.Sprintf("buscou %q no 360: %d resultado(s)", q, len(res)), nil

	case "importar_cliente":
		cod, coluna := argStr("codService"), argStr("coluna")
		if cod == "" || coluna == "" {
			return "", "", fmt.Errorf("codService e coluna são obrigatórios")
		}
		t, err := a.agentImportarCliente(ctx, cod, coluna)
		if err != nil {
			return "", "", err
		}
		return fmt.Sprintf("Card #%d criado: %s na coluna %s.", t.ID, t.NomeCliente, t.ColunaAtual),
			fmt.Sprintf("adicionou %s (%s) na coluna %s", t.NomeCliente, cod, t.ColunaAtual), nil

	case "listar_colunas":
		cs, err := a.colunas.List(ctx)
		if err != nil {
			return "", "", err
		}
		return strings.Join(cs, ", "), "listou as colunas", nil

	case "criar_coluna":
		nome := argStr("nome")
		if nome == "" {
			return "", "", fmt.Errorf("nome vazio")
		}
		criada, err := a.colunas.Add(ctx, nome)
		if err != nil {
			return "", "", err
		}
		return "Coluna criada: " + criada, "criou a coluna " + criada, nil

	case "reordenar_colunas":
		brutos, ok := args["ordem"].([]any)
		if !ok || len(brutos) == 0 {
			return "", "", fmt.Errorf("ordem é obrigatória: a lista COMPLETA das colunas na ordem desejada")
		}
		ordem := make([]string, 0, len(brutos))
		for _, b := range brutos {
			if s, _ := b.(string); strings.TrimSpace(s) != "" {
				ordem = append(ordem, strings.TrimSpace(s))
			}
		}
		if err := a.colunas.Reordenar(ctx, ordem); err != nil {
			return "", "", err
		}
		return "Nova ordem das colunas: " + strings.Join(ordem, " | "), "reordenou as colunas", nil

	case "financeiro_cliente":
		cod := argStr("codService")
		if cod == "" {
			return "", "", fmt.Errorf("codService vazio")
		}
		return a.agentFinanceiro(ctx, cod)

	case "equipamentos_cliente":
		cod := argStr("codService")
		if cod == "" {
			return "", "", fmt.Errorf("codService vazio")
		}
		return a.agentEquipamentos(ctx, cod)

	case "atendimentos_cliente":
		cod := argStr("codService")
		if cod == "" {
			return "", "", fmt.Errorf("codService vazio")
		}
		return a.agentAtendimentos(ctx, cod)

	case "pesquisar_tickets":
		q := argStr("query")
		if q == "" {
			return "", "", fmt.Errorf("query vazia")
		}
		return a.agentPesquisarTickets(ctx, q)

	case "detalhe_ticket_360":
		numero := argStr("numero")
		if numero == "" {
			return "", "", fmt.Errorf("numero vazio")
		}
		return a.agentDetalheTicket(ctx, numero)

	case "abrir_ticket_360":
		numero := argStr("numero")
		if numero == "" {
			return "", "", fmt.Errorf("numero vazio")
		}
		return a.agentAbrirTicket(ctx, numero)

	case "responder_ticket_360":
		numero, texto := argStr("numero"), argStr("texto")
		if numero == "" || texto == "" {
			return "", "", fmt.Errorf("numero e texto são obrigatórios")
		}
		return a.agentResponderTicket(ctx, numero, texto)

	case "alterar_status_ticket_360":
		numero, status := argStr("numero"), argStr("status")
		if numero == "" || status == "" {
			return "", "", fmt.Errorf("numero e status são obrigatórios")
		}
		return a.agentAlterarStatusTicket(ctx, numero, status)

	case "listar_tickets":
		return a.agentListarTickets(ctx, argStr("coluna"))

	case "mover_card":
		id, err := cardID()
		if err != nil {
			return "", "", err
		}
		coluna := argStr("coluna")
		if coluna == "" {
			return "", "", fmt.Errorf("coluna é obrigatória")
		}
		if err := a.MoveTicket(id, coluna, a.contagemColuna(coluna)); err != nil {
			return "", "", err
		}
		extra := ""
		if coluna == service.ColunaCobranca {
			extra = " (fluxo de cobrança disparado no próximo acesso pelo usuário)"
		}
		if coluna == ColunaUpgrade {
			extra = " (falta registrar os valores com registrar_comissao se ainda não registrou)"
		}
		return fmt.Sprintf("Card %d movido para %s%s.", id, coluna, extra),
			fmt.Sprintf("moveu o card %d para %s", id, coluna), nil

	case "verificar_tickets_fechados":
		res, err := a.VerificarTicketsFechados()
		if err != nil {
			return "", "", err
		}
		msg := fmt.Sprintf("%d ticket(s) verificado(s): %d card(s) movido(s) para %s, %d ficaram.",
			res.Verificados, len(res.Movidos), res.Coluna, res.Ficaram)
		for _, m := range res.Movidos {
			nome := m.Cliente
			if nome == "" {
				nome = fmt.Sprintf("card %d", m.CardID)
			}
			num := m.TicketNumero
			if num != "" {
				num = " #" + num
			}
			msg += fmt.Sprintf("\n• %s%s — %s", nome, num, m.Motivo)
		}
		for _, e := range res.Erros {
			msg += "\n⚠ " + e
		}
		return msg, fmt.Sprintf("moveu %d card(s) para %s", len(res.Movidos), res.Coluna), nil

	case "registrar_comissao":
		id, err := cardID()
		if err != nil {
			return "", "", err
		}
		servico, err := parseReaisParaCents(argStr("valorServico"))
		if err != nil {
			return "", "", fmt.Errorf("valorServico: %v", err)
		}
		parcela, err := parseReaisParaCents(argStr("valorParcela"))
		if err != nil {
			return "", "", fmt.Errorf("valorParcela: %v", err)
		}
		t, err := a.tickets.Get(ctx, id)
		if err != nil {
			return "", "", err
		}
		c, err := a.SalvarComissao(id, servico, parcela, argStr("observacoes"), argStr("dataRegistro"))
		if err != nil {
			return "", "", err
		}
		data := c.DataRegistro
		if data == "" {
			data = time.Now().Format("2006-01-02")
		}
		comServico := servico * db.ComissaoServicoPct / 100
		comParcela := parcela * db.ComissaoParcelaPct / 100
		return fmt.Sprintf("Card %d (%s) na coluna %s. Comissão registrada em %s: serviço R$ %.2f → %d%% = R$ %.2f; parcela R$ %.2f → %d%% = R$ %.2f. TOTAL R$ %.2f. Botão ✅ no card marca como recebida.",
			id, t.NomeCliente, ColunaUpgrade, data,
			float64(servico)/100, db.ComissaoServicoPct, float64(comServico)/100,
			float64(parcela)/100, db.ComissaoParcelaPct, float64(comParcela)/100,
			float64(c.ComissaoCents)/100),
			fmt.Sprintf("registrou comissão de %s (R$ %.2f) na coluna Upgrade", t.NomeCliente, float64(c.ComissaoCents)/100), nil

	case "comissao_recebida":
		id, err := cardID()
		if err != nil {
			return "", "", err
		}
		c, err := a.MarcarComissaoRecebida(id)
		if err != nil {
			return "", "", err
		}
		return fmt.Sprintf("Comissão do card %d marcada como RECEBIDA em %s (R$ %.2f) — está no histórico.",
			id, c.RecebidoEm, float64(c.ComissaoCents)/100),
			fmt.Sprintf("marcou comissão do card %d como recebida", id), nil

	case "listar_comissoes":
		cs, err := a.ListarComissoes()
		if err != nil {
			return "", "", err
		}
		if len(cs) == 0 {
			return "Nenhuma comissão registrada.", "listou as comissões: nenhuma", nil
		}
		var pendTotal, recTotal int64
		var sb strings.Builder
		for _, c := range cs {
			if c.Recebido {
				recTotal += c.ComissaoCents
				fmt.Fprintf(&sb, "✅ %s — R$ %.2f (recebida em %s)\n", c.DataRegistro, float64(c.ComissaoCents)/100, c.RecebidoEm)
			} else {
				pendTotal += c.ComissaoCents
				fmt.Fprintf(&sb, "⏳ %s — R$ %.2f\n", c.DataRegistro, float64(c.ComissaoCents)/100)
			}
		}
		sb.WriteString(fmt.Sprintf("Pendentes: R$ %.2f | Recebidas: R$ %.2f", float64(pendTotal)/100, float64(recTotal)/100))
		return sb.String(), "listou as comissões", nil

	case "agendar_visita":
		id, err := cardID()
		if err != nil {
			return "", "", err
		}
		if argStr("data") == "" {
			return "", "", fmt.Errorf("data obrigatória (AAAA-MM-DD)")
		}
		atual, err := a.tickets.Get(ctx, id)
		if err != nil {
			return "", "", err
		}
		hora, turno := atual.VisitaHora, atual.VisitaTurno
		if quando := argStr("quando"); quando != "" {
			if len(quando) == 5 && quando[2] == ':' {
				hora, turno = quando, ""
			} else {
				turno, hora = quando, ""
			}
		}
		endereco := argStr("endereco")
		if endereco == "" {
			endereco = atual.VisitaEndereco
		}
		t, err := a.SalvarVisita(id, argStr("data"), turno, hora, endereco)
		if err != nil {
			return "", "", err
		}
		extra := ""
		if t.VisitaHora != "" {
			extra = " às " + t.VisitaHora
		} else if t.VisitaTurno != "" {
			extra = " (" + t.VisitaTurno + ")"
		}
		return fmt.Sprintf("Visita do card %d (%s) agendada para %s%s.",
			t.ID, t.NomeCliente, t.VisitaData, extra),
			fmt.Sprintf("agendou visita de %s para %s", t.NomeCliente, t.VisitaData), nil

	case "apagar_card":
		id, err := cardID()
		if err != nil {
			return "", "", err
		}
		t, err := a.tickets.Get(ctx, id)
		if err != nil {
			return "", "", err
		}
		if err := a.DeleteTicket(id); err != nil {
			return "", "", err
		}
		return fmt.Sprintf("Card %d (%s) apagado, junto com os acordos (estava em %q).", id, t.NomeCliente, t.ColunaAtual),
			fmt.Sprintf("apagou o card %d (%s)", id, t.NomeCliente), nil

	case "vincular_ticket":
		id, err := cardID()
		if err != nil {
			return "", "", err
		}
		t, aviso, err := a.vincularTicket360(id, argStr("numero"))
		if err != nil {
			return "", "", err
		}
		if t.Ticket360Numero == "" {
			return fmt.Sprintf("Card %d (%s): vínculo com o ticket do 360 removido.", t.ID, t.NomeCliente),
				fmt.Sprintf("desvinculou o ticket 360 do card %d", t.ID), nil
		}
		resp := fmt.Sprintf("Card %d (%s) agora aponta para o ticket #%s do 360.", t.ID, t.NomeCliente, t.Ticket360Numero)
		if aviso != "" {
			resp += " Obs.: " + aviso
		}
		return resp,
			fmt.Sprintf("vinculou o ticket #%s ao card %d (%s)", t.Ticket360Numero, t.ID, t.NomeCliente), nil

	case "observacao_card":
		obs := argStr("observacoes")
		id, err := cardID()
		if err != nil {
			return "", "", err
		}
		if argStr("modo") == "acrescentar" && obs != "" {
			t, err := a.tickets.Get(ctx, id)
			if err != nil {
				return "", "", err
			}
			if atual := strings.TrimSpace(t.Observacoes); atual != "" {
				obs = atual + "\n" + obs
			}
		}
		if err := a.tickets.SetObservacoes(ctx, id, obs); err != nil {
			return "", "", err
		}
		rotulo := "observações atualizadas"
		if obs == "" {
			rotulo = "observações apagadas"
		}
		return fmt.Sprintf("Card %d: %s.\nObservações agora:\n%s", id, rotulo, strings.TrimSpace(obs)),
			fmt.Sprintf("salvou as observações internas do card %d", id), nil

	case "avaliar_cliente":
		id, err := cardID()
		if err != nil {
			return "", "", err
		}
		return a.agentAvaliar(ctx, id)

	case "gerar_proposta":
		id, err := cardID()
		if err != nil {
			return "", "", err
		}
		return a.agentProposta(ctx, id)

	case "listar_tabela_orcamento":
		t, err := a.planilhaParaAgente(ctx)
		if err != nil {
			return "", "", err
		}
		var sb strings.Builder
		for _, tipo := range t.Tipos {
			fmt.Fprintf(&sb, "== %s (taxa de instalação R$ %.2f) ==\n", tipo.Nome, tipo.TaxaInstalacao)
			for _, sec := range tipo.Secoes {
				for _, it := range sec.Itens {
					preco := ""
					if it.Taxa != nil {
						preco += fmt.Sprintf(" instalação R$ %.2f;", *it.Taxa)
					}
					if it.Mensal != nil {
						preco += fmt.Sprintf(" R$ %.2f/mês;", *it.Mensal)
					}
					fmt.Fprintf(&sb, "- [%s] %s:%s\n", sec.Titulo, it.Nome, preco)
				}
			}
			for _, f := range tipo.FaixasMensal {
				fmt.Fprintf(&sb, "* mensalidade %s: R$ %.2f\n", f.Rotulo, f.Valor)
			}
		}
		return sb.String(), "listou a tabela de orçamentos", nil

	case "gerar_orcamento":
		tipo, _ := args["tipoSistema"].(string)
		itensArg, _ := args["itens"].(map[string]any)
		if strings.TrimSpace(tipo) == "" || len(itensArg) == 0 {
			return "", "", fmt.Errorf("tipoSistema e itens são obrigatórios")
		}
		itens := map[string]int{}
		for nome, q := range itensArg {
			qf, _ := q.(float64)
			itens[nome] = int(qf)
		}
		t, err := a.planilhaParaAgente(ctx)
		if err != nil {
			return "", "", err
		}
		res, err := orcamento.Calcular(t, orcamento.Selecao{TipoSistema: tipo, Itens: itens})
		if err != nil {
			return "", "", err
		}
		cliente := ""
		if id, err := cardID(); err == nil {
			if t, err := a.tickets.Get(a.ctx, id); err == nil {
				cliente = t.NomeCliente
			}
		}
		return res.PropostaTexto(cliente),
			fmt.Sprintf("gerou orçamento %s (R$ %.2f/mês + instalação R$ %.2f)", tipo, res.Mensal, res.TaxaInstalacao), nil

	case "salvar_padrao":
		nome, conteudo := argStr("nome"), argStr("conteudo")
		if err := a.padroesUpsert(ctx, nome, conteudo); err != nil {
			return "", "", err
		}
		return fmt.Sprintf("Padrão %q salvo: %s", nome, conteudo),
			fmt.Sprintf("salvou o padrão %q", nome), nil

	case "editar_prompt_proposta":
		acao := strings.ToLower(strings.TrimSpace(argStr("acao")))
		if acao == "ler" || acao == "" {
			atual := a.PromptPolirAtual()
			resumo := atual
			if len(resumo) > 400 {
				resumo = resumo[:400] + "…"
			}
			return resumo, "leu o prompt da proposta", nil
		}
		if acao != "salvar" && acao != "salvo" {
			return "", "", fmt.Errorf("acao inválida: %q (use ler ou salvar)", acao)
		}
		texto := strings.TrimSpace(argStr("texto"))
		if texto == "" {
			return "", "", fmt.Errorf("informe o texto completo do novo prompt em 'texto'")
		}
		if err := a.SalvarPromptPolir(texto); err != nil {
			return "", "", err
		}
		return fmt.Sprintf("Prompt da proposta salvo (%d caracteres). Vale já para as próximas propostas.", len(texto)),
			"salvou o prompt da proposta", nil

	case "listar_padroes":
		ps, err := a.padroes.List(ctx)
		if err != nil {
			return "", "", err
		}
		if len(ps) == 0 {
			return "Nenhum padrão salvo ainda.", "listou os padrões (vazio)", nil
		}
		var sb strings.Builder
		for _, p := range ps {
			fmt.Fprintf(&sb, "- %s: %s\n", p.Nome, p.Conteudo)
		}
		return sb.String(), fmt.Sprintf("listou %d padrão(ões)", len(ps)), nil

	case "apagar_padrao":
		nome := argStr("nome")
		if nome == "" {
			return "", "", fmt.Errorf("nome vazio")
		}
		if err := a.padroes.Delete(ctx, nome); err != nil {
			return "", "", err
		}
		return "Padrão apagado: " + nome, "apagou o padrão " + nome, nil

	default:
		return "", "", fmt.Errorf("ferramenta desconhecida: %s", ferramenta)
	}
}

// padroesUpsert valida o limite de padrões e grava (cria ou substitui).
func (a *App) padroesUpsert(ctx context.Context, nome, conteudo string) error {
	if a.padroes == nil {
		return fmt.Errorf("padrões indisponíveis")
	}
	if strings.TrimSpace(nome) == "" {
		return db.ErrPadraoNomeVazio
	}
	if strings.TrimSpace(conteudo) == "" {
		return db.ErrPadraoConteudoVazio
	}
	n, err := a.padroes.Contagem(ctx)
	if err != nil {
		return err
	}
	if n >= db.MaxPadroes {
		return db.ErrPadroesExcedidos
	}
	return a.padroes.Upsert(ctx, nome, conteudo)
}

func (a *App) contagemColuna(coluna string) int {
	ts, err := a.tickets.List(a.ctx)
	if err != nil {
		return 0
	}
	n := 0
	for _, t := range ts {
		if t.ColunaAtual == coluna {
			n++
		}
	}
	return n
}

func (a *App) agentBuscarClientes(ctx context.Context, q string) (string, error) {
	c := a.rotaAtual()
	if c == nil {
		return "", rota360.ErrSemSessao
	}
	filtro := "todos"
	if isNumero(q) {
		filtro = "codcliente"
	}
	res, err := c.BuscarClientes(ctx, q, filtro)
	if err != nil {
		return "", err
	}
	if len(res) == 0 {
		return "", nil
	}
	var sb strings.Builder
	for i, r := range res {
		if i >= 10 {
			sb.WriteString(fmt.Sprintf("… (+%d mais)", len(res)-10))
			break
		}
		fmt.Fprintf(&sb, "cod=%d | %s | CNPJ=%s | fone=%s | %s\n",
			r.CodCliente, r.Nome, r.CGCCPF, r.Fone1, r.Situacao)
	}
	return sb.String(), nil
}

func isNumero(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func (a *App) agentImportarCliente(ctx context.Context, cod, coluna string) (db.Ticket, error) {
	c := a.rotaAtual()
	if c == nil {
		return db.Ticket{}, rota360.ErrSemSessao
	}
	if !a.colunaValida(coluna) {
		return db.Ticket{}, service.ErrColunaInvalida
	}
	cliente, _, err := a.sincronizarCliente(ctx, c, cod)
	if err != nil {
		return db.Ticket{}, err
	}
	endereco := strings.TrimSpace(cliente.Endereco + " " + cliente.NumEndereco)
	t := db.Ticket{
		NomeCliente: limitarTexto(cliente.Nome, service.MaxNomeCliente),
		TipoTicket:  "Outro",
		CodService:  cod,
		Endereco:    limitarTexto(endereco, service.MaxEndereco),
		Telefone:    limitarTexto(cliente.Fone1, service.MaxTelefone),
		ColunaAtual: coluna,
	}
	id, err := a.tickets.Save(a.ctx, &t)
	if err != nil {
		return db.Ticket{}, err
	}
	salvo, err := a.tickets.Get(a.ctx, id)
	if err != nil {
		return db.Ticket{}, err
	}
	a.vinculoAutomaticoTela(salvo.ID, salvo.CodService, salvo.NomeCliente)
	return *salvo, nil
}

func limitarTexto(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// agentFinanceiro devolve só a parte de dinheiro: saldo devedor e faturas
// atrasadas. É a fatia que a IA puxa quando o ticket é de cobrança — o resto
// (equipamentos, atendimentos) fica em ferramentas separadas, para o Groq
// receber apenas o que o caso precisa.
func (a *App) agentFinanceiro(ctx context.Context, cod string) (string, string, error) {
	c := a.rotaAtual()
	if c == nil {
		return "", "", rota360.ErrSemSessao
	}
	fin, err := c.FinanceiroCliente(ctx, cod)
	if err != nil {
		return "", "", err
	}
	return textoFinanceiro360(cod, fin), fmt.Sprintf("consultou o financeiro do cliente %s", cod), nil
}

// textoFinanceiro360 formata a fatia financeira do dashboard (puro, sem
// rede) — compartilhado entre a tool do agente e a pré-busca.
func textoFinanceiro360(cod string, fin *rota360.Dashboard360) string {
	dados := fin.Sources.InsideService.Data
	f := dados.Financeiro
	var sb strings.Builder
	fmt.Fprintf(&sb, "Cliente %s — saldo devedor: R$ %.2f (com encargos R$ %.2f; encargos R$ %.2f); %d fatura(s) atrasada(s).\n",
		cod, f.TotalDevendo, f.TotalDevendoComEncargos, f.TotalEncargosAtraso, f.TotalAtrasadas)
	for _, fat := range dados.FaturasAbertas {
		fmt.Fprintf(&sb, "Doc %s: R$ %.2f, venceu %s, %d dias de atraso (multa R$ %.2f, mora R$ %.2f)\n",
			fat.NumeroDoc, fat.ValorDuplicata, fat.Vencimento.Format("02/01/2006"), fat.DiasAtraso, fat.MultaValor, fat.MoraValor)
	}
	return sb.String()
}

// agentEquipamentos devolve o que o cliente tem cadastrado (alarme, câmera,
// sensores, produtos) — a fatia para tickets técnicos e orçamentos.
func (a *App) agentEquipamentos(ctx context.Context, cod string) (string, string, error) {
	c := a.rotaAtual()
	if c == nil {
		return "", "", rota360.ErrSemSessao
	}
	fin, err := c.FinanceiroCliente(ctx, cod)
	if err != nil {
		return "", "", err
	}
	resumo := resumoEquipamentos(fin.Sources.Produtos.Data)
	if dc, err := c.DadosCliente(ctx, cod); err == nil && strings.TrimSpace(dc.Observacoes) != "" {
		resumo += "\nObservações do cadastro:\n" + limitarTexto(dc.Observacoes, 500) + "\n"
	}
	return resumo, fmt.Sprintf("consultou os equipamentos do cliente %s", cod), nil
}

// agentAtendimentos devolve o histórico de O.S. (incluindo concluídas —
// fonte os-funil, pois o dashboard360 só expõe as ativas) e os atendimentos
// recentes (Octadesk) — a fatia para entender o histórico do cliente.
func (a *App) agentAtendimentos(ctx context.Context, cod string) (string, string, error) {
	c := a.rotaAtual()
	if c == nil {
		return "", "", rota360.ErrSemSessao
	}
	var (
		funil *rota360.OSFunil360
		fin   *rota360.Dashboard360
	)
	var errFunil error
	funil, errFunil = c.OSFunil(ctx, cod)
	if errFunil != nil {
		slog.Warn("os-funil do cliente falhou", "cod", cod, "err", errFunil)
	}
	fin, errFin := c.FinanceiroCliente(ctx, cod)
	if errFin != nil && errFunil != nil {
		return "", "", errFunil
	}
	return textoAtendimentos360(cod, funil, fin),
		fmt.Sprintf("consultou os atendimentos do cliente %s", cod), nil
}

// textoAtendimentos360 formata a fatia de O.S./atendimentos (puro, sem
// rede) — compartilhado entre a tool do agente e a pré-busca.
func textoAtendimentos360(cod string, funil *rota360.OSFunil360, fin *rota360.Dashboard360) string {
	var sb strings.Builder
	if funil != nil {
		ct := funil.Contagem
		fmt.Fprintf(&sb, "Ordens de serviço (histórico): %d concluída(s), %d cancelada(s), %d fechada(s) sem processamento, %d atendida(s) sem fechamento, %d sem atendimento.\n",
			ct.Concluida, ct.Cancelada, ct.FechadaSemProcessam, ct.AtendidaSemFechamento, ct.SemAtendimento)
		for i, os := range funil.Itens {
			if i >= 10 {
				fmt.Fprintf(&sb, "… (+%d mais antigas)\n", len(funil.Itens)-10)
				break
			}
			abertura, fechamento := "—", "—"
			if os.DataAbertura != nil {
				abertura = os.DataAbertura.Format("02/01/2006")
			}
			if os.DataFechamento != nil {
				fechamento = os.DataFechamento.Format("02/01/2006")
			}
			desc := primeiraLinha(os.DefeitoSigma)
			if desc == "" {
				desc = primeiraLinha(os.Descricao)
			}
			fmt.Fprintf(&sb, "- O.S. %d [%s/%s] aberta %s, fechada %s, técnico %s: %s\n",
				os.Ordem, rota360.StatusOS(os.Status), rota360.TipoOS(os.Tipo), abertura, fechamento, os.Tecnico, limitarTexto(desc, 120))
		}
	}
	// O dashboard complementa com as OS em aberto do cadastro (quando o
	// os-funil falhou, é a única visão de OS que sobra).
	if fin != nil {
		dados := fin.Sources.InsideService.Data
		if n := len(dados.OSAtivas); n > 0 {
			sb.WriteString("\nOS ativas no cadastro:\n")
			for i, os := range dados.OSAtivas {
				if i >= 5 {
					fmt.Fprintf(&sb, "… (+%d mais)\n", n-5)
					break
				}
				fmt.Fprintf(&sb, "- %s\n", resumoMapa(os, "numero", "descricao", "status", "data", "situacao"))
			}
		} else if funil == nil {
			sb.WriteString("Sem OS ativas no cadastro.\n")
		}
		if rec := fin.Sources.Octadesk.Data.TicketsRecentes; len(rec) > 0 {
			sb.WriteString("\nAtendimentos recentes (Octadesk):\n")
			for i, t := range rec {
				if i >= 5 {
					fmt.Fprintf(&sb, "… (+%d mais)\n", len(rec)-5)
					break
				}
				fmt.Fprintf(&sb, "- #%s %s [%s] (%s)\n", t.Number, t.Summary, t.Status.Name, t.CreatedAt)
			}
		}
	}
	return sb.String()
}

// primeiraLinha devolve a primeira linha não vazia do texto.
func primeiraLinha(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

// resumoMapa junta os valores dos campos pedidos de um mapa genérico
// (OS ativas e outros blocos do dashboard vêm sem schema fixo).
func resumoMapa(m map[string]any, campos ...string) string {
	var partes []string
	for _, c := range campos {
		if v, ok := m[c]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprintf("%v", v))
			if s != "" && s != "<nil>" {
				partes = append(partes, s)
			}
		}
	}
	if len(partes) == 0 {
		b, _ := json.Marshal(m)
		return limitarTexto(string(b), 200)
	}
	return strings.Join(partes, " | ")
}

// regexModeloCamera captura os modelos de câmera wifi usados pela Rota
// (IM3, IM5, IM7, IM9) nas descrições de produto.
var regexModeloCamera = regexp.MustCompile(`IM[3579]`)

// resumoEquipamentos deduz do cadastro de produtos o que o cliente tem:
// alarme e/ou câmera, quantidade de sensores/controles/câmeras e o modelo
// das câmeras wifi. As descrições do Service marcam o sistema no nome
// ("-ALARME-" ou "-CFTV-"), então a classificação é heurística. A lista de
// produtos é um bônus de contexto: contagens entram primeiro, e só as
// primeiras linhas vão para a IA.
func resumoEquipamentos(prods []rota360.Produto360) string {
	if len(prods) == 0 {
		return "Produtos cadastrados: nenhum.\n"
	}
	temAlarme, temCamera := false, false
	sensores, controles, cameras := 0, 0, 0
	modelos := map[string]bool{}
	var ordemModelos []string
	var linhas []string
	for _, p := range prods {
		d := strings.ToUpper(p.Descricao)
		temAlarme = temAlarme || strings.Contains(d, "ALARME")
		ehCamera := strings.Contains(d, "CAMERA") || strings.Contains(d, "CÂMERA")
		temCamera = temCamera || ehCamera || strings.Contains(d, "CFTV")
		switch {
		case strings.Contains(d, "SENSOR"):
			sensores += int(p.Quantidade)
		case strings.Contains(d, "CONTROLE"):
			controles += int(p.Quantidade)
		case ehCamera:
			cameras += int(p.Quantidade)
		}
		for _, m := range regexModeloCamera.FindAllString(d, -1) {
			if !modelos[m] {
				modelos[m] = true
				ordemModelos = append(ordemModelos, m)
			}
		}
		if len(linhas) < 8 {
			linhas = append(linhas, fmt.Sprintf("- %dx %s (%s)", p.Quantidade, p.Descricao, p.Tipo))
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Equipamentos no cadastro: alarme=%s, câmera=%s",
		simNao(temAlarme), simNao(temCamera))
	if len(ordemModelos) > 0 {
		fmt.Fprintf(&sb, " (câmera wifi modelo %s)", strings.Join(ordemModelos, "/"))
	}
	fmt.Fprintf(&sb, "; sensores: %d, controles: %d, câmeras: %d. Produtos (%d):\n",
		sensores, controles, cameras, len(prods))
	sb.WriteString(strings.Join(linhas, "\n"))
	if len(prods) > 8 {
		fmt.Fprintf(&sb, "\n… (+%d mais)\n", len(prods)-8)
	}
	sb.WriteString("\n")
	return sb.String()
}

func simNao(b bool) string {
	if b {
		return "SIM"
	}
	return "não"
}

// resolverTicket360 localiza o ticket da fila do helpdesk pelo número e
// devolve o item com o uuid (Campo ID) para detalhe/escrita/URL.
func (a *App) resolverTicket360(ctx context.Context, numero string) (*rota360.Ticket360, error) {
	c := a.rotaAtual()
	if c == nil {
		return nil, rota360.ErrSemSessao
	}
	for _, view := range []string{"em_aberto", "meus_em_aberto", "pendentes"} {
		itens, _, err := c.TicketsDaFila(ctx, view, 1, 100)
		if err != nil {
			return nil, err
		}
		for i := range itens {
			if strings.EqualFold(itens[i].Numero, numero) {
				return &itens[i], nil
			}
		}
	}
	return nil, fmt.Errorf("ticket %s não encontrado nas filas em aberto/pendentes", numero)
}

// dividirEntradaTicket separa a entrada do usuário em (uuid, numero): aceita
// a URL do ticket no 360, o uuid puro ou o número exibido (com ou sem "#").
func dividirEntradaTicket(entrada string) (uuid, numero string) {
	e := strings.TrimSpace(entrada)
	if e == "" {
		return "", ""
	}
	low := strings.ToLower(e)
	if i := strings.Index(low, "helpdesk/tickets/"); i >= 0 {
		rest := e[i+len("helpdesk/tickets/"):]
		if j := strings.IndexAny(rest, "?#/"); j >= 0 {
			rest = rest[:j]
		}
		return strings.TrimSpace(rest), ""
	}
	if len(e) == 36 && strings.Count(e, "-") == 4 {
		return e, ""
	}
	return "", strings.TrimPrefix(e, "#")
}

// VincularTicket360 liga um ticket do helpdesk 360 a um card do Kanban.
// entrada aceita número ("12345", "#12345"), uuid ou a URL do ticket; vazio
// desvincula. Com o 360 conectado, valida no helpdesk, guarda o uuid (para
// abrir/responder depois) e preenche o codService do card se estiver vazio;
// sem sessão (ou ticket fora das filas), vincula pelo que foi digitado.
func (a *App) VincularTicket360(ticketID int64, entrada string) (db.Ticket, error) {
	t, _, err := a.vincularTicket360(ticketID, entrada)
	return t, err
}

// vincularTicket360 faz o trabalho e devolve também um aviso (vazio quando a
// validação no 360 deu certa) para o chat passar ao usuário.
func (a *App) vincularTicket360(ticketID int64, entrada string) (db.Ticket, string, error) {
	t, err := a.tickets.Get(a.ctx, ticketID)
	if err != nil {
		return db.Ticket{}, "", friendly("VincularTicket360", err)
	}

	uuid, numero := dividirEntradaTicket(entrada)
	if uuid == "" && numero == "" {
		// desvincular: limpa o principal e tira da lista o ticket que era ele
		if t.Ticket360ID != "" && a.vinculos != nil {
			if vs, err := a.vinculos.List(a.ctx, t.ID); err == nil {
				for _, v := range vs {
					if v.Ticket360ID == t.Ticket360ID {
						_, _ = a.vinculos.Remove(a.ctx, v.ID)
					}
				}
			}
		}
		if err := a.tickets.VincularTicket360(a.ctx, t.ID, "", "", ""); err != nil {
			return db.Ticket{}, "", friendly("VincularTicket360", err)
		}
		t, err = a.tickets.Get(a.ctx, t.ID)
		if err != nil {
			return db.Ticket{}, "", friendly("VincularTicket360", err)
		}
		return *t, "", nil
	}

	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	var det *rota360.TicketDetalhe360
	aviso := ""
	c := a.rotaAtual()
	switch {
	case c == nil:
		aviso = "Sem sessão no 360: vinculei pelo que foi digitado, sem validar no helpdesk."
	case uuid != "":
		if d, err := c.DetalheTicket(ctx, uuid); err == nil {
			det = d
		} else {
			aviso = "Não consegui ler o ticket no 360; vinculei mesmo assim."
		}
	case numero != "":
		item, err := a.resolverTicket360(ctx, numero)
		if err != nil {
			aviso = fmt.Sprintf("Ticket #%s não está nas filas em aberto/pendentes do 360; vinculei pelo número digitado.", numero)
			break
		}
		uuid = item.ID
		if d, err := c.DetalheTicket(ctx, uuid); err == nil {
			det = d
		}
	}
	if det != nil {
		if det.Numero != "" {
			numero = det.Numero
		}
		if uuid == "" {
			uuid = det.ID
		}
	}
	if uuid == "" && numero == "" {
		return db.Ticket{}, "", fmt.Errorf("não entendi o ticket %q — cole a URL do ticket, o uuid ou o número", entrada)
	}

	cod := ""
	if det != nil {
		cod = det.CodService
	}
	if err := a.tickets.VincularTicket360(a.ctx, t.ID, uuid, numero, cod); err != nil {
		return db.Ticket{}, "", friendly("VincularTicket360", err)
	}
	// Vínculos múltiplos: registra também na lista (com o título do 360 como
	// assunto) para a aba Cliente exibir e abrir cada ticket.
	if a.vinculos != nil {
		assunto := ""
		if det != nil {
			assunto = det.Titulo
		}
		if err := a.vinculos.Upsert(a.ctx, t.ID, uuid, numero, assunto); err != nil {
			return db.Ticket{}, "", friendly("VincularTicket360", err)
		}
	}
	t, err = a.tickets.Get(a.ctx, t.ID)
	if err != nil {
		return db.Ticket{}, "", friendly("VincularTicket360", err)
	}
	return *t, aviso, nil
}

// AbrirTicket360DoCard abre no navegador o ticket 360 vinculado ao card (pelo uuid).
func (a *App) AbrirTicket360DoCard(uuid string) error {
	if strings.TrimSpace(uuid) == "" {
		return fmt.Errorf("card sem ticket do 360 vinculado")
	}
	url := urlTicket360(uuid)
	if err := tela.NavegarAbaAtiva(url); err != nil {
		return abrirNoNavegador(url)
	}
	return nil
}

// SalvarObservacoesTicket grava as observações internas do card e devolve o
// card atualizado.
func (a *App) SalvarObservacoesTicket(ticketID int64, observacoes string) (db.Ticket, error) {
	if err := a.tickets.SetObservacoes(a.ctx, ticketID, strings.TrimSpace(observacoes)); err != nil {
		return db.Ticket{}, friendly("SalvarObservacoesTicket", err)
	}
	t, err := a.tickets.Get(a.ctx, ticketID)
	if err != nil {
		return db.Ticket{}, friendly("SalvarObservacoesTicket", err)
	}
	return *t, nil
}

// vinculoAutomaticoTela liga ao card recém-criado o último ticket capturado
// pelo Shift+F2, quando for do mesmo cliente (código service igual; sem
// código no card, nome igual). Best effort: falha só vira log. Devolve o card
// atualizado (o id é sempre o mesmo).
func (a *App) vinculoAutomaticoTela(ticketID int64, codService, nomeCliente string) (db.Ticket, error) {
	t, err := a.tickets.Get(a.ctx, ticketID)
	if err != nil {
		return db.Ticket{}, err
	}
	cap := a.ultimoTicketTela()
	if cap.UUID == "" && cap.Numero == "" {
		return *t, nil // não veio de Shift+F2 em um ticket
	}
	igualCod := cap.CodService != "" && cap.CodService == strings.TrimSpace(codService)
	igualNome := cap.CodService == "" && cap.NomeCliente != "" &&
		strings.EqualFold(cap.NomeCliente, strings.TrimSpace(nomeCliente))
	if !igualCod && !igualNome {
		return *t, nil
	}
	entrada := cap.UUID
	if entrada == "" {
		entrada = "#" + cap.Numero
	}
	t2, _, err := a.vincularTicket360(ticketID, entrada)
	if err != nil {
		slog.Warn("vínculo automático do ticket da tela", "card", ticketID, "err", err)
		return *t, nil
	}
	slog.Info("ticket da tela vinculado automaticamente ao card novo", "card", ticketID, "ticket", entrada)
	return t2, nil
}

// VinculosDoCard lista os tickets do 360 vinculados ao card (pode haver vários).
func (a *App) VinculosDoCard(ticketID int64) ([]db.Vinculo360, error) {
	if a.vinculos == nil {
		return nil, fmt.Errorf("vínculos indisponíveis")
	}
	vs, err := a.vinculos.List(a.ctx, ticketID)
	if err != nil {
		return nil, friendly("VinculosDoCard", err)
	}
	return vs, nil
}

// VinculosTodos devolve todos os vínculos card↔ticket do banco — alimenta o
// filtro do board por assunto do ticket sem uma consulta por card.
func (a *App) VinculosTodos() ([]db.Vinculo360, error) {
	if a.vinculos == nil {
		return nil, fmt.Errorf("vínculos indisponíveis")
	}
	vs, err := a.vinculos.ListTodos(a.ctx)
	if err != nil {
		return nil, friendly("VinculosTodos", err)
	}
	return vs, nil
}

// DetalheVinculo360 busca o ticket do 360 completo (status, responsável,
// interações…) pelo uuid — alimenta a aba 🎫 Ticket do card completo.
func (a *App) DetalheVinculo360(uuid string) (*rota360.TicketDetalhe360, error) {
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return nil, fmt.Errorf("uuid do ticket vazio")
	}
	c := a.rotaAtual()
	if c == nil {
		return nil, rota360.ErrSemSessao
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	t, err := c.DetalheTicket(ctx, uuid)
	if err != nil {
		return nil, friendly("DetalheVinculo360", err)
	}
	return t, nil
}

// RemoverVinculo360 tira um ticket da lista de vínculos do card. Se era o
// principal (campos legados do card), promove o mais recente restante — ou
// limpa os campos se não sobrar nenhum.
func (a *App) RemoverVinculo360(vinculoID int64) ([]db.Vinculo360, error) {
	if a.vinculos == nil {
		return nil, fmt.Errorf("vínculos indisponíveis")
	}
	v, err := a.vinculos.Remove(a.ctx, vinculoID)
	if err != nil {
		return nil, friendly("RemoverVinculo360", err)
	}
	if t, err := a.tickets.Get(a.ctx, v.TicketID); err == nil && t.Ticket360ID == v.Ticket360ID {
		restantes, _ := a.vinculos.List(a.ctx, v.TicketID)
		novo := db.Vinculo360{}
		if len(restantes) > 0 {
			novo = restantes[0]
		}
		if err := a.tickets.VincularTicket360(a.ctx, v.TicketID, novo.Ticket360ID, novo.Ticket360Numero, ""); err != nil {
			return nil, friendly("RemoverVinculo360", err)
		}
	}
	return a.VinculosDoCard(v.TicketID)
}

// agentPesquisarTickets lista os tickets abertos do 360 que casam com o
// termo (número, código service ou solicitante), enxuto para o tool result.
func (a *App) agentPesquisarTickets(ctx context.Context, q string) (string, string, error) {
	c := a.rotaAtual()
	if c == nil {
		return "", "", rota360.ErrSemSessao
	}
	itens, err := c.PesquisarTickets(ctx, q)
	if err != nil {
		return "", "", err
	}
	if len(itens) == 0 {
		return "Nenhum ticket aberto encontrado para " + q + ".", "pesquisa de tickets sem resultados", nil
	}
	var sb strings.Builder
	for _, t := range itens {
		fmt.Fprintf(&sb, "#%s | %s | cliente %s (código %s) | status %s | responsável %s\n",
			t.Numero, t.Titulo, t.Solicitante, t.CodigoService, t.Status, t.Responsavel)
	}
	resumo := fmt.Sprintf("pesquisou tickets %q: %d resultado(s)", q, len(itens))
	return sb.String(), resumo, nil
}

// agentDetalheTicket devolve o resumo do ticket pelo número (identificação +
// descrição + última interação de fora da sessão); o uuid é localizado nas
// filas do helpdesk. O ticket inteiro continua no 360 — abrir_ticket_360
// mostra tudo no navegador.
func (a *App) agentDetalheTicket(ctx context.Context, numero string) (string, string, error) {
	item, err := a.resolverTicket360(ctx, numero)
	if err != nil {
		return "", "", err
	}
	c := a.rotaAtual()
	if c == nil {
		return "", "", rota360.ErrSemSessao
	}
	det, err := c.DetalheTicket(ctx, item.ID)
	if err != nil {
		return "", "", err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Ticket #%s — %s [%s] | cliente %s (código %s) | assunto %s/%s | responsável %s\n\n",
		det.Numero, det.Titulo, det.Status, det.Solicitante, det.CodService, det.CategoriaNome, det.AssuntoNome, det.ResponsavelNome)
	autor := a.autorSessao360()
	if autor == "" {
		autor = det.ResponsavelNome
	}
	sb.WriteString(resumoMensagensTicket(det.Mensagens, autor))
	return sb.String(), fmt.Sprintf("leu o ticket #%s do 360", det.Numero), nil
}

func (a *App) agentListarTickets(ctx context.Context, coluna string) (string, string, error) {
	ts, err := a.tickets.List(a.ctx)
	if err != nil {
		return "", "", err
	}
	// Filtro tolerante: a IA pode citar "cobrança" para a coluna "Cobrança"
	// (ou qualquer variação de caixa/acento) — quem casa é o CasarColuna.
	var alvo string
	if coluna != "" {
		if colunas, err := a.colunas.List(a.ctx); err == nil {
			if casada, err := service.CasarColuna(coluna, colunas); err == nil {
				alvo = casada
			}
		}
		if alvo == "" {
			alvo = coluna
		}
	}
	var sb strings.Builder
	n, escritas := 0, 0
	for _, t := range ts {
		if alvo != "" && t.ColunaAtual != alvo {
			continue
		}
		n++
		if escritas < maxCardsNoResultado {
			fmt.Fprintf(&sb, "id=%d | %s | coluna=%s | cod_service=%s", t.ID, t.NomeCliente, t.ColunaAtual, t.CodService)
			if obs := strings.TrimSpace(t.Observacoes); obs != "" {
				if r := []rune(obs); len(r) > 120 {
					obs = string(r[:120]) + "…"
				}
				fmt.Fprintf(&sb, " | obs: %s", obs)
			}
			sb.WriteString("\n")
			escritas++
		}
	}
	if n == 0 {
		return "Nenhum card" + mapaSe(coluna != "", " na coluna "+coluna) + ".", "listou cards", nil
	}
	if n > escritas {
		fmt.Fprintf(&sb, "… (+%d cards não listados — peça por coluna)\n", n-escritas)
	}
	return sb.String(), fmt.Sprintf("listou %d card(s)", n), nil
}

// maxCardsNoResultado limita a listagem de cards: a linha é densa (~70
// runes) e o resultado inteiro passa pelo cap de tool result — melhor 20
// linhas inteiras do que o corte cego no meio da lista.
const maxCardsNoResultado = 20

// resolverCardID aceita o id local do card OU o número do ticket do 360: se
// o id não existe na tabela, tenta casar pelo ticket360_numero — o deslize
// mais comum da IA é passar o número do 360 no lugar do id local.
func (a *App) resolverCardID(id int64) (int64, error) {
	if _, err := a.tickets.Get(a.ctx, id); err == nil {
		return id, nil
	} else if !errors.Is(err, db.ErrTicketNaoEncontrado) {
		return 0, friendly("resolverCardID", err)
	}
	t, err := a.tickets.GetPorNumero360(a.ctx, id)
	if err != nil {
		return 0, friendly("resolverCardID", err)
	}
	if t == nil {
		return 0, fmt.Errorf("nenhum card com id %d nem ticket 360 #%d — use listar_tickets para ver os ids", id, id)
	}
	return t.ID, nil
}

func mapaSe(cond bool, s string) string {
	if cond {
		return s
	}
	return ""
}

func (a *App) agentAvaliar(ctx context.Context, ticketID int64) (string, string, error) {
	t, err := a.tickets.Get(a.ctx, ticketID)
	if err != nil {
		return "", "", err
	}
	if a.avaliador == nil {
		return "", "", fmt.Errorf("avaliador indisponível")
	}
	av, err := a.avaliador.Avaliar(ctx, ticketID, t.CodService)
	if err != nil {
		return "", "", err
	}
	return fmt.Sprintf("Cliente %s: %s", t.NomeCliente, av.ResumoUmaLinha()),
		fmt.Sprintf("avaliou o cliente %s", t.NomeCliente), nil
}

func (a *App) agentProposta(ctx context.Context, ticketID int64) (string, string, error) {
	t, err := a.tickets.Get(a.ctx, ticketID)
	if err != nil {
		return "", "", err
	}
	var in service.ReparcelamentoInput
	if cob, _ := a.cobrancas.GetAtiva(a.ctx, ticketID); cob != nil {
		in = service.ReparcelamentoInput{
			TotalAbertoCents: cob.TotalAbertoCents,
			EntradaCents:     cob.EntradaCents,
			NumeroVezes:      cob.NumeroVezes,
			ParcelaBaseCents: cob.ParcelaBaseCents,
		}
	} else {
		cli, _ := a.clientes.Get(ctx, t.CodService)
		total := int64(0)
		if cli != nil {
			total = cli.SaldoCents
		}
		if total <= 0 {
			return "", "", fmt.Errorf("card sem acordo ativo e sem saldo do 360 — importe/sincronize o cliente primeiro")
		}
		in = service.ReparcelamentoInput{TotalAbertoCents: total, NumeroVezes: 12}
	}
	prop, err := a.GenerateProposal(ticketID, in, false)
	if err != nil {
		return "", "", err
	}
	return "PROPOSTA:\n" + prop.Texto, fmt.Sprintf("gerou a proposta de %s", t.NomeCliente), nil
}

// urlTicket360 monta a URL do ticket no helpdesk do portal 360.
func urlTicket360(uuid string) string {
	return rota360.BaseURL + "/helpdesk/tickets/" + uuid
}

// agentAbrirTicket navega a aba do navegador que já está no portal 360 para
// o ticket pedido; sem essa aba, abre no navegador padrão (fallback).
func (a *App) agentAbrirTicket(ctx context.Context, numero string) (string, string, error) {
	item, err := a.resolverTicket360(ctx, numero)
	if err != nil {
		return "", "", err
	}
	url := urlTicket360(item.ID)
	modo := "aba atual do navegador"
	if err := tela.NavegarAbaAtiva(url); err != nil {
		slog.Info("navegar aba ativa falhou; abrindo no navegador padrão", "err", err)
		if err := abrirNoNavegador(url); err != nil {
			return "", "", fmt.Errorf("ticket #%s está em %s, mas não foi possível abrir o navegador: %w", item.Numero, url, err)
		}
		modo = "navegador padrão"
	}
	return fmt.Sprintf("Ticket #%s aberto em %s (via %s).", item.Numero, url, modo),
		fmt.Sprintf("abriu o ticket #%s no navegador", item.Numero), nil
}

// abrirNoNavegador abre a URL no navegador padrão do Windows.
func abrirNoNavegador(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// agentResponderTicket publica a resposta do agente no ticket do 360.
// Operação real sobre a produção: o texto chega já revisado pelo usuário.
func (a *App) agentResponderTicket(ctx context.Context, numero, texto string) (string, string, error) {
	item, err := a.resolverTicket360(ctx, numero)
	if err != nil {
		return "", "", err
	}
	c := a.rotaAtual()
	if c == nil {
		return "", "", rota360.ErrSemSessao
	}
	if err := c.ResponderTicket(ctx, item.ID, texto); err != nil {
		return "", "", err
	}
	resumo := fmt.Sprintf("respondeu o ticket #%s do 360", item.Numero)
	return "Resposta registrada no ticket #" + item.Numero + ".", resumo, nil
}

// agentAlterarStatusTicket muda o status do ticket do 360 para o nome exato
// que a API do portal usa (o agente lê os valores do detalhe do ticket).
func (a *App) agentAlterarStatusTicket(ctx context.Context, numero, status string) (string, string, error) {
	item, err := a.resolverTicket360(ctx, numero)
	if err != nil {
		return "", "", err
	}
	c := a.rotaAtual()
	if c == nil {
		return "", "", rota360.ErrSemSessao
	}
	if err := c.AlterarStatusTicket(ctx, item.ID, status); err != nil {
		return "", "", err
	}
	resumo := fmt.Sprintf("alterou o status do ticket #%s para %q", item.Numero, status)
	return fmt.Sprintf("Status do ticket #%s alterado para %s.", item.Numero, status), resumo, nil
}

// executarComandoVincular roda o /vincular (varredura de vínculos, sem IA).
// ok=false quando a mensagem não é o comando.
func (a *App) executarComandoVincular(mensagem string) (*service.ChatResultado, bool) {
	if !service.ParseComandoVincular(mensagem) {
		return nil, false
	}
	res, err := a.VincularTicketsPendentes()
	if err != nil {
		return &service.ChatResultado{Resposta: "Falhou ao vincular: " + err.Error()}, true
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "🔗 Varri %d card(s) sem ticket vinculado: %d vinculado(s).",
		res.Varridos, len(res.Vinculados))
	for _, v := range res.Vinculados {
		fmt.Fprintf(&sb, "\n• %s → ticket #%s (%s)", v.Cliente, v.TicketNumero, v.Titulo)
	}
	if len(res.SemTicket) > 0 {
		sb.WriteString("\nSem ticket na fila aberta: " + strings.Join(res.SemTicket, ", "))
	}
	for _, e := range res.Erros {
		sb.WriteString("\n⚠ " + e)
	}
	return &service.ChatResultado{
		Resposta: sb.String(),
		Acoes: []service.AcaoDoAgente{{Ferramenta: "vincular_ticket",
			Resumo: fmt.Sprintf("vinculou %d card(s) via /vincular", len(res.Vinculados))}},
	}, true
}

// cardDoContextoChat acha o cliente "em discussão" na conversa: varre as
// mensagens persistidas do chat (mais recentes primeiro) procurando um código
// service/id citado ou um nome que case com UM card do board. É o que permite
// "/move cobrança" e "/apagar" sem dizer o cliente.
func (a *App) cardDoContextoChat(chatID int64) (service.CardResumo, bool) {
	if a.chats == nil || a.tickets == nil {
		return service.CardResumo{}, false
	}
	ms, err := a.chats.Mensagens(a.ctx, chatID)
	if err != nil || len(ms) == 0 {
		return service.CardResumo{}, false
	}
	ts, err := a.tickets.List(a.ctx)
	if err != nil {
		return service.CardResumo{}, false
	}
	cards := make([]service.CardResumo, 0, len(ts))
	for _, t := range ts {
		cards = append(cards, service.CardResumo{ID: t.ID, NomeCliente: t.NomeCliente, CodService: t.CodService, ColunaAtual: t.ColunaAtual})
	}
	for i := len(ms) - 1; i >= 0; i-- {
		if card, ok := service.ResolverCardNoTexto(ms[i].Conteudo, cards); ok {
			return card, true
		}
	}
	return service.CardResumo{}, false
}

// executarComandoApagar roda o "/apagar" do chat (sem IA): com cliente no
// comando apaga o card dele; sem cliente, apaga o card do cliente em
// discussão no chat. O qualificador opcional "em X" serve de conferência —
// se o card não estiver na coluna dita, nada é apagado. ok=false quando não
// é o comando.
func (a *App) executarComandoApagar(mensagem string, chatID int64) (*service.ChatResultado, bool) {
	cmd, ok := service.ParseComandoApagar(mensagem)
	if !ok {
		return nil, false
	}
	alvo := cmd.Card
	if alvo == "" {
		card, ok := a.cardDoContextoChat(chatID)
		if !ok {
			return &service.ChatResultado{
				Resposta: "Não identifiquei o cliente em discussão nesta conversa. Diga: /apagar <cliente> (ou cite o código service antes).",
				Acoes:    []service.AcaoDoAgente{},
			}, true
		}
		alvo = card.NomeCliente
	}
	ts, err := a.tickets.List(a.ctx)
	if err != nil {
		return &service.ChatResultado{Resposta: "Não consegui listar os cards: " + err.Error(), Acoes: []service.AcaoDoAgente{}}, true
	}
	cards := make([]service.CardResumo, 0, len(ts))
	for _, t := range ts {
		cards = append(cards, service.CardResumo{ID: t.ID, NomeCliente: t.NomeCliente, CodService: t.CodService, ColunaAtual: t.ColunaAtual})
	}
	card, err := service.ResolverCard(alvo, cards)
	if err != nil {
		return &service.ChatResultado{Resposta: err.Error(), Acoes: []service.AcaoDoAgente{}}, true
	}
	return a.apagarCardResolvido(card, cmd.Coluna), true
}

// executarComandoRealizada roda o "/realizada" do chat (sem IA): com cliente
// no comando marca a visita agendada do card dele; sem cliente, do cliente em
// discussão no chat. ok=false quando não é o comando.
func (a *App) executarComandoRealizada(mensagem string, chatID int64) (*service.ChatResultado, bool) {
	cmd, ok := service.ParseComandoRealizada(mensagem)
	if !ok {
		return nil, false
	}
	alvo := cmd.Card
	if alvo == "" {
		card, ok := a.cardDoContextoChat(chatID)
		if !ok {
			return &service.ChatResultado{
				Resposta: "Não identifiquei o cliente em discussão nesta conversa. Diga: /realizada <cliente> (ou cite o código service antes).",
				Acoes:    []service.AcaoDoAgente{},
			}, true
		}
		alvo = card.NomeCliente
	}
	ts, err := a.tickets.List(a.ctx)
	if err != nil {
		return &service.ChatResultado{Resposta: "Não consegui listar os cards: " + err.Error(), Acoes: []service.AcaoDoAgente{}}, true
	}
	cards := make([]service.CardResumo, 0, len(ts))
	for _, t := range ts {
		cards = append(cards, service.CardResumo{ID: t.ID, NomeCliente: t.NomeCliente, CodService: t.CodService, ColunaAtual: t.ColunaAtual})
	}
	card, err := service.ResolverCard(alvo, cards)
	if err != nil {
		return &service.ChatResultado{Resposta: err.Error(), Acoes: []service.AcaoDoAgente{}}, true
	}
	t, err := a.MarcarVisitaRealizada(card.ID)
	if err != nil {
		return &service.ChatResultado{Resposta: "⚠ " + err.Error(), Acoes: []service.AcaoDoAgente{}}, true
	}
	return &service.ChatResultado{
		Resposta: fmt.Sprintf("✅ Visita de %s (%s %s %s — %s) marcada como **realizada**.",
			t.NomeCliente, t.VisitaData, t.VisitaTurno, t.VisitaHora, t.ColunaAtual),
		Acoes: []service.AcaoDoAgente{{Ferramenta: "marcar_visita_realizada",
			Resumo: fmt.Sprintf("marcou a visita do card %d (%s) como realizada via /realizada", t.ID, t.NomeCliente)}},
	}, true
}

// apagarCardResolvido apaga o card já identificado, conferindo antes a coluna
// dita ("em X") quando houver — evita apagar card homônimo de outra coluna.
func (a *App) apagarCardResolvido(card service.CardResumo, colunaDita string) *service.ChatResultado {
	if colunaDita != "" {
		colunas, err := a.colunas.List(a.ctx)
		if err != nil {
			return &service.ChatResultado{Resposta: "Não consegui listar as colunas: " + err.Error(), Acoes: []service.AcaoDoAgente{}}
		}
		if coluna, err := service.CasarColuna(colunaDita, colunas); err == nil && coluna != card.ColunaAtual {
			return &service.ChatResultado{
				Resposta: fmt.Sprintf("O card %d (%s) está em %q, não em %q. Nada foi apagado — confira e repita se for esse mesmo.",
					card.ID, card.NomeCliente, card.ColunaAtual, coluna),
				Acoes: []service.AcaoDoAgente{},
			}
		}
	}
	if err := a.DeleteTicket(card.ID); err != nil {
		return &service.ChatResultado{Resposta: "Falhou ao apagar: " + err.Error(), Acoes: []service.AcaoDoAgente{}}
	}
	return &service.ChatResultado{
		Resposta: fmt.Sprintf("Card %d (%s) apagado, junto com os acordos dele (estava em %q).", card.ID, card.NomeCliente, card.ColunaAtual),
		Acoes:    []service.AcaoDoAgente{{Ferramenta: "apagar_card", Resumo: fmt.Sprintf("apagou o card %d (%s, em %s) via comando local", card.ID, card.NomeCliente, card.ColunaAtual)}},
	}
}

// executarIntencaoLocal reconhece comandos rígidos de card no texto e os
// executa direto (reconhecimento local de intenção, sem API). ok=false
// devolve a mensagem ao fluxo normal do agente.
func (a *App) executarIntencaoLocal(mensagem string) (*service.ChatResultado, bool) {
	in, ok := service.ParseIntencaoCard(mensagem)
	if !ok {
		return nil, false
	}
	ts, err := a.tickets.List(a.ctx)
	if err != nil {
		return &service.ChatResultado{Resposta: "Não consegui listar os cards: " + err.Error()}, true
	}
	cards := make([]service.CardResumo, 0, len(ts))
	for _, t := range ts {
		cards = append(cards, service.CardResumo{ID: t.ID, NomeCliente: t.NomeCliente, CodService: t.CodService, ColunaAtual: t.ColunaAtual})
	}
	card, err := service.ResolverCard(in.Alvo, cards)
	if err != nil {
		return &service.ChatResultado{Resposta: err.Error()}, true
	}
	switch {
	case in.Apagar:
		// "apague o card do João que está em Cobrança": confere a coluna
		// dita antes de destruir — evita apagar card homônimo de outra
		// coluna e responde onde o card realmente está.
		return a.apagarCardResolvido(card, in.Coluna), true
	default:
		colunas, err := a.colunas.List(a.ctx)
		if err != nil {
			return &service.ChatResultado{Resposta: "Não consegui listar as colunas: " + err.Error()}, true
		}
		coluna, err := service.CasarColuna(in.Coluna, colunas)
		if err != nil {
			return &service.ChatResultado{Resposta: err.Error()}, true
		}
		in.Coluna = coluna
		if err := a.MoveTicket(card.ID, in.Coluna, a.contagemColuna(in.Coluna)); err != nil {
			return &service.ChatResultado{Resposta: "Falhou ao mover: " + err.Error()}, true
		}
		return &service.ChatResultado{
			Resposta: fmt.Sprintf("Card %d (%s) movido para %s.", card.ID, card.NomeCliente, in.Coluna),
			Acoes:    []service.AcaoDoAgente{{Ferramenta: "mover_card", Resumo: fmt.Sprintf("moveu o card %d (%s) para %s via comando local", card.ID, card.NomeCliente, in.Coluna)}},
		}, true
	}
}
