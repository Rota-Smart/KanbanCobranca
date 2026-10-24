package rota360

import (
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	_ "modernc.org/sqlite"
)

// CookieDomain é o host cujo cookie de sessão o app procura no Chrome.
const CookieDomain = "360.rotagroup.com.br"

// ChromeCookieNames são os cookies de sessão aceitos, em ordem de preferência.
var ChromeCookieNames = []string{"rota360_auth", "session", "sid", "connect.sid", "auth", "token"}

type dataBlob struct{ cbData, pbData uintptr }

// dpapiDescriptografa um blob protegido do usuário atual (Windows DPAPI).
func dpapiDescriptografa(b []byte) ([]byte, error) {
	in := &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(in, nil, nil, 0, nil, 0, &out); err != nil {
		return nil, fmt.Errorf("dpapi: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data))) //nolint:errcheck
	gov := make([]byte, out.Size)
	copy(gov, unsafe.Slice(out.Data, out.Size))
	return gov, nil
}

// chaveAESDoLocalState lê `os_crypt.encrypted_key` do Local State do perfil
// e desprotege via DPAPI (esquema v10 do Chrome).
func chaveAESDoLocalState(userdata, perfil string) ([]byte, error) {
	raw, err := os.ReadFile(filepath.Join(userdata, "Local State"))
	if err != nil {
		return nil, err
	}
	var ls struct {
		OsCrypt struct {
			EncryptedKey string `json:"encrypted_key"`
		} `json:"os_crypt"`
	}
	if err := json.Unmarshal(raw, &ls); err != nil {
		return nil, err
	}
	k, err := base64.StdEncoding.DecodeString(ls.OsCrypt.EncryptedKey)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(string(k), "DPAPI") {
		return nil, fmt.Errorf("chave os_crypt sem prefixo DPAPI")
	}
	return dpapiDescriptografa(k[5:])
}

// descriptografaValor trata os prefixos conhecidos do Chrome.
// v10 (AES-GCM com chave do Local State) é suportado; v20/v11 (App-Bound)
// não é descriptografável por processos externos — devolve erro claro,
// e o app cai no fallback de colar o cookie nas configurações.
func descriptografaValor(enc []byte, chave []byte) (string, error) {
	if len(enc) == 0 {
		return "", nil
	}
	switch {
	case string(enc[:3]) == "v10":
		if len(enc) < 3+12+16 {
			return "", fmt.Errorf("blob v10 curto")
		}
		block, err := aes.NewCipher(chave)
		if err != nil {
			return "", err
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return "", err
		}
		nonce, corpo := enc[3:3+12], enc[3+12:]
		plain, err := gcm.Open(nil, nonce, corpo, nil)
		if err != nil {
			return "", fmt.Errorf("gcm: %w", err)
		}
		return string(plain), nil
	case string(enc[:3]) == "v20" || string(enc[:3]) == "v11":
		return "", fmt.Errorf("cookie com App-Bound Encryption (%s) — cole o cookie nas configurações", string(enc[:3]))
	default:
		// Perfil antigo: DPAPI direto no blob.
		plain, err := dpapiDescriptografa(enc)
		if err != nil {
			return "", err
		}
		return string(plain), nil
	}
}

// caminhosChrome devolve os perfis do Chrome a inspecionar, mais provável primeiro.
func caminhosChrome() ([]string, error) {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return nil, fmt.Errorf("LOCALAPPDATA não definido")
	}
	userdata := filepath.Join(local, "Google", "Chrome", "User Data")
	entradas, err := os.ReadDir(userdata)
	if err != nil {
		return nil, err
	}
	var perfis []string
	prioridade := []string{"Default", "Profile 1", "Profile 2"}
	for _, p := range prioridade {
		if _, err := os.Stat(filepath.Join(userdata, p, "Network", "Cookies")); err == nil {
			perfis = append(perfis, p)
		}
	}
	for _, e := range entradas {
		n := e.Name()
		if strings.HasPrefix(n, "Profile ") && !contem(perfis, n) {
			if _, err := os.Stat(filepath.Join(userdata, n, "Network", "Cookies")); err == nil {
				perfis = append(perfis, n)
			}
		}
	}
	return perfis, nil
}

func contem(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// copiaCookieDB copia o banco de cookies (está locked pelo Chrome) para um
// arquivo temporário legível. best-effort para WAL.
func copiaCookieDB(origem string) (string, error) {
	tmp, err := os.CreateTemp("", "rota-cookies-*.db")
	if err != nil {
		return "", err
	}
	dst := tmp.Name()
	tmp.Close()
	if err := copiaArquivo(origem, dst); err != nil {
		os.Remove(dst)
		return "", err
	}
	_ = copiaArquivo(origem+"-wal", dst+"-wal")
	_ = copiaArquivo(origem+"-shm", dst+"-shm")
	return dst, nil
}

func copiaArquivo(origem, dst string) error {
	src, err := os.Open(origem)
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, src)
	return err
}

// DetectaCookieChrome procura o cookie de sessão do 360 nos perfis do Chrome.
// Devolve o cookie pronto para o Cliente, ou erro explicando o fallback.
func DetectaCookieChrome() (*Cookie, error) {
	perfis, err := caminhosChrome()
	if err != nil {
		return nil, fmt.Errorf("Chrome não encontrado: %w", err)
	}
	local := os.Getenv("LOCALAPPDATA")
	userdata := filepath.Join(local, "Google", "Chrome", "User Data")

	var ultimoErro error
	for _, perfil := range perfis {
		cookie, err := cookieDoPerfil(userdata, perfil)
		if err == nil && cookie != nil {
			return cookie, nil
		}
		if err != nil {
			ultimoErro = err
		}
	}
	if ultimoErro != nil {
		return nil, ultimoErro
	}
	return nil, fmt.Errorf("nenhum cookie de sessão do %s encontrado nos perfis do Chrome — faça login no site pelo Chrome e tente de novo", CookieDomain)
}

// Cookie é o par nome=valor da sessão do 360. Usuario (opcional) é o nome
// exibido no cabeçalho do portal — quem está logado — lido via CDP quando a
// aba do 360 está aberta; alimenta o filtro de "autor da sessão" do chat.
type Cookie struct {
	Nome    string
	Valor   string
	Usuario string
}

func cookieDoPerfil(userdata, perfil string) (*Cookie, error) {
	chave, err := chaveAESDoLocalState(userdata, perfil)
	if err != nil {
		return nil, fmt.Errorf("perfil %s: %w", perfil, err)
	}
	dbPath, err := copiaCookieDB(filepath.Join(userdata, perfil, "Network", "Cookies"))
	if err != nil {
		return nil, fmt.Errorf("perfil %s: copiar cookies: %w", perfil, err)
	}
	defer os.Remove(dbPath)

	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=busy_timeout(3000)")
	if err != nil {
		return nil, fmt.Errorf("perfil %s: abrir cookies: %w", perfil, err)
	}
	defer db.Close()

	for _, nome := range ChromeCookieNames {
		var enc []byte
		err := db.QueryRow(`SELECT encrypted_value FROM cookies WHERE host_key LIKE ? AND name = ?`,
			"%"+CookieDomain, nome).Scan(&enc)
		if err != nil {
			continue
		}
		valor, err := descriptografaValor(enc, chave)
		if err != nil {
			return nil, fmt.Errorf("perfil %s: %w", perfil, err)
		}
		if valor != "" {
			return &Cookie{Nome: nome, Valor: valor}, nil
		}
	}
	return nil, nil
}

// SessaoDetectada tenta o banco de cookies do Chrome e, em falha, o Chrome
// DevTools Protocol (o Chrome recente trava o banco e cifra os cookies com
// App-Bound, que só o próprio Chrome consegue ler).
func SessaoDetectada() (*Cliente, string, error) {
	cookie, err := DetectaCookieChrome()
	if err == nil {
		return NovoCliente(cookie), "chrome", nil
	}
	if cookie, errCdp := DetectaCookieCDP(); errCdp == nil {
		return NovoCliente(cookie), "chrome (CDP)", nil
	}
	return nil, "", err
}

// ParseCookieColado aceita "valor", "nome=valor" ou o cabeçalho Cookie
// inteiro colado pelo usuário e devolve o cookie da sessão do 360.
func ParseCookieColado(txt string) (*Cookie, error) {
	txt = strings.TrimSpace(txt)
	if txt == "" {
		return nil, fmt.Errorf("cookie vazio")
	}
	if strings.Contains(txt, "=") {
		for _, parte := range strings.Split(txt, ";") {
			k, v, _ := strings.Cut(strings.TrimSpace(parte), "=")
			for _, nome := range ChromeCookieNames {
				if strings.EqualFold(strings.TrimSpace(k), nome) && v != "" {
					return &Cookie{Nome: nome, Valor: strings.TrimSpace(v)}, nil
				}
			}
		}
		// Único par "nome=valor" desconhecido: aceita se parecer sessão.
		if !strings.Contains(txt, ";") {
			k, v, _ := strings.Cut(txt, "=")
			return &Cookie{Nome: strings.TrimSpace(k), Valor: strings.TrimSpace(v)}, nil
		}
		return nil, fmt.Errorf("nenhum cookie de sessão reconhecido (procure por %s)", strings.Join(ChromeCookieNames, "/"))
	}
	return &Cookie{Nome: ChromeCookieNames[0], Valor: txt}, nil
}
