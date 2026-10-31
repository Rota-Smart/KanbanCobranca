package service

import "testing"

func TestClassificarConsulta(t *testing.T) {
	casos := []struct {
		msg  string
		want ClasseConsulta
	}{
		{"qual o financeiro do cliente 27257?", ConsultaFinanceiro},
		{"quantas OS o cliente João tem?", ConsultaOS},
		{"quantos atendimentos concluídos ele tem?", ConsultaOS},
		{"que câmeras o cliente tem instaladas?", ConsultaEquipamentos},
		{"tem sensor de abertura cadastrado?", ConsultaEquipamentos},
		// Duas fatias na mesma mensagem = cliente completo.
		{"qual o saldo e as OS do cliente 1234?", ConsultaCliente},
		{"me dá o resumo do cliente: faturas, equipamentos e OS", ConsultaCliente},
		{"o que responderam no ticket 45231?", ConsultaTicket},
		// Board: agregação + coluna vence mesmo com palavra de financeiro
		// ("renegociação") na frase — o escopo é o quadro, não um cliente.
		{"verifique todos os clientes da coluna cobrança e quais possuem renegociação", ConsultaBoard},
		{"liste os cards da coluna Cobrança", ConsultaBoard},
		{"quantos clientes na coluna Contato?", ConsultaBoard},
		// "coluna" sozinho não basta. Ação de mover agora classifica como
		// acao_card (roteia as tools); na prática o comando é executado
		// antes pela intenção local, sem passar pela classificação.
		{"qual a coluna do cliente 1234?", ConsultaNenhuma},
		{"mova o card do João para Cobrança", AcaoCard},
		{"melhore esse texto: ...", ConsultaNenhuma},
		{"quais as ferramentas disponíveis?", ConsultaNenhuma},
	}
	for _, c := range casos {
		if got := ClassificarConsulta(c.msg); got != c.want {
			t.Errorf("ClassificarConsulta(%q) = %q; quero %q", c.msg, got, c.want)
		}
	}
}

func TestExtrairAlvo(t *testing.T) {
	casos := []struct {
		msg  string
		want AlvoConsulta
	}{
		{"financeiro do cliente 27257", AlvoConsulta{Tipo: "cod", Valor: "27257"}},
		{"o que tem no ticket 45231?", AlvoConsulta{Tipo: "ticket", Valor: "45231"}},
		{"interações do #9876", AlvoConsulta{Tipo: "ticket", Valor: "9876"}},
		{`quantas OS o cliente "João da Silva" tem?`, AlvoConsulta{Tipo: "nome", Valor: "João da Silva"}},
		{"quantas OS o cliente João da Silva tem?", AlvoConsulta{Tipo: "nome", Valor: "João da Silva"}},
		{"código service 1234: saldo", AlvoConsulta{Tipo: "cod", Valor: "1234"}},
		{"e o financeiro?", AlvoConsulta{}},
	}
	for _, c := range casos {
		got := ExtrairAlvo(c.msg)
		if got != c.want {
			t.Errorf("ExtrairAlvo(%q) = %+v; quero %+v", c.msg, got, c.want)
		}
	}
}

func TestFerramentasRemovidas(t *testing.T) {
	// Financeiro pedido: só a fatia financeira sai; OS/equipamentos ficam
	// disponíveis para follow-up (o filtro é o que ENTRA no contexto).
	rem := FerramentasRemovidas(ConsultaFinanceiro, true)
	tem := map[string]bool{}
	for _, f := range rem {
		tem[f] = true
	}
	if !tem["financeiro_cliente"] || !tem["buscar_cliente_360"] || !tem["listar_colunas"] {
		t.Errorf("financeiro deveria remover financeiro/buscar/colunas, veio %v", rem)
	}
	if tem["atendimentos_cliente"] || tem["equipamentos_cliente"] {
		t.Errorf("fatias não pedidas não devem sair do request: %v", rem)
	}

	// Sem alvo: nada sai do request — o bloco pode ser sobre o último ticket
	// capturado, mas a pergunta pode alcançar outros clientes.
	rem = FerramentasRemovidas(ConsultaOS, false)
	if rem != nil {
		t.Errorf("sem alvo, nada deve sair do request; veio %v", rem)
	}

	// Board: a listagem de cards já veio mastigada no contexto; colunas e
	// busca de cliente também são redundantes (a coluna já foi resolvida).
	rem = FerramentasRemovidas(ConsultaBoard, true)
	tem = map[string]bool{}
	for _, f := range rem {
		tem[f] = true
	}
	if !tem["listar_tickets"] || tem["financeiro_cliente"] {
		t.Errorf("board deveria remover listar_tickets (só), veio %v", rem)
	}
}

func TestExtrairColunaBoard(t *testing.T) {
	casos := []struct {
		msg  string
		want string
	}{
		{"verifique todos os clientes da coluna cobrança e quais possuem renegociação", "cobrança"},
		{"liste os cards da coluna Cobrança", "Cobrança"},
		{"quantos clientes na coluna Ticket Fechado?", "Ticket Fechado"},
		{"olhe todos os cards do quadro", ""},
	}
	for _, c := range casos {
		if got := ExtrairColunaBoard(c.msg); got != c.want {
			t.Errorf("ExtrairColunaBoard(%q) = %q; quero %q", c.msg, got, c.want)
		}
	}
}

func TestAgentToolsSem(t *testing.T) {
	todas := AgentTools()
	restantes := AgentToolsSem([]string{"financeiro_cliente", "equipamentos_cliente"})
	if len(restantes) != len(todas)-2 {
		t.Fatalf("esperava %d tools, veio %d", len(todas)-2, len(restantes))
	}
	for _, tool := range restantes {
		nome := tool["function"].(map[string]any)["name"].(string)
		if nome == "financeiro_cliente" || nome == "equipamentos_cliente" {
			t.Errorf("tool %s deveria ter sido removida", nome)
		}
	}
}

func TestClassificarBoardSemPalavraColuna(t *testing.T) {
	casos := []struct {
		msg    string
		want   ClasseConsulta
		coluna string
	}{
		{"quais cards em Cobrança?", ConsultaBoard, "Cobrança"},
		{"quem está na Cobrança?", ConsultaBoard, "Cobrança"},
		{"quem tá no Ticket Fechado", ConsultaBoard, "Ticket Fechado"},
		{"liste os cards", ConsultaBoard, ""},
		{"todos os cards do quadro", ConsultaBoard, ""},
		{"cards em Orçamento", ConsultaBoard, "Orçamento"},
		// negativos: não podem virar consulta de quadro
		{"o financeiro do cliente 12345", ConsultaFinanceiro, ""},
		{"qual o saldo do cliente João?", ConsultaFinanceiro, ""},
		{"quantos boletos venceram esse mês", ConsultaFinanceiro, ""},
		{"mova o card do João para Cobrança", AcaoCard, ""},
		{"remova o padrão antigo", AcaoPadroes, ""},
	}
	for _, c := range casos {
		if got := ClassificarConsulta(c.msg); got != c.want {
			t.Errorf("ClassificarConsulta(%q) = %q; quero %q", c.msg, got, c.want)
			continue
		}
		if c.want == ConsultaBoard {
			if got := ExtrairColunaBoard(c.msg); got != c.coluna {
				t.Errorf("ExtrairColunaBoard(%q) = %q; quero %q", c.msg, got, c.coluna)
			}
		}
	}
}

// ---- Classes de ação: roteamento de tools por intenção ----

func TestClassificarAcoes(t *testing.T) {
	casos := []struct {
		msg  string
		want ClasseConsulta
	}{
		{"responda o ticket 1234", AcaoTicket},
		{"abra o ticket 45 no navegador", AcaoTicket},
		{"mude o status do ticket 123 para Concluído", AcaoTicket},
		{"verifique os tickets fechados", AcaoTicket},
		// consulta de ticket (leitura) vence a ação:
		{"o que responderam no ticket 1234?", ConsultaTicket},
		{"registre a comissão do card 5", AcaoComissao},
		{"quanto já recebi de comissão?", AcaoComissao},
		{"gere um orçamento de CFTV com 4 câmeras", AcaoOrcamento},
		{"liste a tabela de preços", AcaoOrcamento},
		{"salve esse padrão de resposta", AcaoPadroes},
		{"remova o padrão antigo", AcaoPadroes},
		{"quais são os meus padrões?", AcaoPadroes},
		{"adicione o cliente João da Silva", AcaoImportar},
		{"importe o código 12345", AcaoImportar},
		{"mova o card do João para Cobrança", AcaoCard},
		{"apague o card 5", AcaoCard},
		{"agende uma visita no card 3", AcaoCard},
		// mensagens ambíguas NÃO são classificadas — vão com tools completas.
		{"olá, tudo bem?", ConsultaNenhuma},
		{"melhore esse texto: ...", ConsultaNenhuma},
		{"qual a coluna do cliente 1234?", ConsultaNenhuma},
		// consulta de dados vence (prefetch tem prioridade):
		{"qual o orçamento que ele tem de equipamentos?", ConsultaEquipamentos},
	}
	for _, c := range casos {
		if got := ClassificarConsulta(c.msg); got != c.want {
			t.Errorf("ClassificarConsulta(%q) = %q; quero %q", c.msg, got, c.want)
		}
	}
}

func TestFerramentasForaDaClasse(t *testing.T) {
	todas := len(AgentTools())
	fora := FerramentasForaDaClasse(AcaoCard)
	if len(fora) == 0 || len(fora) >= todas {
		t.Fatalf("AcaoCard: fora=%d, todas=%d", len(fora), todas)
	}
	// mover_card precisa continuar dentro.
	permitidas := map[string]bool{}
	for _, f := range AgentTools() {
		permitidas[f["function"].(map[string]any)["name"].(string)] = true
		for _, rem := range fora {
			if f["function"].(map[string]any)["name"].(string) == rem {
				delete(permitidas, rem)
			}
		}
	}
	for _, precisa := range []string{"mover_card", "apagar_card", "listar_tickets"} {
		if !permitidas[precisa] {
			t.Errorf("AcaoCard: %q foi removida e não devia", precisa)
		}
	}
	// gerar_proposta não pode sair na classe de card (fluxo Cobrança).
	if !permitidas["gerar_proposta"] {
		t.Error("AcaoCard: gerar_proposta removida e não devia")
	}
	// responder_ticket_360 fora da classe de card; dentro da de ticket.
	foraCard := map[string]bool{}
	for _, f := range FerramentasForaDaClasse(AcaoCard) {
		foraCard[f] = true
	}
	if !foraCard["responder_ticket_360"] {
		t.Error("AcaoCard: responder_ticket_360 devia sair")
	}
	for _, f := range FerramentasForaDaClasse(AcaoTicket) {
		if f == "responder_ticket_360" {
			t.Error("AcaoTicket: responder_ticket_360 devia ficar")
		}
	}
	// classe sem conjunto → nada sai (fallback seguro).
	if got := FerramentasForaDaClasse(ConsultaNenhuma); got != nil {
		t.Errorf("ConsultaNenhuma: fora = %v; quero nil", got)
	}
	if !EhAcao(AcaoCard) || EhAcao(ConsultaBoard) {
		t.Error("EhAcao inconsistente")
	}
}
