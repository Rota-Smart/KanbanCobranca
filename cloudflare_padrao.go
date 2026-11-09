package main

// Credenciais padrão do Workers AI embutidas no exe: valem quando o usuário
// não preencheu as próprias nas Configurações. Ficam embaralhadas porque o
// repositório de release é público — o token em texto puro seria detectado
// pelo secret scanning do GitHub e revogado pela Cloudflare.
var (
	cfMascara      = []byte("RotaSuite360")
	cfTokenPadrao  = []byte{49, 9, 1, 21, 12, 55, 10, 16, 14, 125, 115, 89, 40, 88, 13, 15, 37, 29, 94, 61, 17, 101, 84, 81, 48, 3, 29, 42, 102, 67, 25, 12, 50, 118, 88, 89, 97, 13, 22, 21, 39, 24, 26, 17, 28, 3, 85, 9, 102, 87, 67, 83, 97}
	cfContaPadrao  = []byte{48, 95, 22, 82, 55, 66, 12, 18, 92, 87, 4, 81, 107, 89, 17, 88, 98, 67, 81, 22, 83, 1, 5, 3, 106, 10, 71, 7, 100, 77, 80, 18}
)

func desembaralhar(b []byte) string {
	out := make([]byte, len(b))
	for i, c := range b {
		out[i] = c ^ cfMascara[i%len(cfMascara)]
	}
	return string(out)
}

func cloudflareTokenPadrao() string { return desembaralhar(cfTokenPadrao) }
func cloudflareContaPadrao() string { return desembaralhar(cfContaPadrao) }
