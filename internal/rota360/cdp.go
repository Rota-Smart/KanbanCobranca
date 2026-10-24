package rota360

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/sys/windows/registry"
)

// Detecção via Chrome DevTools Protocol (CDP).
//
// O Chrome recente trava o banco de cookies de forma exclusiva e cifra os
// cookies com App-Bound Encryption amarrada ao caminho do perfil — ler o
// perfil do usuário (por cópia ou por arquivo) não funciona mais. O caminho
// que funciona é deixar o próprio Chrome entregar os cookies: mantemos um
// perfil dedicado do app (pastaPerfilCDP) aberto com --remote-debugging-port.
// Na primeira conexão o usuário faz login nessa janela; nas seguintes o
// perfil já está logado e a detecção é imediata. O Chrome do usuário nunca
// é fechado.

const (
	urlLogin360      = "https://360.rotagroup.com.br/"
	cdpTimeoutBoot   = 20 * time.Second // espera o Chrome abrir a porta de debug
	cdpTimeoutLogin  = 5 * time.Minute  // espera o usuário fazer login na janela
	cdpPortaInspecao = 9222
)

// DetectaCookieCDP devolve o cookie de sessão do 360 lido via CDP:
// primeiro numa porta de debug já aberta, depois no perfil dedicado do app.
func DetectaCookieCDP() (*Cookie, error) {
	if userdata, err := pastaUserData(); err == nil {
		if porta, ok := portaCDPAtiva(userdata); ok {
			if c, err := cookiesViaCDP(porta); err == nil {
				c.Usuario = usuarioViaCDP(porta)
				return c, nil
			}
		}
	}
	return cookiesPerfilAssistido()
}

// cookiesPerfilAssistido abre (ou reusa) o perfil dedicado do app na página
// do 360 e espera o cookie de sessão aparecer.
func cookiesPerfilAssistido() (*Cookie, error) {
	chrome, err := chromeExecutavel()
	if err != nil {
		return nil, err
	}
	perfil, err := pastaPerfilCDP()
	if err != nil {
		return nil, err
	}
	_ = os.MkdirAll(perfil, 0o700)
	_ = os.WriteFile(filepath.Join(perfil, "First Run"), nil, 0o600)

	porta, err := portaLivre()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(chrome,
		"--user-data-dir="+perfil,
		fmt.Sprintf("--remote-debugging-port=%d", porta),
		"--no-first-run", "--no-default-browser-check",
		"--window-size=1100,800",
		urlLogin360,
	)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("abrir janela de conexão do Chrome: %w", err)
	}
	defer func() {
		mataArvore(cmd)
	}()

	if !aguardarPorta(porta, cmd, cdpTimeoutBoot) {
		return nil, fmt.Errorf("janela de conexão não abriu a porta de depuração")
	}

	deadline := time.Now().Add(cdpTimeoutLogin)
	for time.Now().Before(deadline) {
		if cookie, err := cookiesViaCDP(porta); err == nil {
			// A janela está aberta no portal já logado: aproveita para ler o
			// nome do usuário no cabeçalho (falha silenciosa se não achar).
			cookie.Usuario = usuarioViaCDP(porta)
			return cookie, nil
		}
		if cmd.ProcessState != nil {
			return nil, fmt.Errorf("janela de conexão foi fechada antes do login")
		}
		time.Sleep(1500 * time.Millisecond)
	}
	return nil, fmt.Errorf("login não detectado em %s — feche a janela e tente de novo", cdpTimeoutLogin)
}

// aguardarPorta espera a porta de debug do Chrome responder.
func aguardarPorta(porta int, cmd *exec.Cmd, limite time.Duration) bool {
	deadline := time.Now().Add(limite)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/version", porta), nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err == nil {
			resp.Body.Close()
			return true
		}
		if cmd.ProcessState != nil {
			return false
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}

// portaCDPAtiva procura uma porta de debug já aberta: arquivo DevToolsActivePort
// do perfil ou a porta de inspeção padrão.
func portaCDPAtiva(userdata string) (int, bool) {
	candidatas := []int{}
	if raw, err := os.ReadFile(filepath.Join(userdata, "DevToolsActivePort")); err == nil {
		linhas := strings.Split(string(raw), "\n")
		if n := parsePorta(strings.TrimSpace(linhas[0])); n > 0 {
			candidatas = append(candidatas, n)
		}
	}
	candidatas = append(candidatas, cdpPortaInspecao)
	for _, p := range candidatas {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/version", p), nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err == nil {
			resp.Body.Close()
			return p, true
		}
	}
	return 0, false
}

func parsePorta(s string) int {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
		if n > 65535 {
			return 0
		}
	}
	return n
}

// cookiesViaCDP conecta ao endpoint do browser e pede todos os cookies.
func cookiesViaCDP(porta int) (*Cookie, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/version", porta), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("porta de debug %d inacessível: %w", porta, err)
	}
	var ver struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ver); err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("resposta /json/version inválida: %w", err)
	}
	resp.Body.Close()
	if ver.WebSocketDebuggerURL == "" {
		return nil, fmt.Errorf("/json/version sem webSocketDebuggerUrl")
	}

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.DialContext(ctx, ver.WebSocketDebuggerURL, nil)
	if err != nil {
		return nil, fmt.Errorf("conectar ao CDP: %w", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{"id": 1, "method": "Storage.getCookies", "params": map[string]any{}}); err != nil {
		return nil, fmt.Errorf("pedir cookies ao CDP: %w", err)
	}
	for {
		var msg struct {
			ID     int `json:"id"`
			Result struct {
				Cookies []struct {
					Name   string `json:"name"`
					Value  string `json:"value"`
					Domain string `json:"domain"`
				} `json:"cookies"`
			} `json:"result"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := conn.ReadJSON(&msg); err != nil {
			return nil, fmt.Errorf("ler resposta do CDP: %w", err)
		}
		if msg.ID != 1 {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("erro do CDP: %s", msg.Error.Message)
		}
		for _, c := range msg.Result.Cookies {
			if !strings.HasSuffix(c.Domain, CookieDomain) || c.Value == "" {
				continue
			}
			for _, nome := range ChromeCookieNames {
				if c.Name == nome {
					return &Cookie{Nome: nome, Valor: c.Value}, nil
				}
			}
		}
		return nil, fmt.Errorf("sem cookie de sessão do %s ainda", CookieDomain)
	}
}

// portaLivre devolve uma porta TCP livre no localhost.
func portaLivre() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// ---- Nome do usuário logado (cabeçalho do portal) ----

// alvoCDP é uma entrada do /json/list do DevTools.
type alvoCDP struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// usuarioViaCDP acha uma aba aberta no 360 e extrai da página o nome do
// usuário logado — o mesmo do cabeçalho do portal ("p" com font-semibold,
// leading-none e text-slate-800). Melhor esforço: vazio se não houver porta
// de debug, aba logada ou o elemento ainda não renderizou.
func usuarioViaCDP(porta int) string {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/list", porta), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	var alvos []alvoCDP
	if err := json.NewDecoder(resp.Body).Decode(&alvos); err != nil {
		resp.Body.Close()
		return ""
	}
	resp.Body.Close()
	for _, alvo := range alvos {
		if alvo.Type != "page" || !strings.Contains(alvo.URL, CookieDomain) || alvo.WebSocketDebuggerURL == "" {
			continue
		}
		if nome, err := avaliarUsuarioWS(ctx, alvo.WebSocketDebuggerURL); err == nil && nome != "" {
			return nome
		}
	}
	return ""
}

// jsNomeUsuario busca no DOM o parágrafo do nome do usuário no cabeçalho do
// portal. Seleção por substring da classe (o seletor CSS exigiria escapar o
// "dark:text-slate-100").
const jsNomeUsuario = `(function(){
var ps = document.querySelectorAll('p');
for (var i = 0; i < ps.length; i++) {
  var c = ps[i].className || '';
  if (c.indexOf('font-semibold') >= 0 && c.indexOf('leading-none') >= 0 && c.indexOf('text-slate-800') >= 0) {
    return (ps[i].textContent || '').trim();
  }
}
return '';
})()`

// avaliarUsuarioWS roda jsNomeUsuario numa aba via Runtime.evaluate.
func avaliarUsuarioWS(ctx context.Context, wsURL string) (string, error) {
	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return "", fmt.Errorf("conectar à aba do 360: %w", err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{
		"id":     1,
		"method": "Runtime.evaluate",
		"params": map[string]any{"expression": jsNomeUsuario, "returnByValue": true},
	}); err != nil {
		return "", fmt.Errorf("pedir avaliação ao CDP: %w", err)
	}
	for {
		var msg struct {
			ID     int `json:"id"`
			Result struct {
				Result struct {
					Value any `json:"value"`
				} `json:"result"`
			} `json:"result"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := conn.ReadJSON(&msg); err != nil {
			return "", fmt.Errorf("ler resposta do CDP: %w", err)
		}
		if msg.ID != 1 {
			continue
		}
		if msg.Error != nil {
			return "", fmt.Errorf("erro do CDP: %s", msg.Error.Message)
		}
		nome, _ := msg.Result.Result.Value.(string)
		return strings.TrimSpace(nome), nil
	}
}

// pastaPerfilCDP é o perfil dedicado do app para a conexão assistida.
// Fica sempre no %APPDATA% (fora do modo portátil): é um perfil do Chrome
// amarrado à máquina, não deve viajar com o exe.
func pastaPerfilCDP() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg, "RotaSmart", "perfil-cdp"), nil
}

// mataArvore encerra o Chrome de conexão e todos os seus filhos — matar só o
// lançador deixa processos órfãos sem janela que impedem o Chrome de abrir.
func mataArvore(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(cmd.Process.Pid)).Run()
	_, _ = cmd.Process.Wait()
}

// chromeExecutavel localiza o chrome.exe: App Paths do registro ou caminhos padrão.
func chromeExecutavel() (string, error) {
	if p, err := chromeDoRegistro(); err == nil && p != "" {
		return p, nil
	}
	var candidatos []string
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		candidatos = append(candidatos, filepath.Join(pf, "Google", "Chrome", "Application", "chrome.exe"))
	}
	if pf := os.Getenv("ProgramFiles(x86)"); pf != "" {
		candidatos = append(candidatos, filepath.Join(pf, "Google", "Chrome", "Application", "chrome.exe"))
	}
	if lp := os.Getenv("LOCALAPPDATA"); lp != "" {
		candidatos = append(candidatos, filepath.Join(lp, "Google", "Chrome", "Application", "chrome.exe"))
	}
	for _, c := range candidatos {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("chrome.exe não encontrado")
}

func chromeDoRegistro() (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\chrome.exe`, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	v, _, err := k.GetStringValue("")
	return v, err
}

// pastaUserData devolve o diretório de perfis do Chrome.
func pastaUserData() (string, error) {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return "", fmt.Errorf("LOCALAPPDATA não definido")
	}
	return filepath.Join(local, "Google", "Chrome", "User Data"), nil
}
