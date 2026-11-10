package tela

// Captura de imagem (print) da janela em foco via GDI (BitBlt) e encode em
// JPEG. Uso temporário: o Shift+F3 com o Service em foco sobe o print para a
// nuvem (logs_app) para mapear os campos da tela do Access.

import (
	"bytes"
	"image"
	"image/jpeg"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procGetDC        = user32.NewProc("GetDC")
	procReleaseDC    = user32.NewProc("ReleaseDC")
	procGetWindowRect = user32.NewProc("GetWindowRect")
	gdi32            = windows.NewLazySystemDLL("gdi32.dll")
	procCreateCompatDC   = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC         = gdi32.NewProc("DeleteDC")
	procCreateCompatBmp  = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject     = gdi32.NewProc("SelectObject")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procBitBlt           = gdi32.NewProc("BitBlt")
	procGetDIBits        = gdi32.NewProc("GetDIBits")
)

const (
	srccopy       = 0x00CC0020
	biRGB         = 0
	dibRGBColors  = 0
)

// rect é a estrutura RECT do Windows.
type rect struct {
	Esq, Top, Dir, Base int32
}

// bitmapinfoheader é o BITMAPINFOHEADER para ler os pixels do bitmap.
type bitmapinfoheader struct {
	Tamanho        uint32
	Largura        int32
	Altura         int32
	Planos         uint16
	BitsPorPixel   uint16
	Compressao     uint32
	TamanhoImagem  uint32
	XPPM           int32
	YPPM           int32
	CoresUsadas    uint32
	CoresImport    uint32
}

// CapturaPrint devolve o print da janela em foco codificado em JPEG
// (qualidade 55 — o teto do envio é de ~900KB). Falha devolve nil.
func CapturaPrint() []byte {
	hwnd, _, _ := procGetForegroundWin.Call()
	if hwnd == 0 {
		return nil
	}
	var r rect
	if r1, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); r1 == 0 {
		return nil
	}
	larg := int(r.Dir - r.Esq)
	alt := int(r.Base - r.Top)
	if larg <= 0 || alt <= 0 || larg > 8000 || alt > 8000 {
		return nil
	}

	hdcTela, _, _ := procGetDC.Call(0)
	if hdcTela == 0 {
		return nil
	}
	defer procReleaseDC.Call(0, hdcTela)

	hdcMem, _, _ := procCreateCompatDC.Call(hdcTela)
	if hdcMem == 0 {
		return nil
	}
	defer procDeleteDC.Call(hdcMem)

	hbmp, _, _ := procCreateCompatBmp.Call(hdcTela, uintptr(larg), uintptr(alt))
	if hbmp == 0 {
		return nil
	}
	velho, _, _ := procSelectObject.Call(hdcMem, hbmp)
	defer procSelectObject.Call(hdcMem, velho)
	defer procDeleteObject.Call(hbmp)

	if r1, _, _ := procBitBlt.Call(hdcMem, 0, 0, uintptr(larg), uintptr(alt), hdcTela, uintptr(r.Esq), uintptr(r.Top), srccopy); r1 == 0 {
		return nil
	}

	// Lê os pixels de volta como BGRX de 32 bits (de baixo pra cima).
	bih := bitmapinfoheader{
		Tamanho:      uint32(unsafe.Sizeof(bitmapinfoheader{})),
		Largura:      int32(larg),
		Altura:       int32(alt),
		Planos:       1,
		BitsPorPixel: 32,
		Compressao:   biRGB,
	}
	linha := larg * 4
	buf := make([]byte, linha*alt)
	if r1, _, _ := procGetDIBits.Call(hdcMem, hbmp, 0, uintptr(alt),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bih)), dibRGBColors); r1 == 0 {
		return nil
	}

	img := image.NewRGBA(image.Rect(0, 0, larg, alt))
	for y := 0; y < alt; y++ {
		linhaSrc := buf[(alt-1-y)*linha : (alt-y)*linha]
		linhaDst := img.Pix[y*img.Stride : y*img.Stride+larg*4]
		for x := 0; x < larg; x++ {
			// BGRX ( little-endian ) → RGBA.
			linhaDst[x*4+0] = linhaSrc[x*4+2]
			linhaDst[x*4+1] = linhaSrc[x*4+1]
			linhaDst[x*4+2] = linhaSrc[x*4+0]
			linhaDst[x*4+3] = 255
		}
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 55}); err != nil {
		return nil
	}
	return out.Bytes()
}
