package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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
	cfgGroqChave  = "groq_chave"
	cfgGroqModelo = "groq_modelo"
	cfgRotaCookie = "rota360_cookie"
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
func (a *App) ConectarRota360() (Rota360Status, error) {
	if st := a.Rota360Status(); st.Conectado {
		return st, nil
	}
	c, origem, err := rota360.SessaoDetectada()
	if err != nil {
		st := Rota360Status{Conectado: false, Mensagem: err.Error()}
		return st, fmt.Errorf("%s", err.Error())
	}
	a.muRota.Lock()
	a.rota = c
	a.muRota.Unlock()
	// Persiste para a sessão sobreviver a reinícios do app (a leitura
	// passiva do banco do Chrome não é mais possível no Chrome recente).
	if ck := c.CookieSalva(); ck != nil {
		a.definirRota(c, origem, ck.Nome+"="+ck.Valor)
	}
	return Rota360Status{Conectado: true, Origem: origem}, nil
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
		totalCents = floatParaCents(res.Financeiro.TotalDevendoComEncargos)
	} else if cli, _ := a.clientes.Get(ctx, t.CodService); cli != nil {
		totalCents = cli.SaldoCents
	}

	if totalCents <= 0 {
		return ResultadoCobranca{}, fmt.Errorf("sem saldo em aberto no 360 para o cliente %s", t.CodService)
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

// ---- Configurações (Groq + parâmetros) ----

// ConfigApp é o estado não-sensível das configurações (a chave nunca volta).
type ConfigApp struct {
	GroqModelo    string `json:"groqModelo"`
	GroqTemChave  bool   `json:"groqTemChave"`
	RotaConectado bool   `json:"rotaConectado"`
	RotaOrigem    string `json:"rotaOrigem"`
	RotaMensagem  string `json:"rotaMensagem"`
}

// GetConfig devolve as configurações atuais.
func (a *App) GetConfig() ConfigApp {
	return ConfigApp{
		GroqModelo:    a.groqModeloAtual(),
		GroqTemChave:  a.configGet(a.ctx, cfgGroqChave) != "",
		RotaConectado: a.rotaAtual() != nil,
	}
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
