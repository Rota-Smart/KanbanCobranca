// Package money formata valores em centavos (int64) para exibição pt-BR.
// É só para apresentação: decisões de valor nunca passam por string ou float.
package money

import (
	"fmt"
	"strconv"
	"strings"
)

// Format(58400) == "584,00"; Format(123456789) == "1.234.567,89"
func Format(cents int64) string {
	neg := cents < 0
	if neg {
		cents = -cents
	}
	reais := strconv.FormatInt(cents/100, 10)
	var b strings.Builder
	for i, r := range reais {
		if i > 0 && (len(reais)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	out := fmt.Sprintf("%s,%02d", b.String(), cents%100)
	if neg {
		out = "-" + out
	}
	return out
}
