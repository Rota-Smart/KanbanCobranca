// Navegação da aba ativa do navegador: localiza a janela do navegador que
// está com o portal 360 aberto, traz para a frente e simula Ctrl+L (focar
// barra de endereço) + colar a URL + Enter — a aba atual navega para o
// destino, sem abrir janela/aba nova. Fallback: o chamador abre no navegador
// padrão. O texto da área de transferência é sobrescrito pela URL durante a
// operação (e não é restaurado, para não disputar com o colar).
package tela

import (
	"fmt"
	"log/slog"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const dominio360 = "360.rotagroup.com.br"

var (
	procEnumWindows         = user32.NewProc("EnumWindows")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procShowWindow          = user32.NewProc("ShowWindow")
	prockeybdEvent          = user32.NewProc("keybd_event")
	procSetClipboardData    = user32.NewProc("SetClipboardData")
	procEmptyClipboard      = user32.NewProc("EmptyClipboard")
	procGlobalAlloc         = kernel32.NewProc("GlobalAlloc")
	procGlobalFree          = kernel32.NewProc("GlobalFree")
)

const (
	vkControl = 0x11
	vkL       = 0x4C
	vkV       = 0x56
	vkReturn  = 0x0D
	keyup     = 0x0002
	swRestore = 9
)

// NavegarAbaAtiva faz a aba do navegador que já mostra o 360 navegar para
// a URL passada. Devolve erro se não achar janela do navegador no domínio.
func NavegarAbaAtiva(destino string) error {
	if !strings.HasPrefix(destino, "http") {
		return fmt.Errorf("URL inválida: %s", destino)
	}
	hwnd, err := janelaDo360()
	if err != nil {
		return err
	}
	procShowWindow.Call(hwnd, swRestore)
	procSetForegroundWindow.Call(hwnd)
	time.Sleep(150 * time.Millisecond) // tempo do foco mudar de fato

	// Clipboard ← URL (paste é o único modo seguro de digitar URL).
	if err := definirClipboard(destino); err != nil {
		return err
	}

	// Ctrl+L foca a barra de endereço; Ctrl+V cola; Enter navega.
	combinada(vkL)
	time.Sleep(120 * time.Millisecond)
	combinada(vkV)
	time.Sleep(150 * time.Millisecond)
	tecla(vkReturn)
	time.Sleep(80 * time.Millisecond)
	return nil
}

// janelaDo360 procura entre as janelas visíveis de navegadores Chromium
// aquela cuja aba ativa está no domínio do 360 (leitura via UIA, mesmo
// mecanismo do Capturar).
func janelaDo360() (uintptr, error) {
	var achada uintptr
	cb := windowsCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if achada != 0 {
			return 1
		}
		if r, _, _ := procIsWindowVisible.Call(hwnd); r == 0 {
			return 1
		}
		proc := processoDaJanela(hwnd)
		if !isBrowser(proc) {
			return 1
		}
		url, err := urlDaAbaAtiva(hwnd)
		if err != nil {
			return 1 // janela de navegador sem campo de URL legível (ex.: devtools)
		}
		if strings.Contains(url, dominio360) {
			achada = hwnd
		}
		return 1
	})
	r, _, _ := procEnumWindows.Call(cb, 0)
	if r == 0 {
		slog.Warn("enumwindows falhou ao procurar o 360")
	}
	if achada == 0 {
		return 0, fmt.Errorf("nenhuma aba do 360 (%s) encontrada no navegador", dominio360)
	}
	return achada, nil
}

// windowsCallback cria o ponteiro de callback para o EnumWindows.
func windowsCallback(fn func(uintptr, uintptr) uintptr) uintptr {
	return windows.NewCallback(fn)
}

// combinada segura Ctrl, pressiona/solta a tecla e solta Ctrl.
func combinada(vk byte) {
	ctrl(false)
	tecla(vk)
	ctrl(true)
}

func ctrl(soltar bool) {
	flags := uintptr(0)
	if soltar {
		flags = keyup
	}
	prockeybdEvent.Call(vkControl, 0, flags, 0)
}

// tecla pressiona e solta uma tecla.
func tecla(vk byte) {
	prockeybdEvent.Call(uintptr(vk), 0, 0, 0)
	prockeybdEvent.Call(uintptr(vk), 0, keyup, 0)
}

// definirClipboard escreve texto (CF_UNICODETEXT) na área de transferência.
func definirClipboard(texto string) error {
	r, _, _ := procOpenClipboard.Call(0)
	if r == 0 {
		return fmt.Errorf("clipboard ocupado")
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	u16, err := syscall.UTF16FromString(texto)
	if err != nil {
		return err
	}
	h, _, _ := procGlobalAlloc.Call(0x2002, uintptr(len(u16)*2)) // GMEM_MOVEABLE|GMEM_ZEROINIT
	if h == 0 {
		return fmt.Errorf("alocar clipboard")
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return fmt.Errorf("travar clipboard")
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(u16)), u16)
	procGlobalUnlock.Call(h)
	if r, _, _ := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("gravar clipboard")
	}
	return nil
}
