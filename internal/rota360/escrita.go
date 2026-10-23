// Escrita no helpdesk 360: resposta ao ticket e mudança de status.
// Os endpoints seguem o padrão da API de leitura (/api/helpdesk/tickets);
// como são operações reais sobre o sistema de produção, nada aqui roda em
// teste — qualquer ajuste de rota/campo só se confirma no primeiro uso real.
package rota360

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// envia faz uma requisição com corpo (POST/PATCH) e cookie de sessão.
// Em erro HTTP, inclui um recorte do corpo na mensagem para diagnosticar
// divergências de rota/campo sem precisar de teste na produção.
func (c *Cliente) envia(ctx context.Context, metodo, path string, corpo any) ([]byte, error) {
	if !c.Autenticado() {
		return nil, ErrSemSessao
	}
	var br io.Reader
	if corpo != nil {
		b, err := json.Marshal(corpo)
		if err != nil {
			return nil, err
		}
		br = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, c.base+path, br)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if corpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(c.cookie)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", metodo, path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return body, ErrSessaoExpirada
	}
	if resp.StatusCode >= 300 {
		recorte := strings.TrimSpace(string(body))
		if len(recorte) > 300 {
			recorte = recorte[:300]
		}
		if recorte == "" {
			return body, fmt.Errorf("%s %s: HTTP %d", metodo, path, resp.StatusCode)
		}
		return body, fmt.Errorf("%s %s: HTTP %d — %s", metodo, path, resp.StatusCode, recorte)
	}
	return body, nil
}

// ResponderTicket registra uma resposta pública no ticket (interação do
// atendente, visível ao solicitante).
func (c *Cliente) ResponderTicket(ctx context.Context, uuid, texto string) error {
	if uuid == "" {
		return fmt.Errorf("uuid do ticket vazio")
	}
	if strings.TrimSpace(texto) == "" {
		return fmt.Errorf("texto da resposta vazio")
	}
	_, err := c.envia(ctx, http.MethodPost,
		"/api/helpdesk/tickets/"+url.PathEscape(uuid)+"/interacoes",
		map[string]string{"tipo": "publica", "corpo": texto})
	return err
}

// AlterarStatusTicket muda o status do ticket (valores exatamente como a
// API do 360 os nomeia — o agente os lê do detalhe do ticket antes).
func (c *Cliente) AlterarStatusTicket(ctx context.Context, uuid, status string) error {
	if uuid == "" {
		return fmt.Errorf("uuid do ticket vazio")
	}
	if strings.TrimSpace(status) == "" {
		return fmt.Errorf("status vazio")
	}
	_, err := c.envia(ctx, http.MethodPatch,
		"/api/helpdesk/tickets/"+url.PathEscape(uuid),
		map[string]string{"status": status})
	return err
}
