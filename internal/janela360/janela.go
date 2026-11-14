//go:build windows

// Package janela360 abre o painel do 360 numa janela WebView2 nativa
// (mesma engine do Wails, via github.com/wailsapp/go-webview2), sem iframe:
// processo separado, acelerado por GPU, não trava o quadro do app.
// O cookie de sessão do 360 é injetado via CookieManager antes de navegar —
// o usuário não loga de novo; se a sessão venceu, o próprio 360 pede login
// dentro da janela e o WebView2 guarda o cookie novo (perfil persistente).
package janela360

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sync"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

var (
	user32              = windows.NewLazySystemDLL("user32.dll")
	kernel32            = windows.NewLazySystemDLL("kernel32.dll")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
)

const (
	wsOverlappedWindow = 0x00CF0000 // titlebar + bordas + min/max
	wsVisible          = 0x10000000
	wsMinimizeBox      = 0x00020000
	swShow             = 5
	wmSize             = 0x0005
	wmClose            = 0x0010
	wmDestroy          = 0x0002
	colorWindow        = 5 // COLOR_WINDOW
	smCxScreen         = 0
	smCyScreen         = 0
	className          = "RoataSuiteCRM360"
)

type wndClassExW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type msgW struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

type janela struct {
	chrome *edge.Chromium
}

var (
	janelasMu sync.Mutex
	// hwnd → janela, para o wndproc achar o dono da mensagem (a janela não
	// está no mapa durante a criação; essas mensagens vão ao DefWindowProc).
	porHwnd = map[uintptr]*janela{}
	registro sync.Once
	registrada bool
)

func wndproc(hwnd, msg, wp, lp uintptr) uintptr {
	janelasMu.Lock()
	j := porHwnd[hwnd]
	janelasMu.Unlock()
	if j == nil {
		r, _, _ := procDefWindowProcW.Call(hwnd, msg, wp, lp)
		return r
	}
	switch msg {
	case wmSize:
		j.chrome.Resize()
		return 0
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		janelasMu.Lock()
		delete(porHwnd, hwnd)
		janelasMu.Unlock()
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}

func garantirClasse() bool {
	registro.Do(func() {
		hinst, _, _ := procGetModuleHandleW.Call(0)
		wc := wndClassExW{
			CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
			LpfnWndProc:   windows.NewCallback(wndproc),
			HInstance:     hinst,
			HbrBackground: colorWindow + 1,
			LpszClassName: windows.StringToUTF16Ptr(className),
		}
		r, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		registrada = r != 0
	})
	return registrada
}

// Abrir abre uma janela nova navegando em url com o cookie de sessão injetado.
// Bloqueia até a janela ser fechada — chamar em goroutine própria.
func Abrir(titulo, url string, cookie *http.Cookie) error {
	if url == "" {
		return fmt.Errorf("url vazia")
	}
	// A janela, a fila de mensagens e o WebView2 precisam viver num só thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if !garantirClasse() {
		return fmt.Errorf("não registrou a classe da janela CRM")
	}
	const larg, alt = 1280, 860
	telaX, _, _ := procGetSystemMetrics.Call(smCxScreen)
	telaY, _, _ := procGetSystemMetrics.Call(smCyScreen)
	x, y := int32(60), int32(60)
	if telaX > uintptr(larg) {
		x = (int32(telaX) - larg) / 2
	}
	if telaY > uintptr(alt) {
		y = (int32(telaY) - alt) / 2
	}
	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(className))),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(titulo))),
		wsOverlappedWindow|wsVisible|wsMinimizeBox,
		uintptr(x), uintptr(y), larg, alt,
		0, 0, 0, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("não criou a janela CRM: %v", err)
	}
	j := &janela{}
	janelasMu.Lock()
	porHwnd[hwnd] = j
	janelasMu.Unlock()

	j.chrome = edge.NewChromium()
	j.chrome.DataPath = pastaDados()
	j.chrome.SetGlobalPermission(edge.CoreWebView2PermissionStateAllow)
	if !j.chrome.Embed(hwnd) {
		return fmt.Errorf("não embutiu o WebView2 na janela CRM")
	}
	j.chrome.Resize()

	if cookie != nil && cookie.Name != "" && cookie.Value != "" {
		if cm, err := j.chrome.GetCookieManager(); err == nil {
			if ck, err := cm.CreateCookie(cookie.Name, cookie.Value, ".rotagroup.com.br", "/"); err == nil {
				_ = ck.PutIsSecure(true)
				_ = cm.AddOrUpdateCookie(ck)
			}
		}
	}
	j.chrome.Navigate(url)

	// loop de mensagens da janela, até WM_DESTROY/WM_QUIT
	var m msgW
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}

// pastaDados é o perfil próprio da janela CRM — cookies persistem entre
// execuções (login feito lá uma vez vale para sempre).
func pastaDados() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = "."
	}
	return base + "\\RotaSuite\\CRM"
}
