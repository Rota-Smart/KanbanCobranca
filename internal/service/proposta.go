package service

import (
	"strings"
	"text/template"
	"time"

	"rota-smart/internal/money"
)

// Proposta é o texto entregue ao frontend (Etapa 5.2). Origem é
// "template" ou "ia"; Aviso traz observações para exibir acima do texto.
type Proposta struct {
	Texto  string `json:"texto"`
	Origem string `json:"origem"` // "template" ou "ia"
	Aviso  string `json:"aviso"`
}

// PropostaService gera o texto do acordo. O relógio é injetável para o
// teste não depender da hora; temIA informa se a chave da Groq existe.
type PropostaService struct {
	agora func() time.Time
	temIA func() bool
}

func NewPropostaService(agora func() time.Time, temIA func() bool) *PropostaService {
	return &PropostaService{agora: agora, temIA: temIA}
}

// TemIA diz ao frontend se o botão "Polir com IA" deve aparecer.
func (p *PropostaService) TemIA() bool { return p.temIA() }

var tplProposta = template.Must(template.New("proposta").Funcs(template.FuncMap{
	"brl": money.Format,
}).Parse(`{{.Saudacao}}, {{.PrimeiroNome}}! Tudo bem?

Referente ao total em aberto (R$ {{brl .TotalAberto}}), consigo uma condição de renegociação para você:
{{if .TemEntrada}}• Entrada de R$ {{brl .Entrada}}
{{end}}• Saldo de R$ {{brl .Saldo}} em {{.Vezes}}x de R$ {{brl .Parcelado}}{{if .TemResto}} (a última parcela fica R$ {{brl .Ultima}}){{end}}

Sua parcela normal é R$ {{brl .ParcelaBase}}. Durante o acordo, a mensalidade passa a ser R$ {{brl .Final}}{{if .TemResto}} (R$ {{brl .UltimaFinal}} na última parcela){{end}}.

Posso formalizar para você?
`))

type propostaData struct {
	Saudacao, PrimeiroNome          string
	TotalAberto, Entrada, Saldo     int64
	Parcelado, Ultima               int64
	ParcelaBase, Final, UltimaFinal int64
	TemEntrada, TemResto            bool
	Vezes                           int
}

// Gerar renderiza o template determinístico a partir do resultado
// recalculado no backend (nunca de valores vindos do frontend).
// Sem o cliente Groq (fora do MVP), polirComIA devolve o template com aviso.
func (p *PropostaService) Gerar(nomeCliente string, in ReparcelamentoInput, res ReparcelamentoResult, polirComIA bool) (Proposta, error) {
	var sb strings.Builder
	err := tplProposta.Execute(&sb, propostaData{
		Saudacao:     saudacao(p.agora()),
		PrimeiroNome: primeiroNome(nomeCliente),
		TotalAberto:  in.TotalAbertoCents,
		Entrada:      in.EntradaCents,
		Saldo:        res.SaldoCents,
		Parcelado:    res.ParceladoCents,
		Ultima:       res.UltimaParcelaCents,
		ParcelaBase:  in.ParcelaBaseCents,
		Final:        res.FinalCents,
		UltimaFinal:  res.UltimaFinalCents,
		TemEntrada:   in.EntradaCents > 0,
		TemResto:     res.RestoCents > 0,
		Vezes:        in.NumeroVezes,
	})
	if err != nil {
		return Proposta{}, err
	}

	prop := Proposta{Texto: sb.String(), Origem: "template"}
	if polirComIA && p.temIA() {
		prop.Aviso = "Polimento com IA ainda não está disponível; proposta gerada pelo modelo padrão."
	}
	return prop, nil
}

func saudacao(t time.Time) string {
	switch h := t.Hour(); {
	case h < 12:
		return "Bom dia"
	case h < 18:
		return "Boa tarde"
	default:
		return "Boa noite"
	}
}

// primeiroNome é a primeira palavra do nome, com limite de 40 caracteres.
func primeiroNome(nome string) string {
	nome = strings.TrimSpace(nome)
	if i := strings.IndexAny(nome, " \t"); i > 0 {
		nome = nome[:i]
	}
	if len(nome) > 40 {
		nome = nome[:40]
	}
	return nome
}
