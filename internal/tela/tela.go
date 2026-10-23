// Package tela captura o contexto da janela em foco do Windows: processo,
// título, URL da aba ativa (navegadores Chromium, via UI Automation) e o
// texto da área de transferência. Serve de matéria-prima para identificar
// o ticket/cliente que o usuário está olhando.
package tela

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Captura é o retrato da janela em foco no momento da chamada.
type Captura struct {
	Processo  string `json:"processo"`
	Titulo    string `json:"titulo"`
	URL       string `json:"url"`
	Clipboard string `json:"clipboard"`
}

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procGetForegroundWin   = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW     = user32.NewProc("GetWindowTextW")
	procGetWindowThreadPID = user32.NewProc("GetWindowThreadProcessId")
	procOpenClipboard      = user32.NewProc("OpenClipboard")
	procCloseClipboard     = user32.NewProc("CloseClipboard")
	procGetClipboardData   = user32.NewProc("GetClipboardData")
	procOpenProcess        = kernel32.NewProc("OpenProcess")
	procCloseHandle        = kernel32.NewProc("CloseHandle")
	procQueryFullProcName  = kernel32.NewProc("QueryFullProcessImageNameW")
	procGlobalLock         = kernel32.NewProc("GlobalLock")
	procGlobalUnlock       = kernel32.NewProc("GlobalUnlock")
)

const processQueryLimitedInformation = 0x1000
const cfUnicodeText = 13

// Capturar devolve o retrato da janela em foco. Nenhum campo é crítico:
// falhas individuais (COM, clipboard) viram campo vazio, não erro.
func Capturar() Captura {
	hwnd, _, _ := procGetForegroundWin.Call()
	c := Captura{
		Titulo:    tituloDaJanela(hwnd),
		Processo:  processoDaJanela(hwnd),
		Clipboard: textoClipboard(),
	}
	if isBrowser(c.Processo) || c.Processo == "" {
		// Sem o nome do processo (sandbox/acesso negado), tentamos mesmo
		// assim: urlDaAbaAtiva só aceita resultado que seja uma URL.
		if url, err := urlDaAbaAtiva(hwnd); err == nil {
			c.URL = url
		}
	}
	return c
}

func tituloDaJanela(hwnd uintptr) string {
	if hwnd == 0 {
		return ""
	}
	buf := make([]uint16, 512)
	n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

func processoDaJanela(hwnd uintptr) string {
	if hwnd == 0 {
		return ""
	}
	var pid uint32
	procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return ""
	}
	h, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)
	buf := make([]uint16, 1024)
	var n uint32 = uint32(len(buf))
	// Assinatura: (handle, dwFlags, lpExeName, lpdwSize) — buffer e tamanho
	// estavam trocados, e a chamada falhava sempre.
	r, _, _ := procQueryFullProcName.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return ""
	}
	caminho := windows.UTF16ToString(buf[:n])
	if i := strings.LastIndexByte(caminho, '\\'); i >= 0 {
		caminho = caminho[i+1:]
	}
	return strings.ToLower(caminho)
}

func isBrowser(processo string) bool {
	switch {
	case processo == "chrome.exe", processo == "msedge.exe", processo == "brave.exe",
		processo == "opera.exe", processo == "vivaldi.exe":
		return true
	}
	return false
}

// textoClipboard lê o texto da área de transferência (CF_UNICODETEXT),
// limitado a 2000 caracteres — é só pista de identificação, não armazenamento.
func textoClipboard() string {
	r, _, _ := procOpenClipboard.Call(0)
	if r == 0 {
		return ""
	}
	defer procCloseClipboard.Call()
	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return ""
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return ""
	}
	defer procGlobalUnlock.Call(h)
	// UTF16PtrToString para até o NUL, com teto de segurança.
	s := windows.UTF16PtrToString((*uint16)(unsafe.Pointer(p)))
	if len(s) > 2000 {
		s = s[:2000]
	}
	return s
}

// ---- UI Automation: URL da aba ativa ----

// CUIAutomation / IID_IUIAutomation (uiautomationcore.h).
var (
	guidUIAClass         = guidDe("{ff48dba4-60ef-4201-aa87-54103eef594e}")
	guidUIAIID           = guidDe("{30cbe57d-d9d0-452a-ab13-7ac5ac4825ee}")
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	oleaut32             = windows.NewLazySystemDLL("oleaut32.dll")
	procVariantClear     = oleaut32.NewProc("VariantClear")
)

type guid struct {
	data1 uint32
	data2 uint16
	data3 uint16
	data4 [8]byte
}

func guidDe(s string) guid {
	g, err := windows.GUIDFromString(s)
	if err != nil {
		panic(fmt.Sprintf("guid fixo inválido: %v", err))
	}
	return guid{g.Data1, g.Data2, g.Data3, g.Data4}
}

// variant é a estrutura de 24 bytes do Windows x64; usada só para VT_I4/VT_BSTR.
type variant struct {
	vt        uint16
	reservado [3]uint16
	val       uintptr
}

const (
	vtI4             = 3
	vtBSTR           = 8
	propControlType  = 30003
	propValue        = 30045 // UIA_ValueValuePropertyId (30012 é ClassName!)
	ctrlEdit         = 50004
	scopeDescendants = 4
)

// vcall chama a entrada idx da vtable do objeto COM e devolve o HRESULT.
func vcall(obj uintptr, idx int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(idx)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	return r
}

func bstrParaString(p uintptr) string {
	if p == 0 {
		return ""
	}
	n := *(*uint32)(unsafe.Pointer(p - 4)) // BSTR guarda o tamanho em chars antes do ponteiro
	u := unsafe.Slice((*uint16)(unsafe.Pointer(p)), n)
	return windows.UTF16ToString(u)
}

// urlDaAbaAtiva lê a barra de endereço da janela do navegador via UIA.
// Só funciona se a aba ativa for do 360 — o chamador filtra.
func urlDaAbaAtiva(hwnd uintptr) (string, error) {
	if hwnd == 0 {
		return "", fmt.Errorf("janela vazia")
	}
	hr, _, _ := procCoInitializeEx.Call(0, 2) // COINIT_APARTMENTTHREADED
	mudouModo := hr == 0x80010106             // RPC_E_CHANGED_MODE: segue mesmo assim
	if hr != 0 && !mudouModo {
		return "", fmt.Errorf("com init: 0x%x", hr)
	}
	defer func() {
		if !mudouModo {
			procCoUninitialize.Call()
		}
	}()

	var uiAutomation uintptr
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&guidUIAClass)), 0, 1, /*INPROC_SERVER*/
		uintptr(unsafe.Pointer(&guidUIAIID)), uintptr(unsafe.Pointer(&uiAutomation)))
	if hr != 0 || uiAutomation == 0 {
		return "", fmt.Errorf("criar UIAutomation: 0x%x", hr)
	}
	defer vcall(uiAutomation, 2) // Release

	// ElementFromHandle(hwnd, &elem) — vtable idx 6.
	var elem uintptr
	if vcall(uiAutomation, 6, hwnd, uintptr(unsafe.Pointer(&elem))) != 0 || elem == 0 {
		return "", fmt.Errorf("elemento da janela")
	}
	defer vcall(elem, 2)

	// CreatePropertyCondition(ControlType=Edit, &cond) — idx 23.
	cond := variant{vt: vtI4, val: ctrlEdit}
	var condição uintptr
	if vcall(uiAutomation, 23, propControlType, uintptr(unsafe.Pointer(&cond)), uintptr(unsafe.Pointer(&condição))) != 0 || condição == 0 {
		return "", fmt.Errorf("condição edit")
	}
	defer vcall(condição, 2)

	// FindAll(TreeScope_Descendants, cond, &achados) — idx 6: a barra de
	// endereço costuma ser o primeiro Edit, mas navegadores variam.
	var achados uintptr
	if vcall(elem, 6, scopeDescendants, condição, uintptr(unsafe.Pointer(&achados))) != 0 || achados == 0 {
		return "", fmt.Errorf("nenhum campo edit na janela")
	}
	defer vcall(achados, 2)

	// IUIAutomationElementArray: get_Length (idx 3), GetElement (idx 4).
	var n int32
	if vcall(achados, 3, uintptr(unsafe.Pointer(&n))) != 0 {
		return "", fmt.Errorf("tamanho da lista")
	}
	for i := 0; i < int(n) && i < 64; i++ {
		var item uintptr
		if vcall(achados, 4, uintptr(i), uintptr(unsafe.Pointer(&item))) != 0 || item == 0 {
			continue
		}
		url, ok := valorURL(item)
		vcall(item, 2) // Release
		if ok {
			return url, nil
		}
	}
	return "", fmt.Errorf("nenhum campo com URL na janela")
}

// valorURL lê UIA_ValueValuePropertyID do elemento e aceita só o que parece
// URL. O Chrome devolve a URL da omnibox SEM o esquema ("host/caminho"), e
// campos de login da página também expõem Value — daí excluir "@" e exigir
// ponto + barra. O chamador filtra pelo domínio do 360.
func valorURL(elem uintptr) (string, bool) {
	var v variant
	if vcall(elem, 10, propValue, uintptr(unsafe.Pointer(&v))) != 0 {
		return "", false
	}
	defer procVariantClear.Call(uintptr(unsafe.Pointer(&v)))
	if v.vt != vtBSTR {
		return "", false
	}
	url := strings.TrimSpace(bstrParaString(v.val))
	if url == "" || strings.ContainsAny(url, " \t@") {
		return "", false
	}
	if strings.HasPrefix(url, "http") {
		return url, true
	}
	if strings.Contains(url, ".") && strings.Contains(url, "/") {
		return "https://" + url, true
	}
	return "", false
}
