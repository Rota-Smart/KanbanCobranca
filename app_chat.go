package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"rota-smart/internal/db"
	"rota-smart/internal/rota360"
	"rota-smart/internal/service"
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

// ---- Chat com o agente Groq ----

// EnviarChat manda a mensagem (com o histórico recente do frontend) para o
// agente Groq, que pode executar ferramentas: buscar clientes no 360,
// adicionar cards, criar colunas, mover cards, consultar financeiro etc.
func (a *App) EnviarChat(historico []service.MensagemDeChat, mensagem string) (service.ChatResultado, error) {
	if a.groq == nil || a.groq.Chave() == "" {
		return service.ChatResultado{}, fmt.Errorf("configure a chave da Groq em ⚙ Configurações para usar o assistente")
	}
	mensagem = strings.TrimSpace(mensagem)
	if mensagem == "" {
		return service.ChatResultado{}, fmt.Errorf("mensagem vazia")
	}
	if len(historico) > 10 {
		historico = historico[len(historico)-10:]
	}
	ctx, cancel := context.WithTimeout(a.ctx, 90*time.Second)
	defer cancel()
	res, err := a.groq.Conversar(ctx, historico, mensagem, a, nil)
	if err != nil {
		if err == service.ErrGroqSemChave {
			return service.ChatResultado{}, err
		}
		return service.ChatResultado{}, friendly("EnviarChat", err)
	}
	if res.Acoes == nil {
		res.Acoes = []service.AcaoDoAgente{}
	}
	return *res, nil
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

	case "financeiro_cliente":
		cod := argStr("codService")
		if cod == "" {
			return "", "", fmt.Errorf("codService vazio")
		}
		return a.agentFinanceiro(ctx, cod)

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

	default:
		return "", "", fmt.Errorf("ferramenta desconhecida: %s", ferramenta)
	}
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
	f := fin.Sources.InsideService.Data.Financeiro
	var sb strings.Builder
	fmt.Fprintf(&sb, "Cliente %s — saldo devedor: R$ %.2f (com encargos R$ %.2f; encargos R$ %.2f); %d fatura(s) atrasada(s).\n",
		cod, f.TotalDevendo, f.TotalDevendoComEncargos, f.TotalEncargosAtraso, f.TotalAtrasadas)
	for _, fat := range fin.Sources.InsideService.Data.FaturasAbertas {
		fmt.Fprintf(&sb, "Doc %s: R$ %.2f, venceu %s, %d dias de atraso (multa R$ %.2f, mora R$ %.2f)\n",
			fat.NumeroDoc, fat.ValorDuplicata, fat.Vencimento.Format("02/01/2006"), fat.DiasAtraso, fat.MultaValor, fat.MoraValor)
	}
	return sb.String(), fmt.Sprintf("consultou o financeiro do cliente %s", cod), nil
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
