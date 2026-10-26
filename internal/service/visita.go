package service

import "strings"

// TurnoVisitaValido informa se o turno existe.
func TurnoVisitaValido(t string) bool {
	for _, x := range TurnosVisitaValidos {
		if x == t {
			return true
		}
	}
	return false
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
