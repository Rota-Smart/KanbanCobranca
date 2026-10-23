package orcamento

import (
	"fmt"
	"sort"
	"strings"
)

// Selecao descreve o que o usuário (ou o agente) escolheu para o orçamento.
type Selecao struct {
	TipoSistema    string         `json:"tipoSistema"`              // aba da planilha (ex.: ALARME)
	Itens          map[string]int `json:"itens"`                    // nome do item → quantidade
	TaxaInstalacao *float64       `json:"taxaInstalacao,omitempty"` // override da taxa da aba
}

// Resultado é o orçamento calculado.
type Resultado struct {
	TipoSistema    string             `json:"tipoSistema"`
	Linhas         []string           `json:"linhas"` // "2x Sensor de Presença — R$ 20,00/mês"
	TaxaInstalacao float64            `json:"taxaInstalacao"`
	Mensal         float64            `json:"mensal"`
	FaixasMensal   []FaixaMensalidade `json:"faixasMensal"`
	Orientacoes    string             `json:"orientacoes,omitempty"`
}

// Calcular soma os itens escolhidos sobre a aba do tipo de sistema.
// Itens com nome desconhecido são reportados em erro.
func Calcular(t *Tabela, s Selecao) (*Resultado, error) {
	tipo := t.Tipo(s.TipoSistema)
	if tipo == nil {
		nomes := make([]string, 0, len(t.Tipos))
		for _, tt := range t.Tipos {
			nomes = append(nomes, tt.Nome)
		}
		return nil, fmt.Errorf("tipo de sistema %q não está na planilha (tipos: %s)", s.TipoSistema, strings.Join(nomes, ", "))
	}

	res := &Resultado{TipoSistema: tipo.Nome, TaxaInstalacao: tipo.TaxaInstalacao, FaixasMensal: tipo.FaixasMensal, Orientacoes: tipo.Orientacoes}
	if s.TaxaInstalacao != nil {
		res.TaxaInstalacao = *s.TaxaInstalacao
	}

	nomes := make([]string, 0, len(s.Itens))
	for nome := range s.Itens {
		nomes = append(nomes, nome)
	}
	sort.Strings(nomes)

	for _, nome := range nomes {
		qtd := s.Itens[nome]
		if qtd <= 0 {
			continue
		}
		var achou *ItemOrcamento
		for si := range tipo.Secoes {
			for ii := range tipo.Secoes[si].Itens {
				if strings.EqualFold(tipo.Secoes[si].Itens[ii].Nome, nome) {
					achou = &tipo.Secoes[si].Itens[ii]
					break
				}
			}
		}
		if achou == nil {
			return nil, fmt.Errorf("item %q não existe na aba %s da planilha", nome, tipo.Nome)
		}
		linha := fmt.Sprintf("%dx %s", qtd, achou.Nome)
		if achou.Taxa != nil {
			res.TaxaInstalacao += *achou.Taxa * float64(qtd)
			linha += fmt.Sprintf(" — instalação R$ %.2f", *achou.Taxa*float64(qtd))
		}
		if achou.Mensal != nil {
			res.Mensal += *achou.Mensal * float64(qtd)
			linha += fmt.Sprintf(" — R$ %.2f/mês", *achou.Mensal*float64(qtd))
		}
		res.Linhas = append(res.Linhas, linha)
	}
	return res, nil
}

// PropostaTexto monta o texto do orçamento para o cliente.
func (r *Resultado) PropostaTexto(cliente string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "ORÇAMENTO — %s\n", r.TipoSistema)
	if cliente != "" {
		fmt.Fprintf(&sb, "Cliente: %s\n", cliente)
	}
	sb.WriteString("\nItens:\n")
	for _, l := range r.Linhas {
		sb.WriteString("- " + l + "\n")
	}
	fmt.Fprintf(&sb, "\nTaxa de instalação: R$ %.2f\n", r.TaxaInstalacao)
	fmt.Fprintf(&sb, "Mensalidade: R$ %.2f/mês\n", r.Mensal)
	if len(r.FaixasMensal) > 0 {
		sb.WriteString("\nReferências da tabela:\n")
		for _, f := range r.FaixasMensal {
			fmt.Fprintf(&sb, "- %s: R$ %.2f/mês\n", f.Rotulo, f.Valor)
		}
	}
	if r.Orientacoes != "" {
		fmt.Fprintf(&sb, "\nObs.: %s\n", r.Orientacoes)
	}
	return sb.String()
}
