package service

import "testing"

func TestParseIntencaoCard(t *testing.T) {
	cases := []struct {
		msg    string
		quer   bool
		apagar bool
		alvo   string
		coluna string
	}{
		{"apague o card do João", true, true, "do João", ""},
		{"Apagar card 12345.", true, true, "12345", ""},
		{"exclua o card da Maria", true, true, "da Maria", ""},
		{"elimine o cliente João", true, true, "João", ""},
		{"remova o ticket do João", true, true, "do João", ""},
		{"tire o card 12345", true, true, "12345", ""},
		{"apague o card do João que está em Cobrança", true, true, "do João", "Cobrança"},
		{"apague o card da Maria, tá na Visita Agendada", true, true, "da Maria", "Visita Agendada"},
		{"exclua o card do João em Negociação.", true, true, "do João", "Negociação"},
		// nome de cliente com "de" minúsculo não vira qualificador
		{"apague o card da empresa de sondagem", true, true, "da empresa de sondagem", ""},
		{"mova o card do João para Cobrança", true, false, "do João", "Cobrança"},
		{"mover card #456 para a coluna Orçamento", true, false, "#456", "Orçamento"},
		{"passe o card 123 pra Negociação", true, false, "123", "Negociação"},
		{"coloque o cliente João em Cobrança", true, false, "João", "Cobrança"},
		{"coloque o card do João na Cobrança", true, false, "do João", "Cobrança"},
		{"coloca o cliente 12345 na coluna cobrança.", true, false, "12345", "cobrança"},
		{"deixe o ticket do João no Orçamento", true, false, "do João", "Orçamento"},
		{"ponha o card #456 em Aguardando", true, false, "#456", "Aguardando"},
		{"coloque observações no card", false, false, "", ""}, // sem card/cliente/ticket → agente
		{"mova o card para Cobrança", false, false, "", ""},   // sem alvo
		{"busque o cliente João", false, false, "", ""},
		{"como você funciona?", false, false, "", ""},
	}
	for _, tc := range cases {
		in, ok := ParseIntencaoCard(tc.msg)
		if ok != tc.quer {
			t.Errorf("%q: ok=%v, quer %v", tc.msg, ok, tc.quer)
			continue
		}
		if !ok {
			continue
		}
		if in.Apagar != tc.apagar || in.Alvo != tc.alvo || in.Coluna != tc.coluna {
			t.Errorf("%q: veio %+v, quer apagar=%v alvo=%q coluna=%q", tc.msg, in, tc.apagar, tc.alvo, tc.coluna)
		}
	}
}

func TestParseComandoMove(t *testing.T) {
	url := "https://360.rotagroup.com.br/helpdesk/tickets/abc-123-def"
	cases := []struct {
		msg    string
		quer   bool
		ticket string
		card   string
		coluna string
	}{
		{"/move 12345 para Cobrança", true, "12345", "", "Cobrança"},
		{"/move #12345 pro Orçamento.", true, "#12345", "", "Orçamento"},
		{"/Mover 12345 Cobrança", true, "12345", "", "Cobrança"},
		{"/movimentar " + url + " para Aguardando", true, url, "", "Aguardando"},
		{"/move João da Silva para Cobrança", true, "", "João da Silva", "Cobrança"},
		{"/move João na Cobrança", true, "", "João", "Cobrança"},
		{"/move 12345", true, "12345", "", ""},
		{"/move", true, "", "", ""},
		{"/move para Cobrança", true, "", "Cobrança", ""},        // sem alvo: o app pede a coluna
		{"mova o card do João para Cobrança", false, "", "", ""}, // fluxo antigo
		{"/apague o card do João", false, "", "", ""},
		{"", false, "", "", ""},
	}
	for _, tc := range cases {
		cmd, ok := ParseComandoMove(tc.msg)
		if ok != tc.quer {
			t.Errorf("%q: ok=%v, quer %v", tc.msg, ok, tc.quer)
			continue
		}
		if !ok {
			continue
		}
		if cmd.Ticket != tc.ticket || cmd.Card != tc.card || cmd.Coluna != tc.coluna {
			t.Errorf("%q: veio %+v, quer ticket=%q card=%q coluna=%q", tc.msg, cmd, tc.ticket, tc.card, tc.coluna)
		}
	}
}

func TestCasarColuna(t *testing.T) {
	colunas := []string{"Fazer contato", "Cobrança", "Aguardando", "Visita Agendada", "Orçamento"}
	casos := []struct {
		texto string
		quer  string
		falha bool
	}{
		{"Cobrança", "Cobrança", false},
		{"cobranca", "Cobrança", false},
		{"COBRANÇA.", "Cobrança", false},
		{"cobr", "Cobrança", false},
		{"visita agendada", "Visita Agendada", false},
		{"a coluna Cobrança", "Cobrança", false},
		{"coluna orcamento", "Orçamento", false},
		{"orcamento", "Orçamento", false},
		{"a", "", true}, // casa "Aguardando" e "Fazer contato" → ambíguo
		{"xyz", "", true},
		{"", "", true},
	}
	for _, tc := range casos {
		got, err := CasarColuna(tc.texto, colunas)
		if tc.falha {
			if err == nil {
				t.Errorf("%q: queria erro, veio %q", tc.texto, got)
			}
			continue
		}
		if err != nil || got != tc.quer {
			t.Errorf("%q: veio %q (%v), quer %q", tc.texto, got, err, tc.quer)
		}
	}
}

func TestResolverCard(t *testing.T) {
	cards := []CardResumo{
		{1, "João da Silva", "123", ""},
		{2, "Maria João", "456", ""},
		{3, "Joaquim", "", ""},
	}
	if c, err := ResolverCard("123", cards); err != nil || c.ID != 1 {
		t.Errorf("por código: %+v err=%v", c, err)
	}
	if c, err := ResolverCard("#456", cards); err != nil || c.ID != 2 {
		t.Errorf("por #código: %+v err=%v", c, err)
	}
	if _, err := ResolverCard("99", cards); err == nil {
		t.Error("número que não é código nem id deveria falhar")
	}
	if c, err := ResolverCard("joaquim", cards); err != nil || c.ID != 3 {
		t.Errorf("por nome: %+v err=%v", c, err)
	}
	if _, err := ResolverCard("jo", cards); err == nil {
		t.Error("dois matches (João/Maria João/Joaquim) deveria ser ambíguo")
	}
	if _, err := ResolverCard("zé", cards); err == nil {
		t.Error("sem match deveria falhar")
	}
}

func TestParseComandoApagar(t *testing.T) {
	cases := []struct {
		msg    string
		quer   bool
		card   string
		coluna string
	}{
		{"/apagar", true, "", ""},
		{"/apagar João", true, "João", ""},
		{"/apagar João em Cobrança", true, "João", "Cobrança"},
		{"/apagar #24755 na Cobrança.", true, "#24755", "Cobrança"},
		{"/apague o card do João da Silva", true, "card do João da Silva", ""},
		{"/excluir 12345", true, "12345", ""},
		{"/delete João em Cobrança em Negociando", true, "João em Cobrança", "Negociando"},
		{"/remova.", true, "", ""},
		{"apague o card do João", false, "", ""},     // fluxo antigo, sem barra
		{"/move 12345 para Cobrança", false, "", ""}, // outro comando
		{"", false, "", ""},
	}
	for _, tc := range cases {
		cmd, ok := ParseComandoApagar(tc.msg)
		if ok != tc.quer {
			t.Errorf("%q: ok=%v, quer %v", tc.msg, ok, tc.quer)
			continue
		}
		if !ok {
			continue
		}
		if cmd.Card != tc.card || cmd.Coluna != tc.coluna {
			t.Errorf("%q: veio %+v, quer card=%q coluna=%q", tc.msg, cmd, tc.card, tc.coluna)
		}
	}
}

func TestResolverCardNoTexto(t *testing.T) {
	cards := []CardResumo{
		{ID: 1, NomeCliente: "Resolute Distribuidora", CodService: "24755"},
		{ID: 2, NomeCliente: "Mercado Bom Preço", CodService: "11223"},
		{ID: 3, NomeCliente: "João Batista", CodService: "9988"},
	}
	casos := []struct {
		texto string
		quer  int64
	}{
		{"qual o financeiro do 24755?", 1},
		{"o card #1 está atrasado", 1},
		{"vamos mover o Resolute Distribuidora", 1},
		{"clientes: Mercado Bom Preço e outros", 2},
		{"falei do Batista ontem", 3},         // pedaço único do nome
		{"a cliente Rejane reclamou", 0},      // nada casa
		{"os dois clientes 24755 e 11223", 1}, // dois códigos: o primeiro citado vence
	}
	for _, tc := range casos {
		card, ok := ResolverCardNoTexto(tc.texto, cards)
		if tc.quer == 0 {
			if ok {
				t.Errorf("%q: casou %+v, queria ambíguo/nao", tc.texto, card)
			}
			continue
		}
		if !ok || card.ID != tc.quer {
			t.Errorf("%q: veio %+v (ok=%v), quer card %d", tc.texto, card, ok, tc.quer)
		}
	}
	if card, ok := ResolverCardNoTexto("os dois clientes 24755 e 11223", cards); !ok || card.ID != 1 {
		t.Errorf("dois códigos: quer card 1 (primeiro citado), veio %+v ok=%v", card, ok)
	}
}

func TestParseIntencaoAbrir(t *testing.T) {
	casos := []struct {
		msg  string
		want string
	}{
		{"abra o cliente João", "João"},
		{"abre o card do cliente Maria Souza", "Maria Souza"},
		{"abra o card do 24755", "24755"},
		{"mostre o ticket 12345", "12345"},
		{"abrir o cliente XPTO.", "XPTO"},
		{"verifique se o cliente João tem card", "João"},
		{"o ticket 55555 tem card?", "55555"},
		{"confere se a Maria tem card", "Maria"},
		// negativos: não são abrir card
		{"abra o orçamento do João", ""},
		{"abra a planilha", ""},
		{"o cliente pagou o card?", ""},
	}
	for _, c := range casos {
		in, ok := ParseIntencaoAbrir(c.msg)
		if c.want == "" {
			if ok {
				t.Errorf("ParseIntencaoAbrir(%q) reconheceu alvo %q; devia ignorar", c.msg, in.Alvo)
			}
			continue
		}
		if !ok {
			t.Errorf("ParseIntencaoAbrir(%q) não reconheceu; quero %q", c.msg, c.want)
			continue
		}
		if in.Alvo != c.want {
			t.Errorf("ParseIntencaoAbrir(%q) = %q; quero %q", c.msg, in.Alvo, c.want)
		}
	}
}
