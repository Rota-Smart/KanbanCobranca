package main

// Fala→texto local via whisper.cpp (whisper-cli.exe + modelo ggml small
// quantizado, ~190 MB, OpenAI Whisper MIT). Os binários e o modelo vivem na
// pasta de dados do app (%APPDATA%\RotaSmart\whisper), FORA do exe — o
// vosk-browser/WASM foi abandonado: o modelo pequeno dele era impreciso e o
// grande estourava a memória do WASM. O frontend grava WAV 16 kHz, manda os
// bytes, e aqui roda o whisper-cli e devolve o texto.

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"rota-smart/internal/appdata"
)

const modeloWhisperNome = "ggml-small-q5_1.bin"

// caminhoWhisper devolve a pasta onde devem estar o whisper-cli.exe (em
// Release/) e o modelo ggml.
func caminhoWhisper() string {
	dir, err := appdata.Caminho("whisper")
	if err != nil {
		return ""
	}
	return dir
}

// whisperInstalado informa se os binários e o modelo estão no lugar.
func whisperInstalado() bool {
	base := caminhoWhisper()
	if base == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(base, "Release", "whisper-cli.exe")); err != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(base, modeloWhisperNome))
	return err == nil
}

// TranscreverVoz recebe o WAV 16 kHz mono gravado no frontend (em base64 —
// a ponte do Wails não carrega Uint8Array), roda o whisper-cli e devolve o
// texto reconhecido em português.
func (a *App) TranscreverVoz(wavB64 string) (string, error) {
	wav, err := base64.StdEncoding.DecodeString(wavB64)
	if err != nil {
		return "", fmt.Errorf("áudio inválido")
	}
	base := caminhoWhisper()
	exe := filepath.Join(base, "Release", "whisper-cli.exe")
	modelo := filepath.Join(base, modeloWhisperNome)
	if _, err := os.Stat(exe); err != nil {
		return "", fmt.Errorf("o reconhecedor de voz (Whisper) não está instalado na pasta %s", base)
	}
	if _, err := os.Stat(modelo); err != nil {
		return "", fmt.Errorf("o modelo de voz não está em %s", modelo)
	}

	tmp, err := os.CreateTemp("", "rotasuite-voz-*.wav")
	if err != nil {
		return "", friendly("TranscreverVoz", err)
	}
	caminho := tmp.Name()
	defer os.Remove(caminho)
	if _, err := tmp.Write(wav); err != nil {
		tmp.Close()
		return "", friendly("TranscreverVoz", err)
	}
	tmp.Close()

	// -nt: só o texto, sem carimbos; -l pt fixa o idioma (evita detecção
	// errada em frases curtas); 90s de teto para não pendurar o chat.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-m", modelo, "-f", caminho, "-l", "pt", "-nt")
	// Sem janela de console: o app é GUI e o console aberto além de feio,
	// quando fechado mata o whisper (status 0xc000013a).
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	var saida, erros bytes.Buffer
	cmd.Stdout = &saida
	cmd.Stderr = &erros
	if err := cmd.Run(); err != nil {
		detalhe := erros.String()
		if len(detalhe) > 300 {
			detalhe = detalhe[len(detalhe)-300:]
		}
		slog.Error("whisper-cli", "err", err, "stderr", detalhe)
		if ctx.Err() != nil {
			return "", fmt.Errorf("a transcrição demorou demais e foi cancelada")
		}
		return "", fmt.Errorf("o reconhecedor de voz falhou ao processar o áudio")
	}
	texto := strings.Join(strings.Fields(strings.TrimSpace(saida.String())), " ")
	if texto == "" {
		return "", fmt.Errorf("não entendi o que você falou — tente de novo, mais perto do microfone")
	}
	return texto, nil
}
