package rota360

// Suporte à captura de voz na aba do Chrome controlada pelo app.
//
// Em máquinas onde o microfone do WebView2 falha (NotReadableError) mas o
// Chrome normal funciona, o app injeta via CDP um gravador na aba que já
// está aberta no 360 (o mesmo perfil-cdp da conexão assistida) e recebe os
// lotes de WAV de volta por Runtime.evaluate — sem abrir aba ou janela nova.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// AbreChromeVoz relança o Chrome do perfil dedicado do app (já logado no
// 360 — o cookie vive no perfil) com porta de depuração, e o DEIXA rodando:
// a voz precisa de uma aba viva para injetar o gravador, e o fluxo de
// conexão normal mata o Chrome assim que lê o cookie. Reuso nas gravações
// seguintes e nas navegações de ticket (aba do 360 fica aberta).
func AbreChromeVoz() (int, error) {
	chrome, err := chromeExecutavel()
	if err != nil {
		return 0, err
	}
	perfil, err := pastaPerfilCDP()
	if err != nil {
		return 0, err
	}
	_ = os.MkdirAll(perfil, 0o700)
	_ = os.WriteFile(filepath.Join(perfil, "First Run"), nil, 0o600)
	porta, err := portaLivre()
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(chrome,
		"--user-data-dir="+perfil,
		fmt.Sprintf("--remote-debugging-port=%d", porta),
		"--no-first-run", "--no-default-browser-check",
		"--window-size=1100,800",
		urlLogin360,
	)
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("abrir o Chrome para a voz: %w", err)
	}
	if !aguardarPorta(porta, cmd, cdpTimeoutBoot) {
		mataArvore(cmd)
		return 0, fmt.Errorf("o Chrome abriu mas a porta de depuração não respondeu")
	}
	slog.Info("voz chrome: perfil relançado para captura", "porta", porta)
	return porta, nil
}

// PortaCDPViva devolve a porta de debug do Chrome do perfil dedicado do app,
// se estiver rodando. Não toca no Chrome pessoal do usuário.
func PortaCDPViva() (int, bool) {
	perfil, err := pastaPerfilCDP()
	if err != nil {
		return 0, false
	}
	if porta, ok := portaCDPAtiva(perfil); ok {
		return porta, true
	}
	return 0, false
}

// AbaVoz devolve o webSocketDebuggerURL da aba onde injetar o gravador:
// prefere uma aba aberta no 360 (a que o usuário está usando); sem ela,
// cai para a primeira aba comum do Chrome do perfil do app.
func AbaVoz(porta int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/list", porta), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("listar abas do Chrome: %w", err)
	}
	defer resp.Body.Close()
	var alvos []alvoCDP
	if err := json.NewDecoder(resp.Body).Decode(&alvos); err != nil {
		return "", fmt.Errorf("resposta /json/list inválida: %w", err)
	}
	var reserva string
	for _, alvo := range alvos {
		if alvo.Type != "page" || alvo.WebSocketDebuggerURL == "" {
			continue
		}
		if strings.HasPrefix(alvo.URL, "chrome-extension://") || strings.HasPrefix(alvo.URL, "devtools://") {
			continue
		}
		if strings.Contains(alvo.URL, CookieDomain) {
			return alvo.WebSocketDebuggerURL, nil
		}
		if reserva == "" {
			reserva = alvo.WebSocketDebuggerURL
		}
	}
	if reserva != "" {
		return reserva, nil
	}
	return "", fmt.Errorf("nenhuma aba do Chrome do 360 está aberta — conecte ao Rota 360 antes de usar a voz")
}

// LiberaMicrofone concede a permissão de captura de áudio via CDP
// (Browser.grantPermissions), evitando o prompt na aba. Melhor esforço:
// se falhar, o Chrome mostra o prompt normal e o usuário concede na mão.
func LiberaMicrofone(porta int) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/version", porta), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	var ver struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ver); err != nil {
		return
	}
	resp.Body.Close()
	if ver.WebSocketDebuggerURL == "" {
		return
	}
	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	conn, _, err := dialer.DialContext(ctx, ver.WebSocketDebuggerURL, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.WriteJSON(map[string]any{
		"id":     1,
		"method": "Browser.grantPermissions",
		"params": map[string]any{"permissions": []string{"audioCapture"}},
	})
}
