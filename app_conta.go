package main

import (
	"bufio"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/zalando/go-keyring"

	"rota-smart/internal/atualizacao"
	"rota-smart/internal/contasync"
	"rota-smart/internal/db"
)

// O token de pareamento fica no Credential Manager do Windows.
// Não entra no SQLite nem em arquivo de configuração.
const (
	cofreServico = "RotaSuite"
	cofreConta   = "token-pareamento"
)

// ContaStatus é o que a tela mostra sobre a conexão. Sem detalhes de rede.
type ContaStatus struct {
	Conectado bool   `json:"conectado"`
	Nome      string `json:"nome"`
	Email     string `json:"email"`
	Estado    string `json:"estado"`
	Pendentes int    `json:"pendentes"`
	Ultima    string `json:"ultima"`
}

func (a *App) iniciarContaSync() {
	a.acordarSync = make(chan struct{}, 1)
	go a.loopContaSync()
	go a.ouvirNuvem()
	go a.manterSessao360()
}

// ouvirNuvem mantém aberto o fluxo /eventos da API: a cada aviso de que algo
// mudou na nuvem (site ou outro PC), acorda o ciclo na hora em vez de esperar.
func (a *App) ouvirNuvem() {
	espera := 2 * time.Second
	for a.ctx.Err() == nil {
		token, _ := lerTokenConta()
		if token == "" {
			if !dormir(a.ctx, 30*time.Second) {
				return
			}
			continue
		}
		req, err := http.NewRequestWithContext(a.ctx, http.MethodGet, contasync.URLPadrao+"/eventos", nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Accept", "text/event-stream")
			var resp *http.Response
			resp, err = (&http.Client{}).Do(req)
			if err == nil {
				if resp.StatusCode == http.StatusOK {
					espera = 2 * time.Second
					ler := bufio.NewScanner(resp.Body)
					for ler.Scan() {
						if strings.HasPrefix(ler.Text(), "data:") {
							a.acordar()
						}
					}
				}
				resp.Body.Close()
			}
		}
		if !dormir(a.ctx, espera) {
			return
		}
		if espera < time.Minute {
			espera *= 2
		}
	}
}

func dormir(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (a *App) loopContaSync() {
	a.cicloConta()
	espera := 20 * time.Second
	timer := time.NewTimer(espera)
	defer timer.Stop()
	// O que muda neste PC entra na fila por gatilho do SQLite; olhar a fila a
	// cada 1 s faz a alteração subir quase na hora, sem esperar o ciclo cheio.
	olhar := time.NewTicker(1 * time.Second)
	defer olhar.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-olhar.C:
			// Só o que já pode sair: item adiado por erro espera a vez dele.
			var n int
			agora := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
			if err := a.db.QueryRowContext(a.ctx, `SELECT count(*) FROM sync_fila WHERE proxima_em <= ?`, agora).Scan(&n); err == nil && n > 0 && a.esperaConta() != 15*time.Second {
				a.acordar()
			}
		case <-a.acordarSync:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			a.cicloConta()
			timer.Reset(a.esperaConta())
		case <-timer.C:
			a.cicloConta()
			timer.Reset(a.esperaConta())
		}
	}
}

func (a *App) esperaConta() time.Duration {
	a.muConta.Lock()
	defer a.muConta.Unlock()
	if a.estadoConta.Estado == contasync.EstadoAguardando {
		return 15 * time.Second
	}
	return 20 * time.Second
}

func (a *App) cicloConta() {
	if a.db == nil || !a.muSync.TryLock() {
		return
	}
	defer a.muSync.Unlock()
	token, _ := lerTokenConta()
	if token == "" {
		a.publicarConta(ContaStatus{})
		return
	}
	a.publicarConta(a.montarStatus(contasync.EstadoSincronizando))
	rel, err := a.motorConta().Ciclo(a.ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		slog.Warn("sync conta", "err", err)
		a.publicarConta(a.montarStatus(contasync.EstadoAguardando))
		return
	}
	if rel.Estado == contasync.EstadoOk {
		_ = contasync.MetaSet(context.Background(), a.db, "ultima", time.Now().Format("15:04"))
	}
	st := a.montarStatus(rel.Estado)
	st.Pendentes = rel.Pendentes
	a.publicarConta(st)
	if rel.Aplicou > 0 && a.uiPronta {
		runtime.EventsEmit(a.ctx, "conta:quadro", nil)
	}
	// Fila de pedidos do site: o app atende depois do sync (o mesmo aviso do
	// /eventos que acorda o ciclo traz também os pedidos novos na hora).
	a.atenderPedidos(token)
}

func (a *App) motorConta() *contasync.Motor {
	return &contasync.Motor{
		DB: a.db,
		API: &contasync.HTTP{
			Base:   contasync.URLPadrao,
			Client: nil,
			Diag:   "v" + atualizacao.VersaoApp + " " + contasync.Diagnostico(context.Background(), a.db),
		},
		Token:   lerTokenConta,
		Agora:   time.Now,
		Reparar: db.RepararGatilhosSync,
	}
}

func (a *App) montarStatus(estado string) ContaStatus {
	ctx := context.Background()
	nome, _ := contasync.MetaGet(ctx, a.db, "nome")
	email, _ := contasync.MetaGet(ctx, a.db, "email")
	ultima, _ := contasync.MetaGet(ctx, a.db, "ultima")
	n, _ := contasync.ContarPendentes(ctx, a.db)
	tok, _ := lerTokenConta()
	if tok == "" {
		estado = ""
	}
	return ContaStatus{
		Conectado: tok != "",
		Nome:      nome,
		Email:     email,
		Estado:    estado,
		Pendentes: n,
		Ultima:    ultima,
	}
}

func (a *App) publicarConta(st ContaStatus) {
	a.muConta.Lock()
	a.estadoConta = st
	a.muConta.Unlock()
	if a.uiPronta {
		runtime.EventsEmit(a.ctx, "conta:sync", st)
	}
}

// ContaStatus devolve o indicador atual, sem ir na rede.
func (a *App) ContaStatus() ContaStatus {
	if a.db == nil {
		return ContaStatus{}
	}
	a.muConta.Lock()
	estado := a.estadoConta.Estado
	a.muConta.Unlock()
	return a.montarStatus(estado)
}

// ConectarConta troca o código de uso único do site pelo token longo.
func (a *App) ConectarConta(codigo string) (ContaStatus, error) {
	if a.db == nil {
		return ContaStatus{}, errors.New("conexão indisponível nesta instalação")
	}
	conta, err := contasync.Parear(a.ctx, contasync.URLPadrao, codigo)
	if err != nil {
		return ContaStatus{}, err
	}
	if err := keyring.Set(cofreServico, cofreConta, conta.Token); err != nil {
		return ContaStatus{}, errors.New("não consegui guardar a conexão neste computador")
	}
	ctx := context.Background()
	_ = contasync.MetaSet(ctx, a.db, "nome", conta.Nome)
	_ = contasync.MetaSet(ctx, a.db, "email", conta.Email)
	a.acordar()
	return a.montarStatus(contasync.EstadoSincronizando), nil
}

// DesconectarConta apaga o token deste computador. O quadro local fica.
func (a *App) DesconectarConta() (ContaStatus, error) {
	if err := keyring.Delete(cofreServico, cofreConta); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return ContaStatus{}, errors.New("não consegui desconectar neste computador")
	}
	if a.db != nil {
		ctx := context.Background()
		_ = contasync.MetaSet(ctx, a.db, "nome", "")
		_ = contasync.MetaSet(ctx, a.db, "email", "")
	}
	st := ContaStatus{}
	a.publicarConta(st)
	return st, nil
}

func (a *App) acordar() {
	if a.acordarSync == nil {
		return
	}
	select {
	case a.acordarSync <- struct{}{}:
	default:
	}
}

func lerTokenConta() (string, error) {
	tok, err := keyring.Get(cofreServico, cofreConta)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	return tok, err
}
