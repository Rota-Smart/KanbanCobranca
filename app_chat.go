package main

import (
	"context"
	"database/sql"
	"encoding/json"
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
	if a.groq == nil || a.groq.Chave() == "" {
		return service.ChatResultado{}, fmt.Errorf("configure a chave da Groq em ⚙ Configurações para usar o assistente")
	}
	if a.chats == nil {
		return service.ChatResultado{}, fmt.Errorf("conversas indisponíveis")
	}
	mensagem = strings.TrimSpace(mensagem)
	if mensagem == "" {
		return service.ChatResultado{}, fmt.Errorf("mensagem vazia")
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
	res, err := a.groq.Conversar(ctx, historico, mensagem, a.textoPadroes(a.ctx), a, nil)
	if err != nil {
		if err == service.ErrGroqSemChave {
			return service.ChatResultado{}, err
		}
		return service.ChatResultado{}, friendly("EnviarChat", err)
	}
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
		id, coluna := argInt("ticketId"), argStr("coluna")
		if id <= 0 || coluna == "" {
			return "", "", fmt.Errorf("ticketId e coluna são obrigatórios")
		}
		if err := a.MoveTicket(id, coluna, a.contagemColuna(coluna)); err != nil {
			return "", "", err
		}
		extra := ""
		if coluna == service.ColunaCobranca {
			extra = " (fluxo de cobrança disparado no próximo acesso pelo usuário)"
		}
		return fmt.Sprintf("Card %d movido para %s%s.", id, coluna, extra),
			fmt.Sprintf("moveu o card %d para %s", id, coluna), nil

	case "avaliar_cliente":
		id := argInt("ticketId")
		if id <= 0 {
			return "", "", fmt.Errorf("ticketId inválido")
		}
		return a.agentAvaliar(ctx, id)

	case "gerar_proposta":
		id := argInt("ticketId")
		if id <= 0 {
			return "", "", fmt.Errorf("ticketId inválido")
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
		if id := argInt("ticketId"); id > 0 {
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
	return *salvo, nil
}

func limitarTexto(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (a *App) agentFinanceiro(ctx context.Context, cod string) (string, string, error) {
	c := a.rotaAtual()
	if c == nil {
		return "", "", rota360.ErrSemSessao
	}
	fin, err := c.FinanceiroCliente(ctx, cod)
	if err != nil {
		return "", "", err
	}
	dados := fin.Sources.InsideService.Data
	f := dados.Financeiro
	var sb strings.Builder
	fmt.Fprintf(&sb, "Cliente %s — saldo devedor: R$ %.2f (com encargos R$ %.2f; encargos R$ %.2f); %d fatura(s) atrasada(s).\n",
		cod, f.TotalDevendo, f.TotalDevendoComEncargos, f.TotalEncargosAtraso, f.TotalAtrasadas)
	for _, fat := range dados.FaturasAbertas {
		fmt.Fprintf(&sb, "Doc %s: R$ %.2f, venceu %s, %d dias de atraso (multa R$ %.2f, mora R$ %.2f)\n",
			fat.NumeroDoc, fat.ValorDuplicata, fat.Vencimento.Format("02/01/2006"), fat.DiasAtraso, fat.MultaValor, fat.MoraValor)
	}
	if n := len(dados.OSAtivas); n > 0 {
		sb.WriteString("\nOS ativas:\n")
		for i, os := range dados.OSAtivas {
			if i >= 10 {
				fmt.Fprintf(&sb, "… (+%d mais)\n", n-10)
				break
			}
			fmt.Fprintf(&sb, "- %s\n", resumoMapa(os, "numero", "descricao", "status", "data", "situacao"))
		}
	} else {
		sb.WriteString("\nSem OS ativas no cadastro.\n")
	}
	sb.WriteString("\n" + resumoEquipamentos(fin.Sources.Produtos.Data))
	if dc, err := c.DadosCliente(ctx, cod); err == nil && strings.TrimSpace(dc.Observacoes) != "" {
		sb.WriteString("\nObservações do cadastro:\n" + limitarTexto(dc.Observacoes, 1500) + "\n")
	}
	if rec := fin.Sources.Octadesk.Data.TicketsRecentes; len(rec) > 0 {
		sb.WriteString("\nAtendimentos recentes (Octadesk):\n")
		for i, t := range rec {
			if i >= 8 {
				fmt.Fprintf(&sb, "… (+%d mais)\n", len(rec)-8)
				break
			}
			fmt.Fprintf(&sb, "- #%s %s [%s] (%s)\n", t.Number, t.Summary, t.Status.Name, t.CreatedAt)
		}
	}
	return sb.String(), fmt.Sprintf("consultou os dados do cliente %s", cod), nil
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
// ("-ALARME-" ou "-CFTV-"), então a classificação é heurística.
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
			sensores += p.Quantidade
		case strings.Contains(d, "CONTROLE"):
			controles += p.Quantidade
		case ehCamera:
			cameras += p.Quantidade
		}
		for _, m := range regexModeloCamera.FindAllString(d, -1) {
			if !modelos[m] {
				modelos[m] = true
				ordemModelos = append(ordemModelos, m)
			}
		}
		if len(linhas) < 20 {
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
	if len(prods) > 20 {
		fmt.Fprintf(&sb, "\n… (+%d mais)", len(prods)-20)
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

// agentDetalheTicket devolve o ticket completo (descrição + interações) pelo
// número; o uuid é localizado nas filas do helpdesk.
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
	for _, m := range det.Mensagens {
		if m.Tipo == "sistema" || strings.TrimSpace(m.Corpo) == "" {
			continue
		}
		tipo := m.Tipo
		if tipo == "descricao" {
			tipo = "descrição"
		}
		fmt.Fprintf(&sb, "[%s] %s: %s\n\n", tipo, m.Autor, limitarTexto(m.Corpo, 700))
	}
	return sb.String(), fmt.Sprintf("leu o ticket #%s do 360", det.Numero), nil
}

func (a *App) agentListarTickets(ctx context.Context, coluna string) (string, string, error) {
	ts, err := a.tickets.List(a.ctx)
	if err != nil {
		return "", "", err
	}
	var sb strings.Builder
	n := 0
	for _, t := range ts {
		if coluna != "" && t.ColunaAtual != coluna {
			continue
		}
		n++
		fmt.Fprintf(&sb, "id=%d | %s | coluna=%s | cod_service=%s\n", t.ID, t.NomeCliente, t.ColunaAtual, t.CodService)
	}
	if n == 0 {
		return "Nenhum card" + mapaSe(coluna != "", " na coluna "+coluna) + ".", "listou cards", nil
	}
	return sb.String(), fmt.Sprintf("listou %d card(s)", n), nil
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
