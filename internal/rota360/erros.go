package rota360

import "errors"

var (
	// ErrSemSessao: nenhum cookie de sessão configurado ainda.
	ErrSemSessao = errors.New("sessão do 360 não configurada — conecte-se nas configurações")
	// ErrSessaoExpirada: o 360 rejeitou o cookie (401/403) — renovar sessão.
	ErrSessaoExpirada = errors.New("sessão do 360 expirada — reconecte-se nas configurações")
)
