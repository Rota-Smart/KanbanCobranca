package main

import (
	"strings"
	"testing"

	"rota-smart/internal/db"
	"rota-smart/internal/service"
)

// Importação de ponta a ponta da lista real (a que motivou o recurso) com
// cards no quadro: fechados vão para Ticket Fechado com o motivo gravado,
// em tratamento vão para a coluna escolhida, ambíguo não mexe e linha sem
// card falha com a explicação (sem sessão do 360 nos testes).
const listaReal = `SELVINO ALOISIO CLOSSMANN - TROCA DE ENDEREÇO - CANCELADO
ROSANE BORCHARDT VOGT - INSATISFAÇÃO COM ATENDIMENTO (JÁ RESOLVIDO) - REVERTIDO
ROSANE ELIA PAUSE - NÃO SABEMOS O MOTIVO
LUIS FERNANDO DA ROSA & CIA LTDA ME - INSATIFAÇÃO COM SERVIÇOS
ROSANE SANTOS - OUTRO MOTIVO
ZE SEM CARD - NÃO SABEMOS O MOTIVO`

func TestImportarListaCancelamentos(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.colunas.Add(a.ctx, ColunaTicketFechado); err != nil {
		t.Fatalf("criar coluna: %v", err)
	}
	criar := func(nome, coluna string) db.Ticket {
		t.Helper()
		c, err := a.SaveTicket(db.Ticket{NomeCliente: nome, TipoTicket: "Outro", ColunaAtual: coluna})
		if err != nil {
			t.Fatalf("criar card %s: %v", nome, err)
		}
		return c
	}
	criar("Selvino Aloisio Clossmann", "Cobrança")
	criar("Rosane Borchardt Vogt", "Cobrança")
	criar("Rosane Elia Pause", "Cobrança")
	// Duas variantes do mesmo cliente no quadro: as duas linhas da lista
	// ("& CIA LTDA ME" e "& CIA LTDA") casam o mesmo card.
	criar("Luis Fernando da Rosa & Cia Ltda ME", "Cobrança")

	previa, err := a.PreviaImportarCancelamentos(listaReal)
	if err != nil {
		t.Fatalf("Prévia: %v", err)
	}
	if len(previa.Cancelados) != 1 || len(previa.Revertidos) != 1 || len(previa.Tratamento) != 2 {
		t.Fatalf("prévia errada: %+v", previa)
	}
	if len(previa.Ambiguos) != 1 || len(previa.Ambiguos[0].Candidatos) != 2 {
		t.Fatalf("ROSANE SANTOS deveria ser ambíguo (dois Rosane): %+v", previa.Ambiguos)
	}
	if len(previa.SemCard) != 1 || previa.SemCard[0].Nome != "ZE SEM CARD" {
		t.Fatalf("sem card: %+v", previa.SemCard)
	}

	res, err := a.ImportarCancelamentos(listaReal, "Cobrança")
	if err != nil {
		t.Fatalf("Importar: %v", err)
	}
	// Registrados = os 4 com card; movidos = os 2 fechados (Selvino e Rosane
	// Borchardt); os 2 em tratamento já estavam na coluna escolhida.
	if res.Registrados != 4 || res.Movidos != 2 || res.JaFechados != 2 || len(res.Falhas) != 2 {
		t.Fatalf("resumo: %+v", res)
	}
	colunaDe := func(nome string) string {
		t.Helper()
		ts, err := a.GetTickets()
		if err != nil {
			t.Fatalf("GetTickets: %v", err)
		}
		for _, x := range ts {
			if x.NomeCliente == nome {
				return x.ColunaAtual
			}
		}
		t.Fatalf("card %s não encontrado", nome)
		return ""
	}
	// Fechado (CANCELADO) foi para Ticket Fechado; em tratamento ficou/moveu
	// para a coluna escolhida (Cobrança — já estava, conta como já na coluna).
	if colunaDe("Selvino Aloisio Clossmann") != ColunaTicketFechado {
		t.Errorf("cancelado deveria estar em Ticket Fechado")
	}
	if colunaDe("Rosane Elia Pause") != "Cobrança" {
		t.Errorf("em tratamento deveria ir para a coluna escolhida")
	}
	// Motivos gravados com o resultado certo.
	ts, _ := a.GetTickets()
	for _, x := range ts {
		c, err := a.cancelamentos.CancelarDoTicket(a.ctx, x.ID)
		if err != nil || c == nil {
			t.Fatalf("cancelamento do card %s: %+v err=%v", x.NomeCliente, c, err)
		}
		switch x.NomeCliente {
		case "Selvino Aloisio Clossmann":
			if c.Resultado != service.CancelamentoPerda || c.Motivo != "TROCA DE ENDEREÇO" {
				t.Errorf("selvino: %+v", c)
			}
		case "Rosane Borchardt Vogt":
			if c.Resultado != service.CancelamentoReversao {
				t.Errorf("rosane b: %+v", c)
			}
		case "Rosane Elia Pause", "Luis Fernando da Rosa & Cia Ltda ME":
			if c.Resultado != service.CancelamentoTratamento {
				t.Errorf("%s deveria ser em_tratamento: %+v", x.NomeCliente, c)
			}
		}
	}
	m, err := a.MetricasCancelamentos(0)
	if err != nil || m.Total != 4 || m.Tratamento != 2 {
		t.Fatalf("métricas: %+v err=%v", m, err)
	}

	// Ambíguo e sem card voltam como falhas explicativas.
	achouAmbiguo, achouSemCard := false, false
	for _, f := range res.Falhas {
		if strings.Contains(f, "vários cards") {
			achouAmbiguo = true
		}
		if strings.Contains(f, "ZE SEM CARD") {
			achouSemCard = true
		}
	}
	if !achouAmbiguo || !achouSemCard {
		t.Fatalf("falhas: %+v", res.Falhas)
	}

	// Sem escolher coluna de destino, a linha em tratamento fica na lista de
	// falhas com a instrução (o resto da lista é processado do mesmo jeito).
	res2, err := a.ImportarCancelamentos("ROSANE ELIA PAUSE - NÃO SABEMOS O MOTIVO", "")
	if err != nil {
		t.Fatalf("import sem coluna: %v", err)
	}
	if res2.Registrados != 0 || len(res2.Falhas) != 1 || !strings.Contains(res2.Falhas[0], "escolha a coluna") {
		t.Fatalf("import sem coluna: %+v", res2)
	}
}
