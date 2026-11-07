package main

import (
	"context"
	"testing"
)

// TestCancelarChat: CancelarChat dispara o cancelamento do pedido em
// andamento da conversa e é no-op quando não há nada em andamento.
func TestCancelarChat(t *testing.T) {
	a := newTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	a.muCancela.Lock()
	a.cancelaChat = map[int64]context.CancelFunc{7: cancel}
	a.muCancela.Unlock()

	a.CancelarChat(7)
	select {
	case <-ctx.Done():
	default:
		t.Fatal("CancelarChat deveria ter cancelado o contexto da conversa 7")
	}

	a.CancelarChat(999) // sem pedido em andamento: não pode panicar
}
