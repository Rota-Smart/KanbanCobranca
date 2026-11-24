package main

// Reenvio da lista de cancelamentos ao backend do site (Render) em segundo
// plano. Antes o app descartava a resposta do site por completo — qualquer
// recusa (lista sem formato reconhecido, coluna de tratamento em branco...)
// ficava invisível e o dashboard do site ficava defasado sem explicação.
// Agora o resultado vira o evento "cancelamentos:sync-site", que o dashboard
// de cancelamentos do app mostra como aviso.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"rota-smart/internal/contasync"
)

const evSyncCancelSite = "cancelamentos:sync-site"

type resumoSyncSite struct {
	Registrados int      `json:"registrados"`
	Movidos     int      `json:"movidos"`
	Criados     int      `json:"criados"`
	JaNaColuna  int      `json:"ja_na_coluna"`
	Falhas      []string `json:"falhas"`
}

// sincronizarImportSite reenvia a lista colada ao site e avisa o usuário com
// o desfecho (aceita a conexão ausente como aviso — sem ela o site nunca
// fica sabendo do import).
func (a *App) sincronizarImportSite(texto, colunaTratamento string) {
	emitir := func(ok bool, mensagem string) {
		if a.ctx == nil || !a.uiPronta {
			return
		}
		runtime.EventsEmit(a.ctx, evSyncCancelSite, map[string]any{"ok": ok, "mensagem": mensagem})
	}
	tok, _ := lerTokenConta()
	if tok == "" {
		emitir(false, "O app não está conectado à conta — a lista não foi enviada para o site.")
		return
	}
	corpo, err := json.Marshal(map[string]string{
		"texto":             texto,
		"coluna_tratamento": colunaTratamento,
	})
	if err != nil {
		return
	}
	ctxHttp, cancelHttp := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelHttp()
	req, err := http.NewRequestWithContext(ctxHttp, http.MethodPost, contasync.URLPadrao+"/cancelamentos/importar", bytes.NewReader(corpo))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("sincronizar cancelamentos com site", "err", err)
		emitir(false, "Site não respondeu ao reenvio da lista: "+err.Error())
		return
	}
	defer resp.Body.Close()
	corpoResp, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		var erroBody struct {
			Erro string `json:"erro"`
		}
		msg := strings.TrimSpace(string(corpoResp))
		if json.Unmarshal(corpoResp, &erroBody) == nil && erroBody.Erro != "" {
			msg = erroBody.Erro
		}
		slog.Warn("site recusou cancelamentos", "status", resp.StatusCode, "resp", msg)
		emitir(false, "O site não recebeu a lista: "+msg)
		return
	}
	var res resumoSyncSite
	if err := json.Unmarshal(corpoResp, &res); err != nil {
		emitir(true, "Lista enviada ao site.")
		return
	}
	msg := syncSiteResumo(res)
	slog.Info("cancelamentos sincronizados com site", "resumo", msg)
	emitir(res.Registrados > 0 || len(res.Falhas) == 0, "Site: "+msg)
}

func syncSiteResumo(res resumoSyncSite) string {
	parts := []string{}
	if res.Registrados > 0 {
		parts = append(parts, formatoPlural(res.Registrados, "cancelamento registrado"))
	}
	if res.Criados > 0 {
		parts = append(parts, formatoPlural(res.Criados, "card criado"))
	}
	if res.Movidos > 0 {
		parts = append(parts, formatoPlural(res.Movidos, "card movido"))
	}
	if res.JaNaColuna > 0 {
		parts = append(parts, formatoPlural(res.JaNaColuna, "já estava na coluna"))
	}
	if len(parts) == 0 {
		parts = append(parts, "nada novo a gravar")
	}
	msg := strings.Join(parts, ", ")
	if len(res.Falhas) > 0 {
		msg += fmt.Sprintf(", %d falha(s): %s", len(res.Falhas), res.Falhas[0])
	}
	return msg
}

func formatoPlural(n int, singular string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %ss", n, singular)
}
