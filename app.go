package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"rota-smart/internal/backup"
	"rota-smart/internal/db"
	"rota-smart/internal/rota360"
	"rota-smart/internal/service"
)

// App expõe a API para o frontend via bridge do Wails (Etapa 5).
// Os métodos públicos são a única ponte com o React.
type App struct {
	ctx       context.Context
	db        *sql.DB
	tickets   *db.TicketRepo
	cobrancas *db.CobrancaRepo
	colunas   *db.ColumnRepo
	propostas *service.PropostaService

	// Integração Rota 360 + Groq + parâmetros (wired em main; nil em testes antigos).
	config     *db.ConfigRepo
	parametros *db.ParametroRepo
	clientes   *db.Cliente360Repo
	groq       *service.GroqCliente
	avaliador  *service.Avaliador

	muRota sync.Mutex
	rota   *rota360.Cliente // sessão ativa do 360; nil se desconectado
}

// WireIntegracao injeta as dependências da integração (chamado pelo main).
func (a *App) WireIntegracao(config *db.ConfigRepo, parametros *db.ParametroRepo,
	clientes *db.Cliente360Repo, groq *service.GroqCliente, avaliador *service.Avaliador) {
	a.config = config
	a.parametros = parametros
	a.clientes = clientes
	a.groq = groq
	a.avaliador = avaliador
}

// configGet lê uma chave da tabela config; vazia se não houver wiring.
func (a *App) configGet(ctx context.Context, chave string) string {
	if a.config == nil {
		return ""
	}
	v, err := a.config.Get(ctx, chave)
	if err != nil {
		return ""
	}
	return v
}

// NewApp monta a App com suas dependências; usado pelos testes e pelo main.
// O ctx inicia como Background para os testes (startup troca pelo do Wails).
func NewApp(database *sql.DB, propostas *service.PropostaService) *App {
	return &App{
		ctx:       context.Background(),
		db:        database,
		tickets:   db.NewTicketRepo(database),
		cobrancas: db.NewCobrancaRepo(database),
		colunas:   db.NewColumnRepo(database),
		propostas: propostas,
	}
}

// colunasAtivas lista as colunas do board (dinâmicas), com fallback estático
// se a tabela não tiver nada.
func (a *App) colunasAtivas() []string {
	if cs, err := a.colunas.List(a.ctx); err == nil && len(cs) > 0 {
		return cs
	}
	return service.ColunasValidas
}

func (a *App) colunaValida(nome string) bool {
	for _, c := range a.colunasAtivas() {
		if c == nome {
			return true
		}
	}
	return false
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// friendly devolve erros de validação como estão; todo o resto vai para o
// slog e vira mensagem genérica, para nunca expor detalhes de banco ou rede.
func friendly(op string, err error) error {
	for _, e := range service.ErrosDeValidacao {
		if errors.Is(err, e) {
			return err
		}
	}
	slog.Error("falha na operação", "op", op, "err", err)
	return errors.New("Não foi possível concluir a operação. Tente novamente.")
}

const dataLayout = "2006-01-02"

func dataValida(s string) error {
	if s == "" {
		return nil
	}
	if _, err := time.Parse(dataLayout, s); err != nil {
		return service.ErrDataInvalida
	}
	return nil
}

func (a *App) validaTicket(t *db.Ticket) error {
	if !a.colunaValida(t.ColunaAtual) {
		return service.ErrColunaInvalida
	}
	if !service.TipoTicketValido(t.TipoTicket) {
		return service.ErrTipoTicketInvalido
	}
	switch {
	case len(t.NomeCliente) > service.MaxNomeCliente,
		len(t.Endereco) > service.MaxEndereco,
		len(t.Telefone) > service.MaxTelefone:
		return service.ErrTextoMuitoLongo
	}
	return nil
}

// SaveTicket cria (ID 0) ou atualiza um ticket. Card novo entra no fim da coluna.
func (a *App) SaveTicket(t db.Ticket) (db.Ticket, error) {
	if err := a.validaTicket(&t); err != nil {
		return db.Ticket{}, err
	}
	id, err := a.tickets.Save(a.ctx, &t)
	if err != nil {
		return db.Ticket{}, friendly("SaveTicket", err)
	}
	salvo, err := a.tickets.Get(a.ctx, id)
	if err != nil {
		return db.Ticket{}, friendly("SaveTicket", err)
	}
	return *salvo, nil
}

// GetTickets devolve todos os tickets ordenados por coluna e posição.
func (a *App) GetTickets() ([]db.Ticket, error) {
	tickets, err := a.tickets.List(a.ctx)
	if err != nil {
		return nil, friendly("GetTickets", err)
	}
	if tickets == nil {
		tickets = []db.Ticket{}
	}
	return tickets, nil
}

// MoveTicket move o card numa transação; posicao é o índice final na coluna de destino.
func (a *App) MoveTicket(id int64, coluna string, posicao int) error {
	if !a.colunaValida(coluna) {
		return service.ErrColunaInvalida
	}
	if posicao < 0 {
		return service.ErrPosicaoInvalida
	}
	t, err := a.tickets.Get(a.ctx, id)
	if err != nil {
		return friendly("MoveTicket", err)
	}
	if err := a.tickets.Move(a.ctx, id, t.ColunaAtual, coluna, posicao); err != nil {
		return friendly("MoveTicket", err)
	}
	return nil
}

// DeleteTicket apaga o card; a confirmação é do frontend.
func (a *App) DeleteTicket(id int64) error {
	if err := a.tickets.Delete(a.ctx, id); err != nil {
		return friendly("DeleteTicket", err)
	}
	return nil
}

// CalculateDebt é a função pura de reparcelamento exposta ao frontend.
func (a *App) CalculateDebt(in service.ReparcelamentoInput) (service.ReparcelamentoResult, error) {
	return service.CalculateReparcelamento(in)
}

// SaveCobranca recalcula tudo no backend, ignora os campos derivados vindos
// do frontend e grava como acordo ativo do ticket.
func (a *App) SaveCobranca(c db.Cobranca) (db.Cobranca, error) {
	if _, err := a.tickets.Get(a.ctx, c.TicketID); err != nil {
		return db.Cobranca{}, friendly("SaveCobranca", err)
	}
	if err := dataValida(c.DataContrato); err != nil {
		return db.Cobranca{}, err
	}
	if err := dataValida(c.DataVencimento); err != nil {
		return db.Cobranca{}, err
	}
	if c.Status != "" && !service.StatusCobrancaValido(c.Status) {
		return db.Cobranca{}, service.ErrStatusInvalido
	}
	if c.Status == "" {
		c.Status = service.StatusProposta
	}

	in := service.ReparcelamentoInput{
		TotalAbertoCents: c.TotalAbertoCents,
		EntradaCents:     c.EntradaCents,
		NumeroVezes:      c.NumeroVezes,
		ParcelaBaseCents: c.ParcelaBaseCents,
	}
	res, err := service.CalculateReparcelamento(in)
	if err != nil {
		return db.Cobranca{}, err
	}

	c.ParceladoCents = res.ParceladoCents
	c.UltimaParcelaCents = res.UltimaParcelaCents
	c.FinalCents = res.FinalCents
	c.Ativa = true
	c.VersaoRegra = service.VersaoRegra

	if _, err := a.cobrancas.SaveAtiva(a.ctx, &c); err != nil {
		return db.Cobranca{}, friendly("SaveCobranca", err)
	}
	salva, err := a.cobrancas.GetAtiva(a.ctx, c.TicketID)
	if err != nil || salva == nil {
		return db.Cobranca{}, friendly("SaveCobranca", err)
	}
	return *salva, nil
}

// GetCobrancaAtiva devolve o acordo ativo do ticket, ou nil se não houver.
func (a *App) GetCobrancaAtiva(ticketID int64) (*db.Cobranca, error) {
	c, err := a.cobrancas.GetAtiva(a.ctx, ticketID)
	if err != nil {
		return nil, friendly("GetCobrancaAtiva", err)
	}
	return c, nil
}

// GetHistoricoCobrancas devolve os acordos do ticket, mais recente primeiro.
func (a *App) GetHistoricoCobrancas(ticketID int64) ([]db.Cobranca, error) {
	list, err := a.cobrancas.History(a.ctx, ticketID)
	if err != nil {
		return nil, friendly("GetHistoricoCobrancas", err)
	}
	if list == nil {
		list = []db.Cobranca{}
	}
	return list, nil
}

// UpdateStatusCobranca altera o status do acordo.
func (a *App) UpdateStatusCobranca(id int64, status string) error {
	if !service.StatusCobrancaValido(status) {
		return service.ErrStatusInvalido
	}
	if err := a.cobrancas.UpdateStatus(a.ctx, id, status); err != nil {
		return friendly("UpdateStatusCobranca", err)
	}
	return nil
}

// GenerateProposal sempre gera pelo template; com a chave da Groq configurada
// e polirComIA=true, tenta o polimento e valida os valores antes de aceitar.
func (a *App) GenerateProposal(ticketID int64, in service.ReparcelamentoInput, polirComIA bool) (service.Proposta, error) {
	t, err := a.tickets.Get(a.ctx, ticketID)
	if err != nil {
		return service.Proposta{}, friendly("GenerateProposal", err)
	}
	res, err := service.CalculateReparcelamento(in)
	if err != nil {
		return service.Proposta{}, err
	}
	prop, err := a.propostas.Gerar(t.NomeCliente, in, res, false)
	if err != nil {
		return service.Proposta{}, friendly("GenerateProposal", err)
	}
	if !polirComIA || a.groq == nil || a.groq.Chave() == "" {
		return prop, nil
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	params, _ := a.parametros.TodosParametros(ctx)
	av, _ := a.avaliador.Avaliar(ctx, ticketID, t.CodService)
	resumo := ""
	if av != nil {
		resumo = av.ResumoUmaLinha()
	}
	polido, err := a.groq.Polir(ctx, prop.Texto, params, resumo)
	if err != nil {
		if errors.Is(err, service.ErrGroqSemChave) {
			return prop, nil
		}
		prop.Aviso = "IA indisponível (" + err.Error() + "); proposta gerada pelo modelo padrão."
		return prop, nil
	}
	return service.Proposta{Texto: polido, Origem: "ia"}, nil
}

// HasAI diz ao frontend se o botão "Polir com IA" deve aparecer.
func (a *App) HasAI() bool {
	return a.groq != nil && a.groq.Chave() != ""
}

// ExportBackup abre o diálogo de salvar e grava uma cópia consistente
// do banco com VACUUM INTO.
func (a *App) ExportBackup() (string, error) {
	caminho, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Exportar backup",
		DefaultFilename: "rotasmart-" + time.Now().Format("2006-01-02") + ".db",
	})
	if err != nil {
		return "", friendly("ExportBackup", err)
	}
	if caminho == "" {
		return "", nil // usuário cancelou
	}
	if err := backup.Export(a.db, caminho); err != nil {
		return "", friendly("ExportBackup", err)
	}
	return caminho, nil
}

// GetColunas devolve as colunas do Kanban na ordem de exibição.
func (a *App) GetColunas() []string {
	return a.colunasAtivas()
}
