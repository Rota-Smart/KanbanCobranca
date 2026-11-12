// Sessão do 360 compartilhada com a conta do site: o app pareado busca o
// cookie que o login no site guardou (sem precisar do Chrome local) e, quando
// detecta um cookie novo no Chrome, oferece à conta. O token do pareamento
// é a autorização — o cookie só circula entre o app e a API.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"rota-smart/internal/contasync"
	"rota-smart/internal/rota360"
)

// Intervalo mínimo entre buscas na conta, para não martelar a API quando
// várias consultas falham em sequência.
const (
	intervaloBuscaSessao   = time.Minute
	intervaloBuscaSessaoBg = 5 * time.Minute
)

var ultimaBuscaSessao atomic.Int64 // unix seconds da última tentativa

func podeBuscarSessao(minimo time.Duration) bool {
	agora := time.Now().Unix()
	prev := ultimaBuscaSessao.Load()
	if agora-prev < int64(minimo.Seconds()) {
		return false
	}
	return ultimaBuscaSessao.CompareAndSwap(prev, agora)
}

var httpSessaoConta = &http.Client{Timeout: 15 * time.Second}

// buscarSessaoConta devolve o cookie do 360 guardado na conta do site.
func buscarSessaoConta(ctx context.Context) (string, error) {
	token, _ := lerTokenConta()
	if token == "" {
		return "", errors.New("conta não pareada")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, contasync.URLPadrao+"/360/sessao", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpSessaoConta.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("a conta não tem sessão do 360 (HTTP %d)", resp.StatusCode)
	}
	var corpo struct {
		Cookie string `json:"cookie"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&corpo); err != nil {
		return "", err
	}
	if corpo.Cookie == "" {
		return "", errors.New("resposta sem cookie")
	}
	return corpo.Cookie, nil
}

// enviarSessaoConta oferece um cookie detectado no Chrome local à conta do
// site. Melhor esforço: falha não atrapalha quem conectou.
func enviarSessaoConta(ctx context.Context, cookie string) {
	token, _ := lerTokenConta()
	if token == "" || cookie == "" {
		return
	}
	corpo, err := json.Marshal(map[string]string{"cookie": cookie})
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, contasync.URLPadrao+"/360/sessao", bytes.NewReader(corpo))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpSessaoConta.Do(req)
	if err != nil {
		slog.Warn("sessao 360 para a conta", "err", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		slog.Info("sessão do 360 compartilhada com a conta do site")
	} else {
		slog.Warn("sessao 360 para a conta", "status", resp.StatusCode)
	}
}

// recusasSeguidas conta recusas do 360 em sequência: 1 = cookie local morto
// (hora de buscar o da conta); 2 = o da conta também está morto — para de
// tentar sozinho e pede reconexão ao usuário.
var recusasSeguidas atomic.Int32

// clienteRota cria o cliente do 360 com o gancho de sessão recusada — toda
// sessão usada pelo app passa por aqui.
func (a *App) clienteRota(cookie *rota360.Cookie) *rota360.Cliente {
	c := rota360.NovoCliente(cookie)
	c.AoRecusar = a.AoRecusarSessao
	return c
}

// AoRecusarSessao roda quando o 360 devolve 401/403: descarta o cookie salvo
// morto e adota obrigatoriamente a sessão da conta do site — o app volta a
// funcionar sem o usuário notar. Se a sessão da conta também for recusada em
// seguida, avisa a UI em vez de martelar a API.
func (a *App) AoRecusarSessao() {
	n := recusasSeguidas.Add(1)
	slog.Warn("360 recusou a sessão — descartando o cookie salvo", "recusas", n)
	a.muRota.Lock()
	a.rota = nil
	a.muRota.Unlock()
	if a.config != nil {
		_ = a.config.Set(a.ctx, cfgRotaCookie, "")
	}
	if n > 1 || !podeBuscarSessao(intervaloBuscaSessao) {
		if n > 1 && a.uiPronta {
			runtime.EventsEmit(a.ctx, evRotaConectado, Rota360Status{Conectado: false,
				Mensagem: "A sessão do 360 venceu (a do site também). Use Conectar e faça o login no 360."})
		}
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	cookie, err := buscarSessaoConta(ctx)
	cancel()
	if err != nil {
		slog.Warn("sessão da conta indisponível após recusa", "err", err)
		return
	}
	_ = a.adotarSessaoRemota(cookie)
}

// adotarSessaoRemota instala um cookie vindo da conta como a sessão local.
func (a *App) adotarSessaoRemota(cookieTxt string) error {
	c, err := rota360.ParseCookieColado(cookieTxt)
	if err != nil {
		return err
	}
	a.definirRota(a.clienteRota(c), "conta", cookieTxt)
	slog.Info("sessão do 360 vinda da conta do site")
	if a.uiPronta {
		runtime.EventsEmit(a.ctx, evRotaConectado,
			Rota360Status{Conectado: true, Origem: "conta do site"})
	}
	return nil
}

// manterSessao360 roda em background: com o app pareado e sem sessão local,
// adota a sessão da conta sozinho (o app abre já conectado).
func (a *App) manterSessao360() {
	for a.ctx.Err() == nil {
		if !dormir(a.ctx, 30*time.Second) {
			return
		}
		a.muRota.Lock()
		tem := a.rota != nil
		a.muRota.Unlock()
		if tem || recusasSeguidas.Load() > 1 || !podeBuscarSessao(intervaloBuscaSessaoBg) {
			continue
		}
		ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
		cookie, err := buscarSessaoConta(ctx)
		cancel()
		if err == nil {
			_ = a.adotarSessaoRemota(cookie)
		}
	}
}
