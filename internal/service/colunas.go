package service

import (
	"errors"

	"rota-smart/internal/db"
	"rota-smart/internal/rota360"
)

// ColunasValidas define as colunas do Kanban na ordem de exibição.
// É a única fonte da verdade: o banco não tem CHECK e o frontend
// recebe a lista por GetColunas().
var ColunasValidas = []string{
	"Fazer contato",
	"Contato feito",
	"Negociando",
	"Cobrança",
	"Proposta enviada",
	"Aguardando resposta",
	"Acordo aceito",
	"Pagando",
	"Quitado",
	"Inadimplente",
	"Concluido",
}

// ColunaCobranca dispara o fluxo automático: ao mover o card para cá o app
// puxa o financeiro do 360, avalia o cliente e gera a proposta.
const ColunaCobranca = "Cobrança"

// TiposTicketValidos restringe o campo tipo_ticket na borda (Etapa 5.3).
var TiposTicketValidos = []string{
	"Instalacao",
	"Manutencao",
	"Suporte",
	"Outro",
}

// StatusCobrancaValidos são os únicos status aceitos em SaveCobranca
// e UpdateStatusCobranca.
var StatusCobrancaValidos = []string{"proposta", "aceita", "quebrada", "cancelada"}

// StatusProposta é o status inicial de todo acordo novo.
const StatusProposta = "proposta"

// Limites de tamanho de texto validados em app.go (Etapa 5.3).
const (
	MaxNomeCliente = 120
	MaxEndereco    = 250
	MaxTelefone    = 30
)

// Sentinels de validação de borda; erros de banco nunca entram nesta lista.
var (
	ErrColunaInvalida     = errors.New("coluna inválida")
	ErrTipoTicketInvalido = errors.New("tipo de ticket inválido")
	ErrStatusInvalido     = errors.New("status inválido")
	ErrTextoMuitoLongo    = errors.New("texto maior que o limite permitido")
	ErrDataInvalida       = errors.New("data deve estar no formato yyyy-mm-dd")
	ErrPosicaoInvalida    = errors.New("posição não pode ser negativa")
)

// ErrosDeValidacao reúne todos os sentinelas que podem chegar ao usuário
// como estão; qualquer outro erro vira mensagem genérica em friendly().
var ErrosDeValidacao = []error{
	ErrTotalInvalido,
	ErrEntradaNegativa,
	ErrEntradaMaiorQueTotal,
	ErrParcelaBaseNegativa,
	ErrNumeroVezes,
	ErrValorMuitoAlto,
	ErrParcelaZerada,
	ErrColunaInvalida,
	ErrTipoTicketInvalido,
	ErrStatusInvalido,
	ErrTextoMuitoLongo,
	ErrDataInvalida,
	ErrPosicaoInvalida,
	db.ErrTicketNaoEncontrado,
	db.ErrCobrancaNaoEncontrada,
	rota360.ErrSemSessao,
	rota360.ErrSessaoExpirada,
}

// ColunaValida informa se a coluna existe no Kanban.
func ColunaValida(coluna string) bool {
	for _, c := range ColunasValidas {
		if c == coluna {
			return true
		}
	}
	return false
}

// TipoTicketValido informa se o tipo existe.
func TipoTicketValido(tipo string) bool {
	for _, t := range TiposTicketValidos {
		if t == tipo {
			return true
		}
	}
	return false
}

// StatusCobrancaValido informa se o status existe.
func StatusCobrancaValido(status string) bool {
	for _, s := range StatusCobrancaValidos {
		if s == status {
			return true
		}
	}
	return false
}
