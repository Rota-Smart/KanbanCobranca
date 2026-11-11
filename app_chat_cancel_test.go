package main

import (
	"context"
	"testing"
)

// TestCancelarChat: CancelarChat dispara o cancelamento dos pedidos em
// andamento da conversa e é no-op quando não há nada em andamento.
func TestCancelarChat(t *testing.T) {
	a := newTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	a.muCancela.Lock()
	a.cancelaChat = map[int64][]*cancelaItem{7: {{fn: cancel}}}
	a.muCancela.Unlock()

	a.CancelarChat(7)
	select {
	case <-ctx.Done():
	default:
		t.Fatal("CancelarChat deveria ter cancelado o contexto da conversa 7")
	}

	a.CancelarChat(999) // sem pedido em andamento: não pode panicar
}

// TestCancelarChatSimultaneo: com dois pedidos em andamento na mesma
// conversa (IA longa + comando local rápido), o ⏹ aborta os dois.
func TestCancelarChatSimultaneo(t *testing.T) {
	a := newTestApp(t)
	ctx1, c1 := context.WithCancel(context.Background())
	ctx2, c2 := context.WithCancel(context.Background())
	a.muCancela.Lock()
	a.cancelaChat = map[int64][]*cancelaItem{7: {{fn: c1}, {fn: c2}}}
	a.muCancela.Unlock()

	a.CancelarChat(7)
	for i, ctx := range []context.Context{ctx1, ctx2} {
		select {
		case <-ctx.Done():
		default:
			t.Fatalf("CancelarChat deveria ter cancelado o pedido %d", i+1)
		}
	}
}
