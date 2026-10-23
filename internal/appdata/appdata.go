// Package appdata resolve a pasta de dados locais do app.
//
// Modo portátil: se existir a pasta "RotaSmart-dados" ao lado do executável,
// todos os dados locais (banco, planilha de orçamento, modelo de voz, perfil
// de conexão do 360) ficam nela — assim o exe distribuído vai com as
// configurações junto. Sem a pasta, usa o padrão %APPDATA%\RotaSmart.
package appdata

import (
	"os"
	"path/filepath"
)

// NomePortatil é a pasta de dados procurada ao lado do exe.
const NomePortatil = "RotaSmart-dados"

// Dir devolve a pasta de dados do app (cria se não existir).
func Dir() (string, error) {
	if exe, err := os.Executable(); err == nil {
		portatil := filepath.Join(filepath.Dir(exe), NomePortatil)
		if st, err := os.Stat(portatil); err == nil && st.IsDir() {
			return portatil, nil
		}
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "RotaSmart")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// Caminho junta Dir() com os elementos de caminho dados.
func Caminho(elem ...string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{dir}, elem...)...), nil
}
