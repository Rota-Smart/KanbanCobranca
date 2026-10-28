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
		{"mova o card do João para Cobrança", ConsultaNenhuma},
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

	// Sem alvo: as tools de localização ficam (o modelo ainda precisa achar
	// o cliente sozinho pelo fluxo antigo).
	rem = FerramentasRemovidas(ConsultaOS, false)
	for _, f := range rem {
		if f == "buscar_cliente_360" {
			t.Errorf("sem alvo, buscar_cliente_360 deve continuar")
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
