package main

import (
	"fmt"
	"strings"
	"testing"

	"rota-smart/internal/rota360"
)

const tecnico = "Luis Henrique"

func ticketDeExemplo() []rota360.MensagemTicket360 {
	ms := []rota360.MensagemTicket360{
		{Tipo: "descricao", Autor: "Cliente Padaria", Corpo: "Câmera wifi não conecta."},
		{Tipo: "sistema", Autor: "Sistema", Corpo: "ticket criado"}, // ignorada
		{Tipo: "publica", Autor: "Outro Técnico", Corpo: "Vamos verificar."},
		{Tipo: "publica", Autor: "Cliente Padaria", Corpo: "Continua fora do ar depois da chuva."},
		{Tipo: "publica", Autor: tecnico, Corpo: "Agendei a visita para amanhã."},
		{Tipo: "interna", Autor: tecnico, Corpo: "nota interna qualquer"},
	}
	return ms
}

func TestResumoMensagensTicketUltimaDeFora(t *testing.T) {
	resumo := resumoMensagensTicket(ticketDeExemplo(), tecnico)

	if !strings.Contains(resumo, "Câmera wifi não conecta") {
		t.Error("descrição (primeira mensagem) sumiu do resumo")
	}
	if !strings.Contains(resumo, "Continua fora do ar depois da chuva") {
		t.Error("última mensagem de fora da sessão deveria estar no resumo")
	}
	if strings.Contains(resumo, "Agendei a visita") || strings.Contains(resumo, "nota interna") {
		t.Error("mensagem do autor da sessão não deveria ser o 'último recado'")
	}
	if strings.Contains(resumo, "Vamos verificar") {
		t.Error("interação intermediária deveria ficar de fora")
	}
	if strings.Contains(resumo, "ticket criado") {
		t.Error("mensagem de sistema deveria ser ignorada")
	}
	if !strings.Contains(resumo, "+3 interação") { // 5 válidas, 2 no resumo
		t.Errorf("nota de omissão ausente ou errada: %q", resumo)
	}
}

func TestResumoMensagensTicketSemAutorSessao(t *testing.T) {
	// Sem autor configurado, a última mensagem válida entra (comportamento antigo).
	resumo := resumoMensagensTicket(ticketDeExemplo(), "")
	if !strings.Contains(resumo, "nota interna qualquer") {
		t.Error("sem autor da sessão, a última interação deveria entrar")
	}
}

func TestResumoMensagensTicketUltimaEADescricao(t *testing.T) {
	// Cliente abriu o ticket e ninguém respondeu: a descrição não se repete.
	ms := []rota360.MensagemTicket360{
		{Tipo: "descricao", Autor: "Cliente Padaria", Corpo: "Sem sinal no DVR."},
	}
	resumo := resumoMensagensTicket(ms, tecnico)
	if strings.Count(resumo, "Sem sinal no DVR") != 1 {
		t.Errorf("descrição deveria aparecer uma única vez: %q", resumo)
	}
	if strings.Contains(resumo, "interação(ões)") {
		t.Errorf("nada foi omitido, nota não deveria existir: %q", resumo)
	}
}

func TestResumoMensagensTicketTetoTotal(t *testing.T) {
	ms := []rota360.MensagemTicket360{
		{Tipo: "descricao", Autor: "Cliente Padaria", Corpo: strings.Repeat("texto longo ", 500)},
		{Tipo: "publica", Autor: "Cliente Padaria", Corpo: fmt.Sprintf("Detalhe — %s", strings.Repeat("mais texto ", 400))},
	}
	resumo := resumoMensagensTicket(ms, tecnico)
	if r := len([]rune(resumo)); r > maxRunasResumoTicket+200 { // +200 = nota/reticências
		t.Errorf("resumo com %d runes (teto %d)", r, maxRunasResumoTicket)
	}
}

func TestResumoMensagensTicketVazio(t *testing.T) {
	if r := resumoMensagensTicket(nil, tecnico); r != "" {
		t.Errorf("ticket sem mensagens deveria dar resumo vazio: %q", r)
	}
	if r := resumoMensagensTicket([]rota360.MensagemTicket360{{Tipo: "sistema", Autor: "S", Corpo: "x"}}, ""); r != "" {
		t.Errorf("só mensagens de sistema deveria dar resumo vazio: %q", r)
	}
}

func TestMensagemTicketDetalheCabecalho(t *testing.T) {
	det := &rota360.TicketDetalhe360{
		Numero: "67220", Titulo: "Câmera offline", Solicitante: "Cliente Padaria",
		CodService: "12345", AssuntoNome: "CFTV", Mensagens: ticketDeExemplo(),
	}
	briefing := mensagemTicketDetalhe(det, tecnico)
	for _, esperado := range []string{"#67220", "Câmera offline", "Cliente Padaria", "12345", "CFTV"} {
		if !strings.Contains(briefing, esperado) {
			t.Errorf("cabeçalho sem %q: %q", esperado, briefing)
		}
	}
	if !strings.Contains(briefing, "SÓ isso") {
		t.Error("instrução de puxar só o necessário ausente")
	}
}
