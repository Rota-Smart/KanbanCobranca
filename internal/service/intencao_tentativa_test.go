package service

import "testing"

func TestParseIntencaoTentativa(t *testing.T) {
	casos := []struct {
		msg               string
		ok                bool
		alvo, canal, nota string
	}{
		{msg: "registre tentativa de contato", ok: true},
		{msg: "registre uma tentativa de contato", ok: true},
		{msg: "registra tentativa de contato do João", ok: true, alvo: "João"},
		{msg: "registre tentativa de contato do cliente 24755 por telefone", ok: true, alvo: "24755", canal: "Telefone"},
		{msg: "anote tentativa de contato via whatsapp", ok: true, canal: "WhatsApp"},
		{msg: "marque tentativa de contato do João por telefone, não atendeu", ok: true, alvo: "João", canal: "Telefone", nota: "não atendeu"},
		{msg: "registre tentativa de contato - sem sinal", ok: true, nota: "sem sinal"},
		{msg: "registre tentativa de contato obs: caiu a ligação", ok: true, nota: "caiu a ligação"},
		{msg: "/tentativa", ok: true},
		{msg: "/tentativa Maria por whatsapp", ok: true, alvo: "Maria", canal: "WhatsApp"},
		{msg: "marque a visita como realizada", ok: false},
		{msg: "quem está em Cobrança?", ok: false},
		{msg: "anote que liguei pro cliente", ok: false},
	}
	for _, c := range casos {
		in, ok := ParseIntencaoTentativa(c.msg)
		if ok != c.ok {
			t.Errorf("%q: ok=%v, queria %v (recebido %+v)", c.msg, ok, c.ok, in)
			continue
		}
		if !ok {
			continue
		}
		if in.Alvo != c.alvo {
			t.Errorf("%q: alvo=%q, queria %q", c.msg, in.Alvo, c.alvo)
		}
		if in.Canal != c.canal {
			t.Errorf("%q: canal=%q, queria %q", c.msg, in.Canal, c.canal)
		}
		if in.Nota != c.nota {
			t.Errorf("%q: nota=%q, queria %q", c.msg, in.Nota, c.nota)
		}
	}
}
