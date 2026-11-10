package service

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// TurnoVisitaValido informa se o turno existe.
func TurnoVisitaValido(t string) bool {
	for _, x := range TurnosVisitaValidos {
		if x == t {
			return true
		}
	}
	return false
}

// reHoraVisita aceita as variações de horário que o modelo/usuário costuma
// mandar no `quando` da visita: 8:45, 08:45, 8h45, 8h45min, 8h (hora cheia).
var reHoraVisita = regexp.MustCompile(`(?i)(\d{1,2})\s*(?::|h)\s*(\d{2})?`)

// NormalizarQuando converte o `quando` do agendamento de visita em
// (hora HH:MM, turno). Aceita "08:45", "8:45", "8h45", "8h", "9 horas" e os
// turnos Manhã/Tarde/Noite com caixa/acento variáveis (inclusive junto:
// "manhã 8:45" vira hora). Texto sem hora nem turno conhecido volta vazio —
// quem chama decide erro ou preservação do valor atual.
func NormalizarQuando(quando string) (hora, turno string) {
	s := strings.TrimSpace(quando)
	if s == "" {
		return "", ""
	}
	norm := normalizar(s) // sem acentos, minúsculo
	for _, t := range TurnosVisitaValidos {
		if strings.Contains(norm, normalizar(t)) {
			turno = t
			break
		}
	}
	if m := reHoraVisita.FindStringSubmatch(s); m != nil {
		h, min := atoiVisita(m[1]), atoiVisita(m[2])
		if m[2] == "" {
			min = 0
		}
		if h >= 0 && h <= 23 && min >= 0 && min <= 59 {
			hora = fmt.Sprintf("%02d:%02d", h, min)
			turno = "" // hora específica vence o turno citado junto
		}
	}
	if hora == "" && turno == "" {
		// não é hora nem turno: devolve como veio para o chamador decidir
		return "", s
	}
	return hora, turno
}

func atoiVisita(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// reDiaVisita reconhece o dia relativo do "quando" do agendamento local
// ("hoje a tarde", "amanhã 8:45"); casa no texto normalizado (sem acento)
// porque \b do RE2 é só ASCII — "amanhã\b" nunca casaria. O mais longo
// vence ("depois de amanha").
var reDiaVisita = regexp.MustCompile(`\b(depois de amanha|amanha|hoje)\b`)

// ParseQuandoVisita converte o "quando" livre ("hoje a tarde", "amanhã
// 8:45") em (data AAAA-MM-DD, hora HH:MM, turno). Dia não citado = hoje;
// hora/turno seguem NormalizarQuando — texto sem hora nem turno conhecido
// vira só a data de hoje.
func ParseQuandoVisita(quando string) (data, hora, turno string) {
	s := strings.TrimSpace(quando)
	if s == "" {
		return "", "", ""
	}
	dias := 0
	if m := reDiaVisita.FindString(normalizar(s)); m != "" {
		switch {
		case strings.HasPrefix(m, "depois"):
			dias = 2
		case strings.HasPrefix(m, "amanh"):
			dias = 1
		}
	}
	data = time.Now().AddDate(0, 0, dias).Format("2006-01-02")
	hora, turno = NormalizarQuando(s)
	// NormalizarQuando devolve o texto cru quando não casa hora nem turno:
	// aqui isso não é turno — vira vazio (data sozinha já agenda).
	if hora == "" && !TurnoVisitaValido(turno) {
		turno = ""
	}
	return data, hora, turno
}

// DescreverQuandoVisita monta o texto amigável do agendamento p/ resposta do
// chat ("2026-11-10 à tarde", "2026-11-11 às 08:45").
func DescreverQuandoVisita(data, hora, turno string) string {
	s := data
	if hora != "" {
		s += " às " + hora
	} else if turno != "" {
		s += " (" + strings.ToLower(turno) + ")"
	}
	return s
}

// EhColunaVisitaRealizada reconhece a coluna de destino que marca a visita
// como realizada — "Visita Realizada" e variações de caixa/acentos/plural.
// Mover o card com visita agendada para ela marca a visita como realizada.
func EhColunaVisitaRealizada(coluna string) bool {
	return strings.Contains(normalizar(coluna), "visita realiz")
}

// ValidarVisita confere o agendamento de visita na borda. Tudo vazio é
// válido (limpa o agendamento); preenchido, data/hora seguem formato fixo,
// turno sai da lista e o endereço respeita o limite de texto.
func ValidarVisita(data, turno, hora, endereco string) error {
	tudoVazio := data == "" && turno == "" && hora == "" && strings.TrimSpace(endereco) == ""
	if tudoVazio {
		return nil
	}
	if len(data) != 10 || data[4] != '-' || data[7] != '-' {
		return ErrDataInvalida
	}
	for i, r := range data {
		if i == 4 || i == 7 {
			continue
		}
		if r < '0' || r > '9' {
			return ErrDataInvalida
		}
	}
	if hora != "" && (len(hora) != 5 || hora[2] != ':' ||
		hora[0] < '0' || hora[0] > '9' || hora[1] < '0' || hora[1] > '9' ||
		hora[3] < '0' || hora[3] > '9' || hora[4] < '0' || hora[4] > '9') {
		return ErrHoraInvalida
	}
	if turno != "" && !TurnoVisitaValido(turno) {
		return ErrTurnoInvalido
	}
	if len(strings.TrimSpace(endereco)) > MaxEndereco {
		return ErrTextoMuitoLongo
	}
	return nil
}
