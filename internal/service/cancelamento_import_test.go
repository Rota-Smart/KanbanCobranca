package service

import (
	"strings"
	"testing"
)

// A lista real que motivou o recurso — cobre resultado no fim, motivo com
// traço no meio, sem resultado, nome com "& CIA LTDA ME" e espaços duplos.
const listaReal = `SELVINO ALOISIO CLOSSMANN - TROCA DE ENDEREÇO - CANCELADO
GABRIELLA DA ROSA LIZOT - ENCERRAMENTO DAS ATIVIDADES DA EMPRESA - CANCELADO
EDUANA REGINA SAMERSLA - PROBLEMAS FINANCEIROS DA EMPRESA - CANCELADO
JULIANO THIER BECKE - PROBLEMAS FINANCEIROS DA EMPRESA - CANCELADO
RESOLUTE AUTOMOTIVE LTDA - ENCERRAMENTO DAS ATIVIDADES DA EMPRESA - CANCELADO
CAROLINA DOS SANTOS CARGNELUTTI - ENCERRAMENTO DAS ATIVIDADES DA EMPRESA - CANCELADO
JEAN CARLO KETZER HECK  - ENCERRAMENTO DAS ATIVIDADES DA EMPRESA - CANCELADO
AMAURI KRAWECHUKA - PROBLEMAS FINANCEIROS - CANCELADO
ROSANE BORCHARDT VOGT - INSATISFAÇÃO COM ATENDIMENTO (JÁ RESOLVIDO) - REVERTIDO
TEREZINHA DOS SANTOS DE SA - PROBLEMAS FINANCEIROS - REVERTIDO
UILIAM FIUZZA - SEPARAÇÃO - REVERTIDO
ROSANE ELIA PAUSE - NÃO SABEMOS O MOTIVO
JOLAIR JOSE GALVAO CORREA - NÃO SABEMOS O MOTIVO
JOAO CARLOS RODRIGUES FERREIRA - MUDANÇA DE ENDEREÇO
LUIS FERNANDO DA ROSA & CIA LTDA ME - INSATIFAÇÃO COM SERVIÇOS
LUIS FERNANDO DA ROSA & CIA LTDA - INSATIFAÇÃO COM SERVIÇOS
JULIANO DA SILVA - MUDANÇA DE ENDEREÇO - NÃO USA MAIS O SERVIÇO
ANDERSON LEAL GOI - PROBLEMAS FINANCEIRO DA EMPRESA
ANA LUIZA RIBEIRO BORGEDUFF - PROBLEMAS FINANCEIRO
HELENITA SCHULZ FEISTEL - PROBLEMAS FINANCEIROS
MARCIA REGINA RIETHMULLER - PROBLEMA FINANCEIRO
DENISE DE SOUZA PINHEIRO - ENCERRAMENTO DAS ATIVIDADES DA EMPRESA
GENTIL CORREA BATU - NÃO SABEMOS O MOTIVO
TECNO MOTOS - COMERCIO DE PECAS E ACESSORIOS PARA MOTOCICLETAS LTDA - INSATISFAÇÃO COM SERVIÇOS
JACSON RAFAEL LIMA CANABARRO - SEPARAÇÃO
KEYLA KURRLE KEGLER - NÃO UTITLIZA MAIS O SISTEMA
CELSO EIDT - MUDANÇA DE ENDEREÇO
ANDREIA TAIS FONSECA - PROBLEMAS FINANCEIROS`

func TestParseListaReal(t *testing.T) {
	linhas, erros := ParseListaCancelamentos(listaReal)
	if len(erros) != 0 {
		t.Fatalf("erros inesperados: %v", erros)
	}
	if len(linhas) != 28 {
		t.Fatalf("esperava 28 linhas, veio %d", len(linhas))
	}
	var perdas, reversoes, tratamento int
	for _, l := range linhas {
		switch l.Resultado {
		case CancelamentoPerda:
			perdas++
		case CancelamentoReversao:
			reversoes++
		case CancelamentoTratamento:
			tratamento++
		default:
			t.Fatalf("resultado inválido na linha %d: %q", l.Linha, l.Resultado)
		}
	}
	if perdas != 8 {
		t.Errorf("esperava 8 CANCELADO, veio %d", perdas)
	}
	if reversoes != 3 {
		t.Errorf("esperava 3 REVERTIDO, veio %d", reversoes)
	}
	if tratamento != 17 {
		t.Errorf("esperava 17 em tratamento, veio %d", tratamento)
	}
}

func TestParseCasosEspeciais(t *testing.T) {
	linhas, erros := ParseListaCancelamentos(listaReal)
	if len(erros) != 0 {
		t.Fatalf("erros: %v", erros)
	}
	porNome := map[string]LinhaCancelamento{}
	for _, l := range linhas {
		porNome[l.Nome] = l
	}

	// Motivo com traço dentro, sem resultado no fim.
	l := porNome["JULIANO DA SILVA"]
	if l.Resultado != CancelamentoTratamento || l.Motivo != "MUDANÇA DE ENDEREÇO - NÃO USA MAIS O SERVIÇO" {
		t.Errorf("duplo traço: %+v", l)
	}
	// Espaço duplo antes do traço não suja o nome.
	if l := porNome["JEAN CARLO KETZER HECK"]; l.Resultado != CancelamentoPerda {
		t.Errorf("espaço duplo: %+v", l)
	}
	// Acento e grafia do usuário vêm como estão (motivo é texto livre).
	if l := porNome["ROSANE BORCHARDT VOGT"]; l.Motivo != "INSATISFAÇÃO COM ATENDIMENTO (JÁ RESOLVIDO)" || l.Resultado != CancelamentoReversao {
		t.Errorf("revertido: %+v", l)
	}
	// Linha vazia e lixo não derrubam o lote.
	_, erros = ParseListaCancelamentos("\n\nSEM TRACO AQUI\nFOO - BAR\n")
	if len(erros) != 1 || !strings.Contains(erros[0], "sem o formato") {
		t.Errorf("erros de lixo: %v", erros)
	}
}

func TestCasarCardPorNome(t *testing.T) {
	cards := []CardResumo{
		{ID: 1, NomeCliente: "Luis Fernando da Rosa & Cia Ltda ME", CodService: "111"},
		{ID: 2, NomeCliente: "Rosane Elia Pause", CodService: "222"},
		{ID: 3, NomeCliente: "Rosane Borchardt Vogt", CodService: "333"},
		{ID: 4, NomeCliente: "Tecno Motos - Comercio de Pecas", CodService: "444"},
	}

	// Contém nas duas direções: variante com ME a mais casa o card sem ME.
	m, ok := CasarCardPorNome("LUIS FERNANDO DA ROSA & CIA LTDA ME", cards)
	if !ok || m.Ambiguo || m.TicketID != 1 {
		t.Errorf("com ME: ok=%v %+v", ok, m)
	}
	m, ok = CasarCardPorNome("LUIS FERNANDO DA ROSA & CIA LTDA", cards)
	if !ok || m.Ambiguo || m.TicketID != 1 {
		t.Errorf("sem ME: ok=%v %+v", ok, m)
	}
	// Ambíguo: dois Rosane.
	m, ok = CasarCardPorNome("ROSANE SILVA", cards)
	if !ok || !m.Ambiguo || len(m.Candidatos) != 2 {
		t.Errorf("ambíguo: ok=%v %+v", ok, m)
	}
	// Nome não existe.
	if _, ok := CasarCardPorNome("ZE NINGUEM DA SILVA", cards); ok {
		t.Errorf("não deveria casar")
	}
}
