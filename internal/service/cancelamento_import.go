package service

import (
	"fmt"
	"strings"
)

// Importação de lista colada de cancelamentos ("NOME - MOTIVO - CANCELADO").
// Cada linha vira um registro de cancelamento no dashboard: linha terminando
// em CANCELADO = perda, REVERTIDO = reversão, sem resultado no fim =
// em_tratamento (ticket ainda em aberto). O nome da frente é casado com os
// cards do quadro — sem card, o importador procura no 360 pelo mesmo nome.

// LinhaCancelamento é uma linha da lista já interpretada (sem olhar o quadro).
type LinhaCancelamento struct {
	Linha     int    `json:"linha"`     // nº da linha no texto colado (1-based)
	Nome      string `json:"nome"`      // cliente do começo da linha
	Motivo    string `json:"motivo"`    // texto do meio (pode ter traço dentro)
	Resultado string `json:"resultado"` // perda | reversao | em_tratamento
}

// CardMatch é o resultado do casamento nome↔card: o card casado (se único) e
// os nomes dos candidatos quando casou com mais de um (ambíguo = não mexe).
type CardMatch struct {
	TicketID   int64
	CodService string
	Coluna     string
	Candidatos []string // preenchido só quando é ambíguo
	Ambiguo    bool
}

// resultadoDeFim interpreta o último trecho da linha: CANCELADO = perda,
// REVERTIDO = reversão, qualquer outra coisa NÃO é resultado (vira motivo —
// ex.: "MUDANÇA DE ENDEREÇO - NÃO USA MAIS O SERVIÇO").
func resultadoDeFim(trecho string) string {
	n := normalizar(trecho)
	if strings.Contains(n, "cancelad") {
		return CancelamentoPerda
	}
	if strings.Contains(n, "revertid") {
		return CancelamentoReversao
	}
	return ""
}

// ParseListaCancelamentos quebra o texto colado em linhas interpretadas.
// Linhas vazias são ignoradas; linha sem nome nem motivo vira erro na lista
// (não para o lote inteiro).
func ParseListaCancelamentos(texto string) ([]LinhaCancelamento, []string) {
	var linhas []LinhaCancelamento
	var erros []string
	for i, bruta := range strings.Split(texto, "\n") {
		bruta = strings.TrimSpace(bruta)
		if bruta == "" {
			continue
		}
		partes := strings.Split(bruta, "-")
		if len(partes) < 2 || strings.TrimSpace(partes[0]) == "" {
			erros = append(erros, fmt.Sprintf("linha %d: sem o formato NOME - MOTIVO", i+1))
			continue
		}
		l := LinhaCancelamento{Linha: i + 1, Nome: strings.TrimSpace(partes[0])}
		resto := partes[1:]
		if r := resultadoDeFim(resto[len(resto)-1]); r != "" {
			l.Resultado = r
			resto = resto[:len(resto)-1]
		} else {
			l.Resultado = CancelamentoTratamento
		}
		for j := range resto {
			resto[j] = strings.TrimSpace(resto[j])
		}
		l.Motivo = strings.TrimSpace(strings.Join(resto, " - "))
		if l.Motivo == "" {
			erros = append(erros, fmt.Sprintf("linha %d: sem motivo", i+1))
			continue
		}
		linhas = append(linhas, l)
	}
	return linhas, erros
}

// chaveNome normaliza e colapsa espaços: "JEAN CARLO  KETZER" e
// "jean carlo ketzer" viram a mesma chave de comparação.
func chaveNome(s string) string {
	return strings.Join(strings.Fields(normalizar(s)), " ")
}

// CasarCardPorNome acha o card do quadro para o nome da linha. Igual ao
// ResolverCardNoTexto: casar com o card ERRADO é pior que não casar — nome
// inteiro (contém, nas duas direções, para tolerar "& CIA LTDA ME" sobrando)
// manda; só aceita pedaço (palavra ≥4 letras) se apontar para um único card.
func CasarCardPorNome(nome string, cards []CardResumo) (CardMatch, bool) {
	alvo := chaveNome(nome)
	if alvo == "" {
		return CardMatch{}, false
	}
	var exatos, contem []CardResumo
	for _, c := range cards {
		n := chaveNome(c.NomeCliente)
		if n == "" {
			continue
		}
		if n == alvo {
			exatos = append(exatos, c)
			continue
		}
		if strings.Contains(n, alvo) || strings.Contains(alvo, n) {
			contem = append(contem, c)
		}
	}
	if len(exatos) == 1 && len(contem) == 0 {
		return cardMatch(exatos[0]), true
	}
	if len(exatos) > 1 || (len(exatos) == 1 && len(contem) > 0) {
		return ambiguo(append(contem, exatos...)), true
	}
	if len(contem) == 1 {
		return cardMatch(contem[0]), true
	}
	if len(contem) > 1 {
		return ambiguo(contem), true
	}
	// Último recurso: todas as palavras grandes do nome citado dentro do nome
	// do card (ex.: sobrenome diferente entre 360 e quadro).
	var porPeca []CardResumo
	for _, c := range cards {
		n := chaveNome(c.NomeCliente)
		if n == "" {
			continue
		}
		casa := true
		for _, tk := range strings.Fields(alvo) {
			if len(tk) < 4 || pedaçoComum(tk) {
				continue
			}
			if !strings.Contains(n, tk) {
				casa = false
				break
			}
		}
		if casa {
			porPeca = append(porPeca, c)
		}
	}
	if len(porPeca) == 1 {
		return cardMatch(porPeca[0]), true
	}
	if len(porPeca) > 1 {
		return ambiguo(porPeca), true
	}
	return CardMatch{}, false
}

func cardMatch(c CardResumo) CardMatch {
	return CardMatch{TicketID: c.ID, CodService: c.CodService, Coluna: c.ColunaAtual}
}

func ambiguo(cs []CardResumo) CardMatch {
	m := CardMatch{Ambiguo: true}
	vistos := map[string]bool{}
	for _, c := range cs {
		if !vistos[c.NomeCliente] {
			vistos[c.NomeCliente] = true
			m.Candidatos = append(m.Candidatos, c.NomeCliente)
		}
	}
	return m
}

// CasarCardsPorNomeLista devolve TODOS os cards cujo nome casa com o citado
// (contém nas duas direções, cobrindo "& CIA LTDA ME"; se nada casar, tenta
// pedaço). Diferente do CasarCardPorNome (que exige match único), aqui quem
// decide entre os candidatos é a regra do ticket de cancelamento.
func CasarCardsPorNomeLista(nome string, cards []CardResumo) []CardResumo {
	alvo := chaveNome(nome)
	if alvo == "" {
		return nil
	}
	var out []CardResumo
	vistos := map[int64]bool{}
	for _, c := range cards {
		if vistos[c.ID] {
			continue
		}
		n := chaveNome(c.NomeCliente)
		if n == "" {
			continue
		}
		if n == alvo || strings.Contains(n, alvo) || strings.Contains(alvo, n) {
			vistos[c.ID] = true
			out = append(out, c)
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, c := range cards {
		if vistos[c.ID] {
			continue
		}
		n := chaveNome(c.NomeCliente)
		if n == "" {
			continue
		}
		casa := true
		for _, tk := range strings.Fields(alvo) {
			if len(tk) < 4 || pedaçoComum(tk) {
				continue
			}
			if !strings.Contains(n, tk) {
				casa = false
				break
			}
		}
		if casa {
			vistos[c.ID] = true
			out = append(out, c)
		}
	}
	return out
}
