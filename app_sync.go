package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"rota-smart/internal/backup"
	"rota-smart/internal/db"
	"rota-smart/internal/syncgh"
)

// Chaves da tabela config para a sincronização.
const (
	cfgSyncToken = "sync_github_token"
	cfgSyncRepo  = "sync_github_repo"
	cfgSyncSHA   = "sync_ultimo_sha"
	cfgSyncHora  = "sync_ultimo_envio"
)

const (
	syncArquivoRemoto = "rotasmart.db"        // nome do arquivo dentro do repo
	syncPendente      = "sync-pendente.db"    // versão baixada, aplicada no boot
	syncSubstituido   = "sync-substituido.db" // versão local descartada ao aplicar
	syncMargemSujo    = 2 * time.Second       // folga entre mtime e o marco do sync
)

// SyncInfo é o estado da sincronização exposto ao frontend.
type SyncInfo struct {
	Configurado    bool   `json:"configurado"`
	Repo           string `json:"repo"`
	TemRemoto      bool   `json:"temRemoto"`
	RemotoQuando   string `json:"remotoQuando"`
	PendenteBaixar bool   `json:"pendenteBaixar"`
	LocalAlterado  bool   `json:"localAlterado"`
}

// ResultadoSync descreve o que o botão Sincronizar decidiu fazer.
type ResultadoSync struct {
	Acao     string `json:"acao"` // "enviado", "recebido", "em-dia", "conflito"
	Mensagem string `json:"mensagem"`
}

// syncConfigurado exige wiring de config e caminho do banco (nada em testes antigos).
func (a *App) syncConfigurado() error {
	if a.config == nil {
		return fmt.Errorf("configuração indisponível")
	}
	if a.dbPath == "" {
		return fmt.Errorf("sincronização indisponível nesta instalação")
	}
	return nil
}

func (a *App) syncCliente() *syncgh.Cliente {
	token := a.configGet(a.ctx, cfgSyncToken)
	repo := a.configGet(a.ctx, cfgSyncRepo)
	if repo == "" {
		repo = "luisboneta/banco-de-dados"
	}
	return syncgh.Novo(token, repo, syncArquivoRemoto)
}

// SetSyncConfig grava o token PAT e o repositório (dono/repo) da sincronização.
func (a *App) SetSyncConfig(token, repo string) error {
	if err := a.syncConfigurado(); err != nil {
		return err
	}
	if repo == "" {
		repo = "luisboneta/banco-de-dados"
	}
	if err := a.config.Set(a.ctx, cfgSyncToken, token); err != nil {
		return friendly("SetSyncConfig", err)
	}
	if err := a.config.Set(a.ctx, cfgSyncRepo, repo); err != nil {
		return friendly("SetSyncConfig", err)
	}
	return nil
}

// syncLocalSujo diz se o banco mudou desde o último sync (mtime maior que o
// marco gravado, com folga de 2s para as escritas do próprio marco).
func (a *App) syncLocalSujo() bool {
	marca, err := time.Parse(time.RFC3339, a.configGet(a.ctx, cfgSyncHora))
	if err != nil {
		return true // nunca sincronizou
	}
	for _, sufixo := range []string{"", "-wal", "-shm"} {
		if fi, err := os.Stat(a.dbPath + sufixo); err == nil && fi.ModTime().After(marca.Add(-syncMargemSujo)) {
			return true
		}
	}
	return false
}

// SyncStatus devolve o estado atual da sincronização.
func (a *App) SyncStatus() (SyncInfo, error) {
	var info SyncInfo
	if err := a.syncConfigurado(); err != nil {
		return info, err
	}
	info.Configurado = a.configGet(a.ctx, cfgSyncToken) != ""
	info.Repo = a.configGet(a.ctx, cfgSyncRepo)
	if info.Repo == "" {
		info.Repo = "luisboneta/banco-de-dados"
	}
	_, err := os.Stat(filepath.Join(filepath.Dir(a.dbPath), syncPendente))
	info.PendenteBaixar = err == nil
	info.LocalAlterado = a.syncLocalSujo()
	if info.Configurado {
		ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
		defer cancel()
		if remoto, err := a.syncCliente().Info(ctx); err == nil && remoto.Existe {
			info.TemRemoto = true
			info.RemotoQuando = remoto.Quando.Local().Format("02/01/2006 15:04")
		}
	}
	return info, nil
}

// SyncEnviar sobe um snapshot consistente do banco para o repositório.
func (a *App) SyncEnviar() (ResultadoSync, error) {
	if err := a.syncConfigurado(); err != nil {
		return ResultadoSync{}, err
	}
	tmp := a.dbPath + ".sync-tmp"
	defer os.Remove(tmp)
	if err := backup.Export(a.db, tmp); err != nil {
		return ResultadoSync{}, friendly("SyncEnviar", err)
	}

	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Minute)
	defer cancel()
	cli := a.syncCliente()
	mensagem := fmt.Sprintf("sync %s", time.Now().Format(time.RFC3339))
	if err := cli.Enviar(ctx, tmp, mensagem); err != nil {
		return ResultadoSync{}, friendly("SyncEnviar", err)
	}
	info, err := cli.Info(ctx)
	if err != nil || !info.Existe {
		return ResultadoSync{}, friendly("SyncEnviar", err)
	}
	a.syncMarcar(info.SHA)
	return ResultadoSync{Acao: "enviado", Mensagem: "Dados enviados para " + a.repoAtual() + "."}, nil
}

// SyncReceber baixa o banco remoto, valida e deixa preparado; o arquivo só
// substitui o banco local quando o app reiniciar (o SQLite está aberto).
func (a *App) SyncReceber() (ResultadoSync, error) {
	if err := a.syncConfigurado(); err != nil {
		return ResultadoSync{}, err
	}
	dir := filepath.Dir(a.dbPath)
	tmp := filepath.Join(dir, syncPendente+".tmp")
	defer os.Remove(tmp)

	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Minute)
	defer cancel()
	cli := a.syncCliente()
	infoRemota, err := cli.Info(ctx)
	if err != nil {
		return ResultadoSync{}, friendly("SyncReceber", err)
	}
	if !infoRemota.Existe {
		return ResultadoSync{}, fmt.Errorf("o repositório ainda não tem dados enviados")
	}
	if err := cli.Baixar(ctx, tmp); err != nil {
		return ResultadoSync{}, friendly("SyncReceber", err)
	}
	validado, err := db.Init(tmp)
	if err != nil {
		return ResultadoSync{}, friendly("SyncReceber", err)
	}
	validado.Close()
	pendente := filepath.Join(dir, syncPendente)
	if err := os.Rename(tmp, pendente); err != nil {
		return ResultadoSync{}, friendly("SyncReceber", err)
	}
	if info, err := cli.Info(ctx); err == nil {
		a.syncMarcar(info.SHA)
	}
	return ResultadoSync{Acao: "recebido", Mensagem: "Dados baixados. Reinicie o app para aplicá-los."}, nil
}

// Sincronizar decide sozinho: se só o local mudou, envia; se só o remoto,
// baixa; se os dois, devolve conflito para o usuário escolher.
func (a *App) Sincronizar() (ResultadoSync, error) {
	if err := a.syncConfigurado(); err != nil {
		return ResultadoSync{}, err
	}
	if a.configGet(a.ctx, cfgSyncToken) == "" {
		return ResultadoSync{Acao: "configurar", Mensagem: "Configure o token do GitHub em Configurações → Sincronização."}, nil
	}

	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	remoto, err := a.syncCliente().Info(ctx)
	if err != nil {
		return ResultadoSync{}, friendly("Sincronizar", err)
	}
	sujo := a.syncLocalSujo()
	conhecido := a.configGet(a.ctx, cfgSyncSHA)

	switch {
	case !remoto.Existe && conhecido == "":
		return a.SyncEnviar() // primeira sincronização: o local vence
	case conhecido == "":
		return a.SyncReceber() // primeira sincronização deste PC: remoto vence
	case sujo && remoto.SHA != conhecido:
		return ResultadoSync{Acao: "conflito", Mensagem: "Há mudanças locais E remotas. Escolha enviar (sobrescreve o repo) ou baixar (sobrescreve este PC)."}, nil
	case sujo:
		return a.SyncEnviar()
	case remoto.SHA != conhecido:
		return a.SyncReceber()
	default:
		return ResultadoSync{Acao: "em-dia", Mensagem: "Tudo em dia (última versão remota: " + remoto.Quando.Local().Format("02/01/2006 15:04") + ")."}, nil
	}
}

// syncMarcar grava o SHA remoto conhecido e o marco de tempo do sync.
func (a *App) syncMarcar(sha string) {
	if a.config == nil {
		return
	}
	_ = a.config.Set(a.ctx, cfgSyncSHA, sha)
	_ = a.config.Set(a.ctx, cfgSyncHora, time.Now().Format(time.RFC3339))
}

// repoAtual devolve o repositório configurado (para mensagens).
func (a *App) repoAtual() string {
	if r := a.configGet(a.ctx, cfgSyncRepo); r != "" {
		return r
	}
	return "luisboneta/banco-de-dados"
}

// aplicarSyncPendente roda antes do banco abrir: se há versão baixada
// aguardando, preserva a local em sync-substituido.db e promove a remota.
func aplicarSyncPendente(path string) {
	dir := filepath.Dir(path)
	pendente := filepath.Join(dir, syncPendente)
	if _, err := os.Stat(pendente); err != nil {
		return
	}
	antigo := filepath.Join(dir, syncSubstituido)
	_ = os.Remove(antigo)
	if err := os.Rename(path, antigo); err == nil {
		_ = os.Remove(path + "-wal")
		_ = os.Remove(path + "-shm")
	}
	if err := os.Rename(pendente, path); err != nil {
		// não foi possível promover: devolve o antigo para não perder dados
		_ = os.Rename(antigo, path)
		slog.Error("sync: falha ao aplicar banco baixado", "err", err)
		return
	}
	slog.Info("sync: banco remoto aplicado")
}
