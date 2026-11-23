package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"rota-smart/internal/db"
	"rota-smart/internal/rota360"
	"rota-smart/internal/service"
)

// Cancelamento obrigatório: bindings para o diálogo de motivo e o dashboard.
// A trava em si vive no MoveTicket (assim botão de ações, arrastar, chat, IA
// e verificação automática passam todos pelo mesmo lugar).

// cancelamentoPendente diz se o card tem ticket de assunto "Cancelamento"
// sem motivo registrado — só ele bloqueia o fechamento.
func (a *App) cancelamentoPendente(ticketID int64) (bool, error) {
	if a.vinculos == nil || a.cancelamentos == nil {
		return false, nil // wiring ausente (testes antigos): não trava
	}
	vs, err := a.vinculos.List(a.ctx, ticketID)
	if err != nil {
		return false, err
	}
	temCancelamento := false
	for _, v := range vs {
		if service.AssuntoCancelamento(v.Assunto) {
			temCancelamento = true
			break
		}
	}
	if !temCancelamento {
		return false, nil
	}
	c, err := a.cancelamentos.CancelarDoTicket(a.ctx, ticketID)
	if err != nil {
		return false, err
	}
	return c == nil, nil
}

// ListarMotivosCancelamento alimenta o datalist do diálogo de motivo.
func (a *App) ListarMotivosCancelamento() ([]db.MotivoCancelamento, error) {
	if a.cancelamentos == nil {
		return nil, fmt.Errorf("banco indisponível nesta instalação")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ms, err := a.cancelamentos.MotivosListar(ctx)
	if err != nil {
		return nil, friendly("ListarMotivosCancelamento", err)
	}
	return ms, nil
}

// RegistrarCancelamento grava motivo (cria a tag se for nova) + resultado do
// cancelamento de um card. O movimento em si fica por conta do chamador.
func (a *App) RegistrarCancelamento(ticketID int64, motivo, resultado string) error {
	if a.cancelamentos == nil {
		return fmt.Errorf("banco indisponível nesta instalação")
	}
	if _, err := a.tickets.Get(a.ctx, ticketID); err != nil {
		return friendly("RegistrarCancelamento", err)
	}
	motivo = strings.TrimSpace(motivo)
	if motivo == "" {
		return fmt.Errorf("diga o motivo do cancelamento")
	}
	if !service.CancelamentoResultadoValido(resultado) {
		return fmt.Errorf("resultado deve ser reversão ou perda")
	}
	m, err := a.cancelamentos.MotivoCriarSeNaoExiste(a.ctx, motivo)
	if err != nil {
		return friendly("RegistrarCancelamento", err)
	}
	if err := a.cancelamentos.CancelarMarcar(a.ctx, ticketID, m.ID, resultado); err != nil {
		return friendly("RegistrarCancelamento", err)
	}
	return nil
}

// MetricasCancelamentos alimenta o dashboard de cancelamentos (últimos dias;
// 0 = todos).
func (a *App) MetricasCancelamentos(dias int) (*db.MetricasCancelamento, error) {
	if a.cancelamentos == nil {
		return nil, fmt.Errorf("banco indisponível nesta instalação")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, err := a.cancelamentos.Metricas(ctx, dias)
	if err != nil {
		return nil, friendly("MetricasCancelamentos", err)
	}
	return m, nil
}

// CancelamentosDoMotivo devolve os cards de uma tag do gráfico (clique).
func (a *App) CancelamentosDoMotivo(motivo string) ([]db.Cancelamento, error) {
	if a.cancelamentos == nil {
		return nil, fmt.Errorf("banco indisponível nesta instalação")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cs, err := a.cancelamentos.CancelamentosDoMotivo(ctx, db.ChaveMotivo(motivo))
	if err != nil {
		return nil, friendly("CancelamentosDoMotivo", err)
	}
	return cs, nil
}

// ---- Lista colada de cancelamentos ("NOME - MOTIVO - CANCELADO") ----

// LinhaImportCancel é uma linha da lista já interpretada e casada com o quadro.
type LinhaImportCancel struct {
	Linha      int      `json:"linha"`
	Nome       string   `json:"nome"`
	Motivo     string   `json:"motivo"`
	Resultado  string   `json:"resultado"`
	TicketID   int64    `json:"ticketId"`
	CodService string   `json:"codService"`
	Coluna     string   `json:"coluna"`
	Candidatos []string `json:"candidatos"` // nomes dos cards quando o casamento é ambíguo
}

// PreviaImportCancel é o que o modal mostra antes de gravar qualquer coisa.
type PreviaImportCancel struct {
	Cancelados  []LinhaImportCancel `json:"cancelados"`
	Revertidos  []LinhaImportCancel `json:"revertidos"`
	Tratamento  []LinhaImportCancel `json:"tratamento"`
	Ambiguos    []LinhaImportCancel `json:"ambiguos"`
	SemCard     []LinhaImportCancel `json:"semCard"` // sem card no quadro — o import tenta o 360
	Erros       []string            `json:"erros"`
	TotalLinhas int                 `json:"totalLinhas"`
}

// ResumoImportCancel é o resultado do import de verdade.
type ResumoImportCancel struct {
	Registrados int      `json:"registrados"`
	Movidos     int      `json:"movidos"`
	Importados  int      `json:"importados"` // card criado do 360
	JaFechados  int      `json:"jaFechados"` // card já estava na coluna de destino
	Falhas      []string `json:"falhas"`
}

// previaImportar interpreta o texto e casa cada nome com os cards do quadro
// (sem gravar nada) — a prévia e o import usam a mesma leitura.
func (a *App) previaImportar(texto string) (*PreviaImportCancel, []service.LinhaCancelamento, error) {
	if a.cancelamentos == nil {
		return nil, nil, fmt.Errorf("banco indisponível nesta instalação")
	}
	linhas, erros := service.ParseListaCancelamentos(texto)
	p := &PreviaImportCancel{Erros: erros}
	if len(linhas) == 0 {
		return p, linhas, nil
	}
	ts, err := a.tickets.List(a.ctx)
	if err != nil {
		return nil, nil, friendly("PreviaImportCancelamentos", err)
	}
	cards := make([]service.CardResumo, 0, len(ts))
	for _, t := range ts {
		cards = append(cards, service.CardResumo{ID: t.ID, NomeCliente: t.NomeCliente, CodService: t.CodService, ColunaAtual: t.ColunaAtual})
	}
	p.TotalLinhas = len(linhas)
	for _, l := range linhas {
		x := LinhaImportCancel{Linha: l.Linha, Nome: l.Nome, Motivo: l.Motivo, Resultado: l.Resultado}
		m, achou := service.CasarCardPorNome(l.Nome, cards)
		if achou && m.Ambiguo {
			x.Candidatos = m.Candidatos
			p.Ambiguos = append(p.Ambiguos, x)
			continue
		}
		if achou {
			x.TicketID, x.CodService, x.Coluna = m.TicketID, m.CodService, m.Coluna
		} else {
			p.SemCard = append(p.SemCard, x)
			continue
		}
		switch l.Resultado {
		case service.CancelamentoPerda:
			p.Cancelados = append(p.Cancelados, x)
		case service.CancelamentoReversao:
			p.Revertidos = append(p.Revertidos, x)
		default:
			p.Tratamento = append(p.Tratamento, x)
		}
	}
	return p, linhas, nil
}

// PreviaImportarCancelamentos analisa a lista colada e devolve as quatro
// listas para o modal mostrar — nada é gravado nem movido.
func (a *App) PreviaImportarCancelamentos(texto string) (*PreviaImportCancel, error) {
	p, _, err := a.previaImportar(texto)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// registrarCancelamentoLinho grava motivo (cria a tag se for nova) + resultado.
func (a *App) registrarCancelamentoLinho(ticketID int64, motivo, resultado string) error {
	if !service.CancelamentoResultadoValido(resultado) {
		return fmt.Errorf("resultado inválido: %s", resultado)
	}
	m, err := a.cancelamentos.MotivoCriarSeNaoExiste(a.ctx, motivo)
	if err != nil {
		return err
	}
	return a.cancelamentos.CancelarMarcar(a.ctx, ticketID, m.ID, resultado)
}

// codServiceDo360 procura o cliente pelo nome no 360 e devolve o código
// service quando o nome confere (mesma conferência dos vínculos automáticos).
func (a *App) codServiceDo360(ctx context.Context, nome string) (string, error) {
	c := a.rotaAtual()
	if c == nil {
		return "", rota360.ErrSemSessao
	}
	cod := a.resolverCodService(ctx, c, nome, map[string]string{}, &strVazio)
	if cod == "" {
		return "", fmt.Errorf("cliente não encontrado no 360 pelo nome")
	}
	return cod, nil
}

var strVazio string

// ImportarCancelamentos processa a lista colada de verdade: grava
// motivo+resultado de cada linha e cria (importando do 360 pelo nome) ou
// move o card — fechados para Ticket Fechado, em tratamento para a coluna
// escolhida no modal. Ambíguos e falhas voltam na lista de falhas, sem
// parar o lote.
func (a *App) ImportarCancelamentos(texto, colunaTratamento string) (*ResumoImportCancel, error) {
	p, _, err := a.previaImportar(texto)
	if err != nil {
		return nil, err
	}
	if p.TotalLinhas == 0 {
		return nil, fmt.Errorf("nenhuma linha reconhecida na lista")
	}
	res := &ResumoImportCancel{Falhas: []string{}}
	for _, x := range p.Erros {
		res.Falhas = append(res.Falhas, x)
	}
	destinoFechado := ColunaTicketFechado
	destinoTratamento := ""
	if colunaTratamento != "" {
		if canon, ok := a.colunaCanonica(colunaTratamento); ok {
			colunaTratamento = canon
		}
		if !a.colunaValida(colunaTratamento) {
			return nil, service.ErrColunaInvalida
		}
		destinoTratamento = colunaTratamento
	}

	// Ambíguo não mexe: mexer no card errado é pior que pedir de novo.
	for _, x := range p.Ambiguos {
		res.Falhas = append(res.Falhas, fmt.Sprintf("linha %d (%s): nome casa com vários cards (%s) — resolva no quadro e importe de novo",
			x.Linha, x.Nome, strings.Join(x.Candidatos, ", ")))
	}

	// Um card pode receber várias linhas (ex.: variante do mesmo nome) — o
	// registro é upsert, mas conta e move só na primeira vez que o card sai
	// da coluna de destino.
	processados := map[int64]bool{}

	gravar := func(x LinhaImportCancel) {
		destino := destinoFechado
		if x.Resultado == service.CancelamentoTratamento {
			destino = destinoTratamento
			if destino == "" {
				res.Falhas = append(res.Falhas, fmt.Sprintf("linha %d (%s): escolha a coluna de destino dos em tratamento", x.Linha, x.Nome))
				return
			}
		}
		if err := a.registrarCancelamentoLinho(x.TicketID, x.Motivo, x.Resultado); err != nil {
			res.Falhas = append(res.Falhas, fmt.Sprintf("linha %d (%s): %v", x.Linha, x.Nome, err))
			return
		}
		res.Registrados++
		if processados[x.TicketID] {
			return
		}
		processados[x.TicketID] = true
		t, err := a.tickets.Get(a.ctx, x.TicketID)
		if err != nil {
			res.Falhas = append(res.Falhas, fmt.Sprintf("linha %d (%s): %v", x.Linha, x.Nome, err))
			return
		}
		if t.ColunaAtual == destino {
			res.JaFechados++
			return
		}
		if err := a.MoveTicket(x.TicketID, destino, 0); err != nil {
			res.Falhas = append(res.Falhas, fmt.Sprintf("linha %d (%s): gravou o motivo, mas não moveu: %v", x.Linha, x.Nome, err))
			return
		}
		res.Movidos++
	}

	for _, x := range p.Cancelados {
		gravar(x)
	}
	for _, x := range p.Revertidos {
		gravar(x)
	}
	for _, x := range p.Tratamento {
		gravar(x)
	}

	// Sem card no quadro: tenta achar o cliente no 360 pelo nome e cria o
	// card direto na coluna de destino.
	for _, x := range p.SemCard {
		destino := destinoFechado
		if x.Resultado == service.CancelamentoTratamento {
			destino = destinoTratamento
			if destino == "" {
				res.Falhas = append(res.Falhas, fmt.Sprintf("linha %d (%s): escolha a coluna de destino dos em tratamento", x.Linha, x.Nome))
				continue
			}
		}
		ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
		cod, err := a.codServiceDo360(ctx, x.Nome)
		cancel()
		if err != nil {
			res.Falhas = append(res.Falhas, fmt.Sprintf("linha %d (%s): sem card no quadro e %v", x.Linha, x.Nome, err))
			continue
		}
		t, err := a.ImportarCliente360(cod, destino)
		if err != nil {
			res.Falhas = append(res.Falhas, fmt.Sprintf("linha %d (%s): importou do 360 (cod %s), mas falhou: %v", x.Linha, x.Nome, cod, err))
			continue
		}
		if err := a.registrarCancelamentoLinho(t.ID, x.Motivo, x.Resultado); err != nil {
			res.Falhas = append(res.Falhas, fmt.Sprintf("linha %d (%s): card criado (cod %s), mas falhou ao gravar o motivo: %v", x.Linha, x.Nome, cod, err))
			continue
		}
		res.Importados++
		res.Registrados++
	}
	return res, nil
}
