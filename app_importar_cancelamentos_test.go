package main

import (
	"strings"
	"testing"

	"rota-smart/internal/db"
	"rota-smart/internal/service"
)

// Importação de ponta a ponta da lista real com a regra do ticket: card com
// ticket de cancelamento vinculado é mantido; sem vínculo, cria um segundo
// card do 360 (nos testes não há sessão → cai em SemCard com a explicação).
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
	selvino := criar("Selvino Aloisio Clossmann", "Cobrança")
	rosaneB := criar("Rosane Borchardt Vogt", "Cobrança")
	rosaneE := criar("Rosane Elia Pause", "Cobrança")
	luis := criar("Luis Fernando da Rosa & Cia Ltda ME", "Cobrança")
	// Cards SEM cancelamento: as linhas deles viram "criar" (2º card).
	criar("Rosane Borchardt Vogt Filha", "Cobrança")
	// Vínculos de cancelamento: quem tem, é mantido.
	vincular := func(card db.Ticket) {
		t.Helper()
		if err := a.vinculos.Upsert(a.ctx, card.ID, "uuid-"+card.NomeCliente, "123", "Cancelamento", ""); err != nil {
			t.Fatalf("vínculo: %v", err)
		}
	}
	vincular(selvino)
	vincular(rosaneB)
	vincular(rosaneE)
	vincular(luis)

	previa, err := a.PreviaImportarCancelamentos(listaReal)
	if err != nil {
		t.Fatalf("Prévia: %v", err)
	}
	if len(previa.Cancelados) != 1 || len(previa.Revertidos) != 1 || len(previa.Tratamento) != 2 {
		t.Fatalf("prévia errada: %+v", previa)
	}
	if previa.Cancelados[0].Acao != "manter" || previa.Cancelados[0].TicketID != selvino.ID {
		t.Errorf("selvino deveria manter o card: %+v", previa.Cancelados[0])
	}
	// "ROSANE SANTOS" casa por pedaço com os dois Rosane, nenhum com vínculo
	// de cancelamento → cria (e nos testes, sem 360, vai pra SemCard).
	if len(previa.SemCard) != 2 {
		t.Fatalf("sem card (precisa do 360): %+v", previa.SemCard)
	}
	for _, x := range previa.SemCard {
		if x.Acao != "criar" {
			t.Errorf("esperava acao criar: %+v", x)
		}
	}

	res, err := a.ImportarCancelamentos(listaReal, "Cobrança")
	if err != nil {
		t.Fatalf("Importar: %v", err)
	}
	// Registrados = os 4 com vínculo de cancelamento; movidos = os 2 fechados
	// (Selvino e Rosane B); os 2 em tratamento já estavam na coluna escolhida.
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
	if colunaDe("Selvino Aloisio Clossmann") != ColunaTicketFechado {
		t.Errorf("cancelado deveria estar em Ticket Fechado")
	}
	if colunaDe("Rosane Elia Pause") != "Cobrança" {
		t.Errorf("em tratamento deveria ir para a coluna escolhida")
	}
	// Motivos gravados com o resultado certo.
	ts, _ := a.GetTickets()
	for _, x := range ts {
		switch x.NomeCliente {
		case "Selvino Aloisio Clossmann", "Rosane Borchardt Vogt", "Rosane Elia Pause", "Luis Fernando da Rosa & Cia Ltda ME":
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
			default:
				if c.Resultado != service.CancelamentoTratamento {
					t.Errorf("%s deveria ser em_tratamento: %+v", x.NomeCliente, c)
				}
			}
		}
	}
	m, err := a.MetricasCancelamentos(0, true)
	if err != nil || m.Total != 4 || m.Tratamento != 2 {
		t.Fatalf("métricas: %+v err=%v", m, err)
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

// Motivo com plural/caixa diferente é a MESMA tag ("Problemas Financeiros" =
// "problema financeiro") — a chave canônica (singular + sem acento + caixa).
func TestMotivoPluralECaixaMesmaTag(t *testing.T) {
	a := newTestApp(t)
	c1, _ := a.SaveTicket(exemploTicket())
	c2, _ := a.SaveTicket(exemploTicket())
	if err := a.RegistrarCancelamento(c1.ID, "Problemas Financeiros", service.CancelamentoPerda); err != nil {
		t.Fatalf("registrar c1: %v", err)
	}
	if err := a.RegistrarCancelamento(c2.ID, "problema financeiro", service.CancelamentoReversao); err != nil {
		t.Fatalf("registrar c2: %v", err)
	}
	ms, err := a.ListarMotivosCancelamento()
	if err != nil || len(ms) != 1 {
		t.Fatalf("deveria ser UMA tag: %+v err=%v", ms, err)
	}
}
