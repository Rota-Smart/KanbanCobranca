package service

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Reconhecimento local de intenção: comandos rígidos de card ("apague o card
// do João", "mova o card #123 para Cobrança") são detectados por regex e
// executados direto — sem chamada de API, sem chave, resposta instantânea.
// Frases fora do molde caem no fluxo normal do agente.

// IntencaoLocal é um comando de card reconhecido no texto puro.
type IntencaoLocal struct {
	Apagar bool   // true = apagar card; false = mover
	Alvo   string // nome, código service ou id do card
	Coluna string // destino (só no mover)
}

var (
	reApagarCard = regexp.MustCompile(
		`(?i)^\s*(?:apag\w+|exclu\w+|remov\w+|delet\w+)\s+(?:o\s+|a\s+|esse\s+|esse\s+aqui\s+o\s+)?card\b(?:\s+d[oa]s?\s+cliente)?\s+(.+?)\s*[.!?]*$`)
	reMoverCard = regexp.MustCompile(
		`(?i)^\s*(?:mov\w+|arrast\w+|passe|passar|lev\w+|mand\w+|mud\w+|jog\w+|pux\w+)\s+(?:o\s+|a\s+)?card\b(?:\s+d[oa]s?\s+cliente)?\s+(.+?)\s+(?:para|pro|pra|p\/|to)\s+(?:a\s+coluna\s+|coluna\s+|o\s+)?(.+?)\s*[.!?]*$`)
)

// ParseIntencaoCard reconhece "apagar card X" e "mover card X para Y" no
// texto; ok=false quando não é um desses comandos.
func ParseIntencaoCard(msg string) (IntencaoLocal, bool) {
	if m := reApagarCard.FindStringSubmatch(msg); m != nil {
		return IntencaoLocal{Apagar: true, Alvo: strings.TrimSpace(m[1])}, true
	}
	if m := reMoverCard.FindStringSubmatch(msg); m != nil {
		return IntencaoLocal{Apagar: false, Alvo: strings.TrimSpace(m[1]), Coluna: strings.TrimSpace(m[2])}, true
	}
	return IntencaoLocal{}, false
}

// CardResumo é o mínimo que o reconhecimento precisa saber dos cards.
type CardResumo struct {
	ID          int64
	NomeCliente string
	CodService  string
}

// ResolverCard casa o alvo com UM card: número puro ou "#n" = código service
// (ou id), texto = nome (substring, sem caixa). Zero ou vários matches são
// erro — apagar/mover o card errado é pior que pedir de novo.
func ResolverCard(alvo string, cards []CardResumo) (CardResumo, error) {
	alvo = strings.TrimSpace(alvo)
	alvo = strings.TrimPrefix(alvo, "#")
	if alvo == "" {
		return CardResumo{}, fmt.Errorf("diga o nome do cliente (ou o código) do card")
	}
	if n, err := strconv.ParseInt(alvo, 10, 64); err == nil {
		for _, c := range cards {
			if c.ID == n || c.CodService == alvo {
				return c, nil
			}
		}
		return CardResumo{}, fmt.Errorf("nenhum card com código/id %s", alvo)
	}
	low := strings.ToLower(alvo)
	var achados []CardResumo
	for _, c := range cards {
		if strings.Contains(strings.ToLower(c.NomeCliente), low) {
			achados = append(achados, c)
		}
	}
	switch len(achados) {
	case 1:
		return achados[0], nil
	case 0:
		return CardResumo{}, fmt.Errorf("nenhum card com %q no nome", alvo)
	default:
		nomes := make([]string, 0, len(achados))
		for _, c := range achados {
			nomes = append(nomes, fmt.Sprintf("%s (código %s)", c.NomeCliente, mapaTexto(c.CodService)))
		}
		return CardResumo{}, fmt.Errorf("%d cards casam com %q: %s — seja mais específico",
			len(achados), alvo, strings.Join(nomes, "; "))
	}
}

func mapaTexto(s string) string {
	if s == "" {
		return "?"
	}
	return s
}
