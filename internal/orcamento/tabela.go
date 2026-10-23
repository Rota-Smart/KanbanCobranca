// Package orcamento lê a planilha-calculadora comercial (xlsx) e monta
// orçamentos a partir dela. O layout esperado é o da tabela em uso:
// uma aba por tipo de sistema (ALARME, CAMERAS WIFI, CAMERAS DVR,
// RASTREAMENTO), com seções de itens (preço mensal ou de instalação),
// taxa de instalação e faixas de mensalidade (tabela, margem mínima).
package orcamento

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// ItemOrcamento é uma linha de preço da planilha.
type ItemOrcamento struct {
	Nome   string   `json:"nome"`
	Taxa   *float64 `json:"taxa,omitempty"`   // valor de instalação (nil = isento/—)
	Mensal *float64 `json:"mensal,omitempty"` // valor mensal (nil = isento/—)
	Seccao string   `json:"secao"`
}

// SecaoOrcamento agrupa itens da mesma categoria numa aba.
type SecaoOrcamento struct {
	Titulo string          `json:"titulo"`
	Itens  []ItemOrcamento `json:"itens"`
}

// FaixaMensalidade é uma linha do bloco MENSALIDADE da planilha
// (ex.: "PREÇO DE TABELA" → 297, "MARGEM MÍNIMA (36 MESES)" → 287).
type FaixaMensalidade struct {
	Rotulo string  `json:"rotulo"`
	Valor  float64 `json:"valor"`
}

// TipoSistema é uma aba da planilha (um sistema orçável).
type TipoSistema struct {
	Nome           string             `json:"nome"`
	Secoes         []SecaoOrcamento   `json:"secoes"`
	TaxaInstalacao float64            `json:"taxaInstalacao"`
	FaixasMensal   []FaixaMensalidade `json:"faixasMensal"`
	Orientacoes    string             `json:"orientacoes,omitempty"` // regra de arredondamento etc.
}

// Tabela é a planilha inteira carregada.
type Tabela struct {
	Arquivo string        `json:"arquivo"`
	Tipos   []TipoSistema `json:"tipos"`
}

// Tipo devolve o tipo de sistema pelo nome (sem diferenciar maiúsculas).
func (t *Tabela) Tipo(nome string) *TipoSistema {
	for i := range t.Tipos {
		if strings.EqualFold(t.Tipos[i].Nome, nome) {
			return &t.Tipos[i]
		}
	}
	return nil
}

// limpaTexto corta espaços e quebras duplicadas dos rótulos da planilha.
func limpaTexto(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// numOuNil converte a célula de preço: número → valor; "Isento"/vazio → nil.
func numOuNil(v any) *float64 {
	switch x := v.(type) {
	case float64:
		return &x
	case int:
		f := float64(x)
		return &f
	case string:
		s := strings.TrimSpace(x)
		if s == "" || strings.EqualFold(s, "Isento") {
			return nil
		}
		s = strings.ReplaceAll(strings.TrimSpace(s), "R$", "")
		s = strings.TrimSpace(s)
		// o separador que aparece por último é o decimal; o outro é milhar
		ultV, ultP := strings.LastIndex(s, ","), strings.LastIndex(s, ".")
		if ultV >= 0 && ultP >= 0 {
			if ultV > ultP {
				s = strings.ReplaceAll(s, ".", "")
			} else {
				s = strings.ReplaceAll(s, ",", "")
			}
		}
		s = strings.ReplaceAll(s, ",", ".")
		var f float64
		if _, err := fmt.Sscanf(s, "%f", &f); err == nil {
			return &f
		}
	}
	return nil
}

// CabecalhosDeSecao marcam o início de um bloco de itens na planilha.
func ehCabecalhoSecao(v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	s = strings.TrimSpace(s)
	return s == "UNITÁRIO" || s == "TAXA" || s == "QTD" || s == "MENSAL" || s == "TOTAL"
}

// ehNota devolve true para as linhas de observação (regra de arredondamento),
// que devem ser guardadas como orientação e não como item.
func ehNota(s string) bool {
	return strings.HasPrefix(s, "Arredondamento")
}

// LerTabela carrega a planilha-calculadora do caminho indicado.
func LerTabela(caminho string) (*Tabela, error) {
	f, err := excelize.OpenFile(caminho)
	if err != nil {
		return nil, fmt.Errorf("não consegui abrir a planilha: %w", err)
	}
	defer f.Close()

	t := &Tabela{Arquivo: caminho}
	for _, aba := range f.GetSheetList() {
		linhas, err := f.GetRows(aba)
		if err != nil || len(linhas) == 0 {
			continue
		}
		tipo := parseAba(aba, linhas)
		if len(tipo.Secoes) > 0 {
			t.Tipos = append(t.Tipos, tipo)
		}
	}
	if len(t.Tipos) == 0 {
		return nil, fmt.Errorf("a planilha não tem abas de itens reconhecíveis (procurei seções com UNITÁRIO/TAXA/MENSAL)")
	}
	return t, nil
}

// parseAba interpreta uma aba seguindo o layout da calculadora:
// linhas de seção (B=titulo, C=cabeçalho), itens (B=nome, preços em C..),
// "TAXA DE INSTALAÇÃO" (valor na linha seguinte) e o bloco MENSALIDADE.
func parseAba(aba string, linhas [][]string) TipoSistema {
	tipo := TipoSistema{Nome: strings.ToUpper(strings.TrimSpace(aba))}
	secIdx := -1
	porTaxa := false // false: unitário,qtd,total | true: taxa,mensal,qtd,total
	headerMensal := ""

	for i := 0; i < len(linhas); i++ {
		cels := linhas[i]
		pick := func(col int) string {
			if col < len(cels) {
				return strings.TrimSpace(cels[col])
			}
			return ""
		}
		b := pick(1) // coluna B
		c := pick(2) // coluna C

		switch {
		case ehCabecalhoSecao(any(c)):
			titulo := limpaTexto(b)
			if titulo == "" {
				continue
			}
			porTaxa = strings.EqualFold(c, "TAXA")
			tipo.Secoes = append(tipo.Secoes, SecaoOrcamento{Titulo: titulo})
			secIdx = len(tipo.Secoes) - 1

		case strings.EqualFold(b, "TAXA DE INSTALAÇÃO"):
			// o valor vem na linha seguinte (coluna B ou C)
			if i+1 < len(linhas) {
				for _, cel := range linhas[i+1] {
					if v := numOuNil(any(strings.TrimSpace(cel))); v != nil {
						tipo.TaxaInstalacao = *v
						break
					}
				}
			}
			secIdx = -1

		case strings.Contains(strings.ToUpper(b), "MENSALIDADE"):
			secIdx = -2 // coletando faixas de mensalidade
			headerMensal = b

		case secIdx == -2:
			if v := numOuNil(any(c)); v != nil && b != "" && !ehNota(b) {
				tipo.FaixasMensal = append(tipo.FaixasMensal, FaixaMensalidade{Rotulo: limpaTexto(b), Valor: *v})
			} else if v := numOuNil(any(b)); v != nil {
				// valor solto na coluna B (ex.: RASTREAMENTO, mensalidade única)
				rotulo := strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(headerMensal), "MENSALIDADE"))
				rotulo = strings.Trim(rotulo, " ()-")
				if rotulo == "" {
					rotulo = "TABELA"
				}
				tipo.FaixasMensal = append(tipo.FaixasMensal, FaixaMensalidade{Rotulo: rotulo, Valor: *v})
			} else if b != "" && ehNota(b) {
				tipo.Orientacoes = limpaTexto(b)
				secIdx = -1
			}

		case secIdx >= 0 && b != "" && !ehCabecalhoSecao(any(c)):
			val := func(col int) *float64 {
				if col < len(cels) {
					return numOuNil(any(strings.TrimSpace(cels[col])))
				}
				return nil
			}
			it := ItemOrcamento{Nome: limpaTexto(b), Seccao: tipo.Secoes[secIdx].Titulo}
			if porTaxa {
				// layout: C=taxa de instalação (pode ser "Isento"), D=mensal
				it.Taxa = val(2)
				it.Mensal = val(3)
			} else {
				// layout: C=unitário (adicional mensal)
				it.Mensal = val(2)
			}
			if it.Taxa == nil && it.Mensal == nil {
				continue
			}
			tipo.Secoes[secIdx].Itens = append(tipo.Secoes[secIdx].Itens, it)
		}
	}
	return tipo
}
