// Package hotkey registra atalhos globais do Windows (valem em qualquer
// aplicativo em foco) e dispara callbacks via WM_HOTKEY. O app é Windows-only,
// então o pacote não tem fallback para outros sistemas.
package hotkey

import (
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmHotkey    = 0x0312
	modShift    = 0x0004
	modNoRepeat = 0x4000
	vkF2        = 0x71
	vkF3        = 0x72
	vkF4        = 0x73
	vkF5        = 0x74
)

const (
	idTicketTela    = 1 // Shift+F2
	idBuscaClientes = 2 // Shift+F3
	idBuscaTickets  = 3 // Shift+F4
	idAcessoTela    = 4 // Shift+F5
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procRegisterHotKey   = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey = user32.NewProc("UnregisterHotKey")
	procGetMessageW      = user32.NewProc("GetMessageW")
)

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

// Registrar registra Shift+F2 (ticket da tela), Shift+F3 (busca de clientes),
// Shift+F4 (busca de tickets) e Shift+F5 (leitura da tela do Access)
// globalmente e entra no loop de mensagens até o processo sair. Cada disparo
// roda o callback correspondente em nova goroutine.
// Bloqueia a thread atual — chamar de uma goroutine dedicada.
func Registrar(onTicketTela, onBuscaClientes, onBuscaTickets, onAcessoTela func()) error {
	errCh := make(chan error, 1)
	go func() {
		// RegisterHotKey é por thread: registra e bombeia mensagens na mesma.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		atalhos := []struct {
			id, vk uintptr
			cb     func()
		}{
			{idTicketTela, vkF2, onTicketTela},
			{idBuscaClientes, vkF3, onBuscaClientes},
			{idBuscaTickets, vkF4, onBuscaTickets},
			{idAcessoTela, vkF5, onAcessoTela},
		}
		for _, a := range atalhos {
			r, _, err := procRegisterHotKey.Call(0, a.id, uintptr(modShift|modNoRepeat), a.vk)
			if r == 0 {
				errCh <- fmt.Errorf("Shift+F%d já em uso por outro programa (%w)", a.id+1, err)
				return
			}
			defer procUnregisterHotKey.Call(0, a.id) //nolint:errcheck
		}
		errCh <- nil

		var m msg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			// 0 = WM_QUIT, -1 = erro: em ambos encerra (o app está saindo).
			if r == 0 || int32(r) == -1 {
				return
			}
			if m.message != wmHotkey {
				continue
			}
			switch m.wParam {
			case idTicketTela:
				go onTicketTela()
			case idBuscaClientes:
				go onBuscaClientes()
			case idBuscaTickets:
				go onBuscaTickets()
			case idAcessoTela:
				go onAcessoTela()
			}
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-time.After(2 * time.Second):
		return fmt.Errorf("tempo esgotado ao registrar atalhos")
	}
}
