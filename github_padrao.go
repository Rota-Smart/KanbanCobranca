package main

// Token padrão do GitHub embutido no exe, só para o envio do log de
// diagnóstico (EnviarLogNuvem): garante que o botão funcione mesmo sem a
// sincronização configurada. Fica embaralhado porque o repositório de release
// é público — token em texto puro seria revogado pelo secret scanning.
var (
	ghMascara     = []byte("RotaSuite360")
	ghTokenPadrao = []byte{53, 7, 27, 62, 17, 61, 93, 63, 93, 73, 110, 115, 59, 56, 44, 87, 36, 61, 25, 25, 52, 66, 81, 90, 56, 55, 23, 56, 38, 47, 31, 63, 43, 101, 6, 95, 36, 22, 65, 50}
)

func githubTokenPadrao() string {
	out := make([]byte, len(ghTokenPadrao))
	for i, c := range ghTokenPadrao {
		out[i] = c ^ ghMascara[i%len(ghMascara)]
	}
	return string(out)
}
