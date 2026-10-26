package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"rota-smart/internal/db"
	"rota-smart/internal/params"
	"rota-smart/internal/rota360"
	"rota-smart/internal/service"
)

// Chaves da tabela config.
const (
	cfgGroqChave       = "groq_chave"
	cfgGroqModelo      = "groq_modelo"
	cfgDeepInfraChave  = "deepinfra_chave"
	cfgDeepInfraModelo = "deepinfra_modelo"
	cfgIaProvedor      = "ia_provedor"
	cfgRotaCookie   = "rota360_cookie"
	cfgAutor360     = "autor_360" // nome do usuário logado no 360 (filtra o resumo do ticket)
	evRotaConectado = "rota360:conectado"
)

// Rota360Status é o estado da sessão com o portal 360.
type Rota360Status struct {
	Conectado bool   `json:"conectado"`
	Origem    string `json:"origem"` // "chrome", "manual" ou ""
	Mensagem  string `json:"mensagem"`
}

// rotaAtual devolve o cliente da sessão ativa, tentando o Chrome na primeira vez.
func (a *App) rotaAtual() *rota360.Cliente {
	a.muRota.Lock()
	defer a.muRota.Unlock()
	if a.rota != nil {
		return a.rota
	}
	if cookie := a.configGet(a.ctx, cfgRotaCookie); cookie != "" {
		if c, err := rota360.ParseCookieColado(cookie); err == nil {
			a.rota = rota360.NovoCliente(c)
			return a.rota
		}
	}
	return nil
}

// definirRota grava a sessão e persiste o cookie para reabrir o app logado.
func (a *App) definirRota(c *rota360.Cliente, origem, cookieBruto string) {
	a.muRota.Lock()
	a.rota = c
	a.muRota.Unlock()
	if a.config != nil {
		_ = a.config.Set(a.ctx, cfgRotaCookie, cookieBruto)
	}
	_ = origem
}

// Rota360Status informa ao frontend o estado da sessão do 360.
// Passivo: não abre janela nem reinicia nada — só a sessão salva e a
// leitura direta do banco do Chrome.
func (a *App) Rota360Status() Rota360Status {
	if c := a.rotaAtual(); c != nil && c.Autenticado() {
		return Rota360Status{Conectado: true, Origem: "sessão salva"}
	}
	if cookie, err := rota360.DetectaCookieChrome(); err == nil {
		c := rota360.NovoCliente(cookie)
		a.muRota.Lock()
		a.rota = c
		a.muRota.Unlock()
		return Rota360Status{Conectado: true, Origem: "chrome"}
	}
	return Rota360Status{Conectado: false}
}

// ConectarRota360 é o botão explícito: tenta o banco do Chrome e, em falha,
// o Chrome DevTools Protocol (janela de conexão do app).
// A detecção via CDP pode levar minutos (espera o login na janela do
// Chrome), então roda em goroutine e o frontend acompanha via Rota360Status
// / evento "rota360:conectado" — a chamada volta na hora.
func (a *App) ConectarRota360() (Rota360Status, error) {
	if st := a.Rota360Status(); st.Conectado {
		return st, nil
	}
	a.muConectando.Lock()
	jaRodando := a.conectando360
	a.conectando360 = true
	a.muConectando.Unlock()
	if jaRodando {
		return Rota360Status{Conectado: false,
			Mensagem: "Conexão já em andamento — faça o login na janela do Chrome."}, nil
	}
	go func() {
		defer func() {
			a.muConectando.Lock()
			a.conectando360 = false
			a.muConectando.Unlock()
		}()
		c, origem, err := rota360.SessaoDetectada()
		if err != nil {
			runtime.EventsEmit(a.ctx, evRotaConectado,
				Rota360Status{Conectado: false, Mensagem: err.Error()})
			return
		}
		a.muRota.Lock()
		a.rota = c
		a.muRota.Unlock()
		// Persiste para a sessão sobreviver a reinícios do app (a leitura
		// passiva do banco do Chrome não é mais possível no Chrome recente).
		if ck := c.CookieSalva(); ck != nil {
			a.definirRota(c, origem, ck.Nome+"="+ck.Valor)
			// O nome vindo do cabeçalho do portal é quem está logado — é o
			// "autor da sessão" que o resumo de ticket usa. Detecção vence
			// valor manual: se trocar o login, o filtro acompanha.
			if ck.Usuario != "" && a.config != nil {
				_ = a.config.Set(a.ctx, cfgAutor360, ck.Usuario)
			}
		}
		runtime.EventsEmit(a.ctx, evRotaConectado,
			Rota360Status{Conectado: true, Origem: origem})
	}()
	return Rota360Status{Conectado: false,
		Mensagem: "Abrindo janela de conexão do Chrome — faça o login no 360; o app conecta sozinho."}, nil
}

// DefinirCookieRota360 grava um cookie colado manualmente (fallback).
func (a *App) DefinirCookieRota360(cookieTxt string) (Rota360Status, error) {
	c, err := rota360.ParseCookieColado(cookieTxt)
	if err != nil {
		return Rota360Status{}, err
	}
	a.definirRota(rota360.NovoCliente(c), "manual", strings.TrimSpace(cookieTxt))
	return Rota360Status{Conectado: true, Origem: "manual"}, nil
}

// DesconectarRota360 limpa a sessão salva.
func (a *App) DesconectarRota360() error {
	a.muRota.Lock()
	a.rota = nil
	a.muRota.Unlock()
	if a.config != nil {
		return a.config.Set(a.ctx, cfgRotaCookie, "")
	}
	return nil
}

// Rota360BuscarClientes pesquisa cliente por código service ou nome.
func (a *App) Rota360BuscarClientes(q string) ([]rota360.ResumoCliente, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []rota360.ResumoCliente{}, nil
	}
	c := a.rotaAtual()
	if c == nil {
		return nil, rota360.ErrSemSessao
	}
	filtro := "todos"
	if _, err := strconv.ParseInt(q, 10, 64); err == nil {
		filtro = "codcliente"
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	res, err := c.BuscarClientes(ctx, q, filtro)
	if err != nil {
		return nil, friendly("Rota360BuscarClientes", err)
	}
	if res == nil {
		res = []rota360.ResumoCliente{}
	}
	return res, nil
}

// Rota360PesquisarTickets pesquisa tickets do helpdesk por número, código
// service do cliente ou nome do solicitante (lê as filas em aberto do 360).
func (a *App) Rota360PesquisarTickets(q string) ([]rota360.Ticket360, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []rota360.Ticket360{}, nil
	}
	c := a.rotaAtual()
	if c == nil {
		return nil, rota360.ErrSemSessao
	}
	ctx, cancel := context.WithTimeout(a.ctx, 25*time.Second)
	defer cancel()
	res, err := c.PesquisarTickets(ctx, q)
	if err != nil {
		return nil, friendly("Rota360PesquisarTickets", err)
	}
	if res == nil {
		res = []rota360.Ticket360{}
	}
	return res, nil
}

// Rota360AbrirTicket abre o ticket do 360 (pelo número exibido) no navegador
// — o botão 🌐 da pesquisa de tickets do Shift+F4.
func (a *App) Rota360AbrirTicket(numero string) (string, error) {
	numero = strings.TrimSpace(numero)
	if numero == "" {
		return "", fmt.Errorf("número do ticket vazio")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	msg, _, err := a.agentAbrirTicket(ctx, numero)
	return msg, err
}

// ImportarCliente360 cria um card no Kanban com os dados do 360 e
// sincroniza o cache para avaliação/financeiro posterior.
func (a *App) ImportarCliente360(codService, coluna string) (db.Ticket, error) {
	codService = strings.TrimSpace(codService)
	if !service.ColunaValida(coluna) {
		return db.Ticket{}, service.ErrColunaInvalida
	}
	c := a.rotaAtual()
	if c == nil {
		return db.Ticket{}, rota360.ErrSemSessao
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()

	cliente, fin, err := a.sincronizarCliente(ctx, c, codService)
	if err != nil {
		return db.Ticket{}, friendly("ImportarCliente360", err)
	}

	endereco := strings.TrimSpace(cliente.Endereco + " " + cliente.NumEndereco)
	t := db.Ticket{
		NomeCliente: cliente.Nome,
		TipoTicket:  "Outro",
		CodService:  codService,
		Endereco:    endereco,
		Telefone:    cliente.Fone1,
		ColunaAtual: coluna,
	}
	if len(t.Endereco) > service.MaxEndereco {
		t.Endereco = t.Endereco[:service.MaxEndereco]
	}
	if len(t.Telefone) > service.MaxTelefone {
		t.Telefone = t.Telefone[:service.MaxTelefone]
	}
	if len(t.NomeCliente) > service.MaxNomeCliente {
		t.NomeCliente = t.NomeCliente[:service.MaxNomeCliente]
	}
	id, err := a.tickets.Save(a.ctx, &t)
	if err != nil {
		return db.Ticket{}, friendly("ImportarCliente360", err)
	}
	salvo, err := a.tickets.Get(a.ctx, id)
	if err != nil {
		return db.Ticket{}, friendly("ImportarCliente360", err)
	}
	a.vinculoAutomaticoTela(salvo.ID, salvo.CodService, salvo.NomeCliente)
	_ = fin
	return *salvo, nil
}

// resumoCliente360 é o cadastro bruto da busca (campos do ERP).
type resumoCliente360 struct {
	CodCliente  int64  `json:"CodCliente"`
	Nome        string `json:"Nome"`
	CGCCPF      string `json:"CGCCPF"`
	Fone1       string `json:"Fone1"`
	Endereco    string `json:"Endereco"`
	NumEndereco string `json:"NumEndereco"`
	Situacao    string `json:"Situacao"`
}

// sincronizarCliente grava o snapshot do 360 no cache e devolve cadastro+dashboard.
func (a *App) sincronizarCliente(ctx context.Context, c *rota360.Cliente, codService string) (*resumoCliente360, *rota360.Dashboard360, error) {
	var busca []rota360.ResumoCliente
	filtro := "codcliente"
	if _, err := strconv.ParseInt(codService, 10, 64); err != nil {
		filtro = "todos"
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	busca, err := c.BuscarClientes(ctx, codService, filtro)
	if err != nil {
		return nil, nil, err
	}
	if len(busca) == 0 {
		return nil, nil, fmt.Errorf("cliente %s não encontrado no 360", codService)
	}
	r := &resumoCliente360{
		CodCliente: busca[0].CodCliente, Nome: busca[0].Nome, CGCCPF: busca[0].CGCCPF,
		Fone1: busca[0].Fone1, Endereco: busca[0].Endereco, NumEndereco: busca[0].NumEndereco,
	}
	fin, err := c.FinanceiroCliente(ctx, codService)
	if err != nil {
		return r, nil, nil // cadastro ok; financeiro falhou — segue sem ele
	}
	cache := &db.Cliente360{
		CodService: codService,
		Nome:       r.Nome,
		CNPJ:       r.CGCCPF,
		Telefone:   r.Fone1,
		Endereco:   strings.TrimSpace(r.Endereco + " " + r.NumEndereco),
		SaldoCents: floatParaCents(fin.Sources.InsideService.Data.Financeiro.TotalDevendoComEncargos),
	}
	faturas, _ := json.Marshal(fin.Sources.InsideService.Data.FaturasAbertas)
	cache.FaturasJSON = string(faturas)
	hist, _ := json.Marshal(fin)
	cache.HistoricoJSON = string(hist)
	if err := a.clientes.Upsert(ctx, cache); err != nil {
		return r, fin, nil
	}
	return r, fin, nil
}

// ResultadoCobranca é o pacote devolvido ao frontend ao arrastar para Cobrança.
type ResultadoCobranca struct {
	Ticket     db.Ticket             `json:"ticket"`
	Cobranca   db.Cobranca           `json:"cobranca"`
	Proposta   service.Proposta      `json:"proposta"`
	Avaliacao  *service.Avaliacao    `json:"avaliacao"`
	Financeiro rota360.Financeiro360 `json:"financeiro"`
	Faturas    []rota360.Fatura360   `json:"faturas"`
	Aviso      string                `json:"aviso"`
}

// PrepararCobranca roda o fluxo automático da coluna Cobrança: puxa o
// financeiro do 360, avalia o cliente, salva o acordo padrão e gera a proposta.
func (a *App) PrepararCobranca(ticketID int64) (ResultadoCobranca, error) {
	t, err := a.tickets.Get(a.ctx, ticketID)
	if err != nil {
		return ResultadoCobranca{}, friendly("PrepararCobranca", err)
	}
	c := a.rotaAtual()
	if c == nil {
		return ResultadoCobranca{}, rota360.ErrSemSessao
	}
	if strings.TrimSpace(t.CodService) == "" {
		return ResultadoCobranca{}, fmt.Errorf("card sem código service — importe o cliente do 360")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 25*time.Second)
	defer cancel()

	res := ResultadoCobranca{Ticket: *t}
	var totalCents int64
	if _, fin, err := a.sincronizarCliente(ctx, c, t.CodService); err != nil {
		res.Aviso = "Não foi possível atualizar o financeiro do 360; usando valores em cache."
		if cli, _ := a.clientes.Get(ctx, t.CodService); cli != nil {
			totalCents = cli.SaldoCents
		}
	} else if fin != nil {
		res.Financeiro = fin.Sources.InsideService.Data.Financeiro
		if fin.Sources.InsideService.Data.FaturasAbertas != nil {
			res.Faturas = fin.Sources.InsideService.Data.FaturasAbertas
		}
		totalCents = floatParaCents(res.Financeiro.TotalDevendoComEncargos)
	} else if cli, _ := a.clientes.Get(ctx, t.CodService); cli != nil {
		totalCents = cli.SaldoCents
	}

	if totalCents <= 0 {
		return ResultadoCobranca{}, fmt.Errorf("sem saldo em aberto no 360 para o cliente %s", t.CodService)
	}
	if res.Faturas == nil {
		res.Faturas = []rota360.Fatura360{}
	}

	// Acordo padrão: total do 360, sem entrada, 12x, sem parcela base.
	in := service.ReparcelamentoInput{
		TotalAbertoCents: totalCents,
		EntradaCents:     0,
		NumeroVezes:      12,
		ParcelaBaseCents: 0,
	}
	cob, err := a.SaveCobranca(db.Cobranca{
		TicketID:         ticketID,
		TotalAbertoCents: in.TotalAbertoCents,
		EntradaCents:     in.EntradaCents,
		NumeroVezes:      in.NumeroVezes,
		ParcelaBaseCents: in.ParcelaBaseCents,
	})
	if err != nil {
		return ResultadoCobranca{}, friendly("PrepararCobranca", err)
	}
	res.Cobranca = cob

	if a.avaliador != nil {
		if av, err := a.avaliador.Avaliar(ctx, ticketID, t.CodService); err == nil {
			res.Avaliacao = av
		}
	}

	prop, err := a.GenerateProposal(ticketID, in, false)
	if err != nil {
		return ResultadoCobranca{}, friendly("PrepararCobranca", err)
	}
	res.Proposta = prop
	return res, nil
}

// floatParaCents converte reais (float do 360) em centavos com arredondamento
// explícito — floats nunca atravessam para o domínio de dinheiro.
func floatParaCents(v float64) int64 {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return int64(math.Round(v * 100))
}

// RenegociacaoPronta é o resultado do botão "Gerar renegociação" (card na
// coluna Cobrança): valores em aberto puxados do 360, contrato (mensalidade
// exata, vencimento, vigência), cortesia e a proposta no padrão fixo.
type RenegociacaoPronta struct {
	TotalAbertoCents          int64                 `json:"totalAbertoCents"`
	TotalVencidoCents         int64                 `json:"totalVencidoCents"`         // faturas já vencidas, sem encargos
	TotalVencidoEncargosCents int64                 `json:"totalVencidoEncargosCents"` // vencidas + multa/mora
	EntradaCents              int64                 `json:"entradaCents"`
	NumeroVezes               int                   `json:"numeroVezes"`
	ParcelaBaseCents          int64                 `json:"parcelaBaseCents"`
	ValorMensalidadeCents     int64                 `json:"valorMensalidadeCents"` // mensalidade exata do contrato
	DiaVencimento             int                   `json:"diaVencimento"`
	ContratoAssinatura        string                `json:"contratoAssinatura"`
	ContratoPrazoMeses        int                   `json:"contratoPrazoMeses"`
	ContratoTermino           string                `json:"contratoTermino"`
	DireitoCortesia           bool                  `json:"direitoCortesia"`
	CortesiaRestantes         int                   `json:"cortesiaRestantes"`
	CortesiaMotivo            string                `json:"cortesiaMotivo"`
	Proposta                  service.Proposta      `json:"proposta"`
	Avaliacao                 *service.Avaliacao    `json:"avaliacao"`
	Financeiro                rota360.Financeiro360 `json:"financeiro"`
	Faturas                   []rota360.Fatura360   `json:"faturas"`
	Aviso                     string                `json:"aviso"`
}

// cortesiasUsadas conta quantas cortesias o cliente já gastou, lendo as
// observações do cadastro (regra do setor): quando a frase diz "usou N
// cortesias" (ou "cortesia 2x", "cortesia de 1 mês"), vale N; menção sem
// número vale as 3. Devolve o máximo achado.
func cortesiasUsadas(obs string) int {
	reLinha := regexp.MustCompile(`(?i)[^\r\n]*cortesia[^\r\n]*`)
	reDepois := regexp.MustCompile(`(?i)(\d{1,2})\s*cortesias?`)
	reAntes := regexp.MustCompile(`(?i)cortesias?\s*(?:x|de)?\s*([1-3])(?:[^0-9]|$)`)
	usadas := 0
	for _, linha := range reLinha.FindAllString(obs, -1) {
		n := 0
		if m := reDepois.FindStringSubmatch(linha); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil && v >= 1 && v <= 3 {
				n = v
			}
		}
		if n == 0 {
			if m := reAntes.FindStringSubmatch(linha); m != nil {
				n, _ = strconv.Atoi(m[1])
			}
		}
		if n > usadas {
			usadas = n
		} else if n == 0 {
			usadas = 3 // menção sem número: usou todas
		}
	}
	return usadas
}

// avaliarCortesia aplica a regra do usuário: contrato com mais de 1 ano dá
// direito a até 3 cortesias (1 mensalidade cada) — as já usadas, contadas
// nas observações do cadastro, são descontadas do total de 3.
func avaliarCortesia(agora time.Time, assinatura time.Time, obs string) (restantes int, motivo string) {
	const maxCortesias = 3
	if assinatura.IsZero() {
		return 0, "sem data de contrato no 360 — confirmar elegibilidade com o comercial"
	}
	meses := int(agora.Sub(assinatura).Hours() / (24 * 30))
	usadas := cortesiasUsadas(obs)
	restantes = maxCortesias - usadas
	if restantes < 0 {
		restantes = 0
	}
	switch {
	case meses < 12:
		return 0, fmt.Sprintf("contrato há %d mês(es) — cortesia exige mais de 1 ano", meses)
	case usadas >= maxCortesias:
		return 0, fmt.Sprintf("contrato há %d meses, mas já usou as %d cortesias", meses, usadas)
	default:
		return restantes, fmt.Sprintf("contrato há %d meses; %d cortesia(s) já usada(s) — resta(m) %d", meses, usadas, restantes)
	}
}

// contratoVigente escolhe o contrato mais relevante: prefere mensalidade
// preenchida e situação não cancelada; empate fica com o primeiro.
func contratoVigente(contratos []rota360.Contrato360) *rota360.Contrato360 {
	var melhor *rota360.Contrato360
	for i := range contratos {
		ct := &contratos[i]
		if melhor == nil {
			melhor = ct
			continue
		}
		melhorCancelado := strings.Contains(strings.ToLower(melhor.Situacao), "cancel")
		ctCancelado := strings.Contains(strings.ToLower(ct.Situacao), "cancel")
		switch {
		case melhorCancelado && !ctCancelado:
			melhor = ct
		case melhor.ValorME <= 0 && ct.ValorME > 0:
			melhor = ct
		}
	}
	return melhor
}

// GerarRenegociacao puxa os valores em aberto do 360, o contrato (mensalidade
// exata e vigência), avalia a cortesia pelas observações do cadastro e gera a
// proposta no padrão. As condições da Política (PDF nas Configurações) são
// lidas à parte via ListarParametros. Não grava acordo: o usuário ajusta
// entrada/vezes e clica em salvar.
func (a *App) GerarRenegociacao(ticketID int64) (RenegociacaoPronta, error) {
	t, err := a.tickets.Get(a.ctx, ticketID)
	if err != nil {
		return RenegociacaoPronta{}, friendly("GerarRenegociacao", err)
	}
	c := a.rotaAtual()
	if c == nil {
		return RenegociacaoPronta{}, rota360.ErrSemSessao
	}
	if strings.TrimSpace(t.CodService) == "" {
		return RenegociacaoPronta{}, fmt.Errorf("card sem código service — importe o cliente do 360")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 25*time.Second)
	defer cancel()

	res := RenegociacaoPronta{EntradaCents: 0, NumeroVezes: 12, Faturas: []rota360.Fatura360{}}
	var totalCents int64
	if _, fin, err := a.sincronizarCliente(ctx, c, t.CodService); err != nil {
		res.Aviso = "Não foi possível atualizar o financeiro do 360; usando valores em cache."
		if cli, _ := a.clientes.Get(ctx, t.CodService); cli != nil {
			totalCents = cli.SaldoCents
			res.Faturas = faturasDoCache(cli)
		}
	} else if fin != nil {
		res.Financeiro = fin.Sources.InsideService.Data.Financeiro
		if fin.Sources.InsideService.Data.FaturasAbertas != nil {
			res.Faturas = fin.Sources.InsideService.Data.FaturasAbertas
		}
		totalCents = floatParaCents(res.Financeiro.TotalDevendoComEncargos)
	} else if cli, _ := a.clientes.Get(ctx, t.CodService); cli != nil {
		totalCents = cli.SaldoCents
		res.Faturas = faturasDoCache(cli)
	}
	if totalCents <= 0 {
		return RenegociacaoPronta{}, fmt.Errorf("sem saldo em aberto no 360 para o cliente %s", t.CodService)
	}
	res.TotalAbertoCents = totalCents

	// Valores já vencidos (para a oferta à vista): soma das faturas atrasadas.
	for _, f := range res.Faturas {
		if f.DiasAtraso > 0 {
			res.TotalVencidoCents += floatParaCents(f.ValorDuplicata)
			res.TotalVencidoEncargosCents += floatParaCents(f.ValorDuplicata + f.MultaValor + f.MoraValor)
		}
	}

	// Parcela base: mensalidade exata do contrato no 360; sem contrato, cai
	// para a mensalidade dos produtos e depois para o acordo ativo.
	var assinatura time.Time
	if contratos, err := c.ContratosCliente(ctx, t.CodService); err == nil && len(contratos) > 0 {
		if ct := contratoVigente(contratos); ct != nil {
			res.ValorMensalidadeCents = floatParaCents(ct.ValorME)
			res.DiaVencimento = ct.DiaVcto
			res.ContratoPrazoMeses = ct.Prazo
			if !ct.DataAssinatura.IsZero() {
				assinatura = ct.DataAssinatura
				res.ContratoAssinatura = ct.DataAssinatura.Format("02/01/2006")
			}
			if !ct.TerminoContrato.IsZero() {
				res.ContratoTermino = ct.TerminoContrato.Format("02/01/2006")
			}
		}
	} else if cli, _ := a.clientes.Get(ctx, t.CodService); cli != nil && cli.ContratosJSON != "" {
		var contratos []rota360.Contrato360
		if json.Unmarshal([]byte(cli.ContratosJSON), &contratos) == nil && len(contratos) > 0 {
			if ct := contratoVigente(contratos); ct != nil {
				res.ValorMensalidadeCents = floatParaCents(ct.ValorME)
				res.DiaVencimento = ct.DiaVcto
				res.ContratoPrazoMeses = ct.Prazo
				if !ct.DataAssinatura.IsZero() {
					assinatura = ct.DataAssinatura
					res.ContratoAssinatura = ct.DataAssinatura.Format("02/01/2006")
				}
				if !ct.TerminoContrato.IsZero() {
					res.ContratoTermino = ct.TerminoContrato.Format("02/01/2006")
				}
			}
		}
	}
	res.ParcelaBaseCents = res.ValorMensalidadeCents

	// Cortesia: precisa das observações do cadastro (contam quantas já usou).
	observacoes := ""
	if dc, err := c.DadosCliente(ctx, t.CodService); err == nil {
		observacoes = dc.Observacoes
		if assinatura.IsZero() && !dc.AtivoDesde.IsZero() {
			assinatura = dc.AtivoDesde
			if res.ContratoAssinatura == "" {
				res.ContratoAssinatura = dc.AtivoDesde.Format("02/01/2006")
			}
		}
		if res.ValorMensalidadeCents <= 0 && dc.Faturamento.ValorNF > 0 {
			res.ValorMensalidadeCents = floatParaCents(dc.Faturamento.ValorNF)
			res.ParcelaBaseCents = res.ValorMensalidadeCents
		}
		if res.DiaVencimento == 0 {
			res.DiaVencimento = dc.Faturamento.DiaVencimento
		}
	}
	res.CortesiaRestantes, res.CortesiaMotivo = avaliarCortesia(time.Now(), assinatura, observacoes)
	res.DireitoCortesia = res.CortesiaRestantes > 0

	if res.ParcelaBaseCents <= 0 && a.avaliador != nil {
		if av, err := a.avaliador.Avaliar(ctx, ticketID, t.CodService); err == nil {
			res.Avaliacao = av
			res.ParcelaBaseCents = av.MensalidadeCents
		}
	} else if a.avaliador != nil {
		if av, err := a.avaliador.Avaliar(ctx, ticketID, t.CodService); err == nil {
			res.Avaliacao = av
		}
	}
	if res.ParcelaBaseCents <= 0 {
		if cob, _ := a.cobrancas.GetAtiva(a.ctx, ticketID); cob != nil {
			res.ParcelaBaseCents = cob.ParcelaBaseCents
		}
	}

	// Política v7: entrada mínima de 20% (primeira renegociação) ou 40%
	// (cliente já renegociou), num múltiplo de R$ 50 pra cima.
	jaRenegociou := res.Avaliacao != nil && res.Avaliacao.Renegociou
	res.EntradaCents = service.EntradaMinima(res.TotalAbertoCents, jaRenegociou)

	in := service.ReparcelamentoInput{
		TotalAbertoCents: res.TotalAbertoCents,
		EntradaCents:     res.EntradaCents,
		NumeroVezes:      res.NumeroVezes,
		ParcelaBaseCents: res.ParcelaBaseCents,
	}
	prop, err := a.GenerateProposal(ticketID, in, false)
	if err != nil {
		return RenegociacaoPronta{}, friendly("GerarRenegociacao", err)
	}
	res.Proposta = prop
	return res, nil
}

// faturasDoCache devolve as faturas guardadas no cache do cliente (usadas
// quando o 360 não responde na hora).
func faturasDoCache(cli *db.Cliente360) []rota360.Fatura360 {
	if cli == nil || cli.FaturasJSON == "" {
		return []rota360.Fatura360{}
	}
	var faturas []rota360.Fatura360
	if json.Unmarshal([]byte(cli.FaturasJSON), &faturas) != nil {
		return []rota360.Fatura360{}
	}
	if faturas == nil {
		return []rota360.Fatura360{}
	}
	return faturas
}

// ---- Configurações (Groq + parâmetros) ----

// ConfigApp é o estado não-sensível das configurações (a chave nunca volta).
type ConfigApp struct {
	GroqModelo        string `json:"groqModelo"`
	GroqTemChave      bool   `json:"groqTemChave"`
	Provedor          string `json:"provedor"`          // "groq" (padrão), "deepinfra" ou "auto"
	DeepInfraTemChave bool   `json:"deepInfraTemChave"`
	Autor360      string `json:"autor360"`
	RotaConectado bool   `json:"rotaConectado"`
	RotaOrigem    string `json:"rotaOrigem"`
	RotaMensagem  string `json:"rotaMensagem"`
}

// GetConfig devolve as configurações atuais.
func (a *App) GetConfig() ConfigApp {
	return ConfigApp{
		GroqModelo:        a.groqModeloAtual(),
		GroqTemChave:      a.configGet(a.ctx, cfgGroqChave) != "",
		Provedor:          a.provedorIA(),
		DeepInfraTemChave: a.configGet(a.ctx, cfgDeepInfraChave) != "",
		Autor360:          a.configGet(a.ctx, cfgAutor360),
		RotaConectado:     a.rotaAtual() != nil,
	}
}

// provedorIA devolve o provedor ativo ("groq" é o default; "auto" usa a Groq
// e manda pro DeepInfra no teto do free).
func (a *App) provedorIA() string {
	switch p := a.configGet(a.ctx, cfgIaProvedor); p {
	case service.ProvedorDeepInfra, service.ProvedorAuto:
		return p
	}
	return service.ProvedorGroq
}

// SetProvedorIA escolhe entre Groq, DeepInfra e auto (Groq → DeepInfra).
func (a *App) SetProvedorIA(provedor string) error {
	if a.config == nil {
		return fmt.Errorf("configuração indisponível")
	}
	switch provedor {
	case service.ProvedorGroq, service.ProvedorDeepInfra, service.ProvedorAuto:
	default:
		return fmt.Errorf("provedor inválido: %s", provedor)
	}
	return a.config.Set(a.ctx, cfgIaProvedor, provedor)
}

// SetDeepInfraConfig grava a chave e o modelo da DeepInfra. Chave vazia não
// apaga (para limpar, use LimparDeepInfraChave).
func (a *App) SetDeepInfraConfig(chave, modelo string) error {
	if a.config == nil {
		return fmt.Errorf("configuração indisponível")
	}
	if chave = strings.TrimSpace(chave); chave != "" {
		if err := a.config.Set(a.ctx, cfgDeepInfraChave, chave); err != nil {
			return friendly("SetDeepInfraConfig", err)
		}
	}
	if modelo = strings.TrimSpace(modelo); modelo != "" {
		if err := a.config.Set(a.ctx, cfgDeepInfraModelo, modelo); err != nil {
			return friendly("SetDeepInfraConfig", err)
		}
	}
	return nil
}

// LimparDeepInfraChave apaga a chave guardada.
func (a *App) LimparDeepInfraChave() error {
	if a.config == nil {
		return nil
	}
	return a.config.Set(a.ctx, cfgDeepInfraChave, "")
}

// SetAutor360 grava o nome do usuário logado no 360 — é quem o resumo do
// ticket trata como "autor da sessão" ao escolher a última interação.
func (a *App) SetAutor360(nome string) error {
	if a.config == nil {
		return fmt.Errorf("configuração indisponível")
	}
	return a.config.Set(a.ctx, cfgAutor360, strings.TrimSpace(nome))
}

func (a *App) groqModeloAtual() string {
	if m := a.configGet(a.ctx, cfgGroqModelo); m != "" {
		return m
	}
	return service.GroqModeloPadrao
}

// SetGroqConfig grava a chave e o modelo da Groq. Chave vazia não apaga
// (para limpar, use LimparGroqChave).
func (a *App) SetGroqConfig(chave, modelo string) error {
	if a.config == nil {
		return fmt.Errorf("configuração indisponível")
	}
	if chave = strings.TrimSpace(chave); chave != "" {
		if err := a.config.Set(a.ctx, cfgGroqChave, chave); err != nil {
			return friendly("SetGroqConfig", err)
		}
	}
	if modelo = strings.TrimSpace(modelo); modelo != "" {
		if err := a.config.Set(a.ctx, cfgGroqModelo, modelo); err != nil {
			return friendly("SetGroqConfig", err)
		}
	}
	return nil
}

// LimparGroqChave apaga a chave guardada.
func (a *App) LimparGroqChave() error {
	if a.config == nil {
		return nil
	}
	return a.config.Set(a.ctx, cfgGroqChave, "")
}

// groqChave é a fonte consultada a cada chamada de IA.
func (a *App) groqChave() string { return a.configGet(a.ctx, cfgGroqChave) }

// SalvarParametroTexto grava um parâmetro digitado pelo usuário.
func (a *App) SalvarParametroTexto(texto string) (db.ParametroNegociacao, error) {
	texto = strings.TrimSpace(texto)
	if texto == "" {
		return db.ParametroNegociacao{}, fmt.Errorf("texto do parâmetro vazio")
	}
	if len(texto) > 50000 {
		return db.ParametroNegociacao{}, service.ErrTextoMuitoLongo
	}
	p := db.ParametroNegociacao{Fonte: "texto", Conteudo: texto}
	id, err := a.parametros.Save(a.ctx, &p)
	if err != nil {
		return db.ParametroNegociacao{}, friendly("SalvarParametroTexto", err)
	}
	p.ID = id
	return p, nil
}

// EscolherEAnexarParametro abre o diálogo de arquivo, extrai o texto e grava.
func (a *App) EscolherEAnexarParametro() (*db.ParametroNegociacao, error) {
	caminho, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Anexar parâmetros de negociação",
		Filters: []runtime.FileFilter{
			{DisplayName: "Documentos (pdf, docx, txt)", Pattern: "*.pdf;*.docx;*.txt;*.md;*.csv"},
		},
	})
	if err != nil {
		return nil, friendly("EscolherEAnexarParametro", err)
	}
	if caminho == "" {
		return nil, nil // cancelado
	}
	return a.AnexarParametroArquivo(caminho)
}

// AnexarParametroArquivo lê o arquivo, extrai texto e grava como parâmetro.
func (a *App) AnexarParametroArquivo(caminho string) (*db.ParametroNegociacao, error) {
	conteudo, err := params.LerEExtrair(caminho)
	if err != nil {
		return nil, err // mensagens do parser são para o usuário
	}
	nome := caminho
	if i := strings.LastIndexAny(caminho, `\/`); i >= 0 {
		nome = caminho[i+1:]
	}
	p := db.ParametroNegociacao{Fonte: "arquivo", NomeArquivo: nome, Conteudo: conteudo}
	id, err := a.parametros.Save(a.ctx, &p)
	if err != nil {
		return nil, friendly("AnexarParametroArquivo", err)
	}
	p.ID = id
	return &p, nil
}

// ListarParametros devolve todos os parâmetros de negociação.
func (a *App) ListarParametros() ([]db.ParametroNegociacao, error) {
	lista, err := a.parametros.List(a.ctx)
	if err != nil {
		return nil, friendly("ListarParametros", err)
	}
	if lista == nil {
		lista = []db.ParametroNegociacao{}
	}
	return lista, nil
}

// RemoverParametro apaga um parâmetro pelo id.
func (a *App) RemoverParametro(id int64) error {
	if err := a.parametros.Delete(a.ctx, id); err != nil {
		return friendly("RemoverParametro", err)
	}
	return nil
}

// GetAvaliacaoCliente devolve a avaliação do cliente de um ticket.
func (a *App) GetAvaliacaoCliente(ticketID int64) (*service.Avaliacao, error) {
	t, err := a.tickets.Get(a.ctx, ticketID)
	if err != nil {
		return nil, friendly("GetAvaliacaoCliente", err)
	}
	if a.avaliador == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
	defer cancel()
	return a.avaliador.Avaliar(ctx, ticketID, t.CodService)
}
