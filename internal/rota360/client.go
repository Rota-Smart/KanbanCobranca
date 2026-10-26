// Package rota360 conversa com o portal 360.rotagroup.com.br usando a
// sessão do usuário conectado (cookie detectado no Chrome ou informado
// nas configurações). Nenhuma credencial nova é criada por aqui.
package rota360

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const BaseURL = "https://360.rotagroup.com.br"

// Cliente chama a API do 360 carregando o cookie de sessão.
type Cliente struct {
	base   string
	http   *http.Client
	cookie *http.Cookie // cookie de sessão (nome=valor)
}

// NovoCliente cria o cliente a partir do cookie de sessão do 360.
func NovoCliente(cookie *Cookie) *Cliente {
	var hc *http.Cookie
	if cookie != nil {
		hc = &http.Cookie{Name: cookie.Nome, Value: cookie.Valor}
	}
	return &Cliente{
		base:   BaseURL,
		http:   &http.Client{Timeout: 12 * time.Second},
		cookie: hc,
	}
}

// Autenticado informa se há sessão para usar.
func (c *Cliente) Autenticado() bool { return c != nil && c.cookie != nil && c.cookie.Value != "" }

// CookieSalva devolve o cookie da sessão, para persistência.
func (c *Cliente) CookieSalva() *Cookie {
	if !c.Autenticado() {
		return nil
	}
	return &Cookie{Nome: c.cookie.Name, Valor: c.cookie.Value}
}

// get faz GET com o cookie, 1 retry em rede/5xx/429, resposta limitada.
func (c *Cliente) get(ctx context.Context, path string, out any) error {
	if !c.Autenticado() {
		return ErrSemSessao
	}
	var tentou bool
	for i := 0; i < 2; i++ {
		if tentou {
			time.Sleep(time.Second)
		}
		tentou = true
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		req.AddCookie(c.cookie)
		resp, err := c.http.Do(req)
		if err != nil {
			err = fmt.Errorf("requisição %s: %w", path, err)
			if i == 0 {
				continue
			}
			return err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			if i == 0 {
				continue
			}
			return fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return ErrSessaoExpirada
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
		}
		if err != nil {
			return err
		}
		return json.Unmarshal(body, out)
	}
	return nil
}

// ---- Resultado da busca de clientes (/api/cliente/buscar) ----

// ResumoCliente é um resultado da busca por código/nome.
type ResumoCliente struct {
	CodCliente  int64  `json:"CodCliente"`
	Nome        string `json:"Nome"`
	CGCCPF      string `json:"CGCCPF"`
	Fone1       string `json:"Fone1"`
	Celular     string `json:"Celular"`
	Cidade      string `json:"Cidade"`
	Estado      string `json:"Estado"`
	Endereco    string `json:"Endereco"`
	NumEndereco string `json:"NumEndereco"`
	Situacao    string `json:"Situacao"`
}

// BuscarClientes pesquisa no 360 por código, nome, CPF/CNPJ etc.
func (c *Cliente) BuscarClientes(ctx context.Context, q, filtro string) ([]ResumoCliente, error) {
	if filtro == "" {
		filtro = "todos"
	}
	var raw []ResumoCliente
	err := c.get(ctx, "/api/cliente/buscar?q="+url.QueryEscape(q)+"&filtro="+url.QueryEscape(filtro), &raw)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// ---- Financeiro (/api/cliente/<cod>/dashboard360) ----

// Fatura360 é uma fatura em aberto do cliente.
type Fatura360 struct {
	NumeroDoc      string    `json:"NumeroDoc"`
	Vencimento     time.Time `json:"Vencimento"`
	ValorDuplicata float64   `json:"ValorDuplicata"`
	MultaValor     float64   `json:"MultaValor"`
	MoraValor      float64   `json:"MoraValor"`
	DiasAtraso     int       `json:"DiasAtraso"`
	Situacao       string    `json:"Situacao"`
	BoletoLinha    string    `json:"BoletoLinhaDigitavel"`
}

// Financeiro360 resume o bloco financeiro do dashboard.
type Financeiro360 struct {
	TotalDevendo            float64 `json:"TotalDevendo"`
	TotalDevendoComEncargos float64 `json:"TotalDevendoComEncargos"`
	TotalEncargosAtraso     float64 `json:"TotalEncargosAtraso"`
	TotalAtrasadas          int     `json:"TotalAtrasadas"`
}

// TicketHistorico360 é um atendimento recente do cliente (Octadesk).
type TicketHistorico360 struct {
	Numero    string `json:"number"`
	Summary   string `json:"summary"`
	Status    string `json:"status"` // preenchido do objeto aninhado no scan
	CreatedAt string `json:"createdAt"`
}

// Produto360 é um produto/linha de contrato do cliente.
type Produto360 struct {
	CodProduto           int64     `json:"codProduto"`
	Descricao            string    `json:"descricao"`
	Quantidade           int       `json:"quantidadeAtual"`
	Tipo                 string    `json:"tipo"` // Locado | Vendido | Brinde...
	ValorTotal           float64   `json:"valorTotal"`
	PrimeiraMovimentacao time.Time `json:"primeiraMovimentacao"`
}

// Dashboard360 é o recorte do que o app consome do dashboard do cliente.
type Dashboard360 struct {
	Sources struct {
		InsideService struct {
			Data struct {
				Dados struct {
					CodCliente int64  `json:"CodCliente"`
					Nome       string `json:"Nome"`
					CGCCPF     string `json:"CGCCPF"`
					Fone1      string `json:"Fone1"`
					Endereco   string `json:"Endereço"`
					NumCasa    string `json:"NumCasa"`
					Cidade     string `json:"Cidade"`
					Estado     string `json:"Estado"`
					Situacao   string `json:"Situacao"`
				} `json:"dados"`
				// OS ativas do Service (ordens de serviço em aberto no cadastro).
				OSAtivas       []map[string]any `json:"osAtivas"`
				FaturasAbertas []Fatura360      `json:"faturasAbertas"`
				Financeiro     Financeiro360    `json:"financeiro"`
			} `json:"data"`
		} `json:"inside_service"`
		Octadesk struct {
			Data struct {
				TicketsRecentes []struct {
					Number    string `json:"number"`
					Summary   string `json:"summary"`
					CreatedAt string `json:"createdAt"`
					Status    struct {
						Name string `json:"name"`
					} `json:"status"`
				} `json:"tickets_recentes"`
			} `json:"data"`
		} `json:"octadesk"`
		Produtos struct {
			Data []Produto360 `json:"data"`
		} `json:"produtos"`
	} `json:"sources"`
}

// FinanceiroCliente busca o dashboard completo do cliente por código service.
func (c *Cliente) FinanceiroCliente(ctx context.Context, codService string) (*Dashboard360, error) {
	if _, err := strconv.ParseInt(codService, 10, 64); err != nil {
		return nil, fmt.Errorf("código service inválido: %s", codService)
	}
	var d Dashboard360
	if err := c.get(ctx, "/api/cliente/"+codService+"/dashboard360", &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// ---- Tickets da fila (/api/helpdesk/tickets) ----

// Ticket360 é um item da fila de tickets.
type Ticket360 struct {
	ID            string `json:"id"`
	Numero        string `json:"numero"`
	Titulo        string `json:"titulo"`
	Status        string `json:"status"`
	AssuntoNome   string `json:"assunto_nome"`
	CodigoService string `json:"codigo_service"`
	Solicitante   string `json:"solicitante_nome"`
	Responsavel   string `json:"responsavel_nome"`
	CanalOrigem   string `json:"canal_origem"`
	CriadoEm      string `json:"criado_em"`
}

// TicketsDaFila lista a fila de tickets (views: em_aberto, meus_em_aberto...).
func (c *Cliente) TicketsDaFila(ctx context.Context, view string, page, limit int) ([]Ticket360, int, error) {
	if view == "" {
		view = "em_aberto"
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 25
	}
	var out struct {
		Total int         `json:"total"`
		Itens []Ticket360 `json:"itens"`
	}
	err := c.get(ctx, fmt.Sprintf("/api/helpdesk/tickets?view=%s&page=%d&limit=%d", url.QueryEscape(view), page, limit), &out)
	if err != nil {
		return nil, 0, err
	}
	return out.Itens, out.Total, nil
}

// maxResultadosPesquisaTickets limita a busca para não estourar o tool result.
const maxResultadosPesquisaTickets = 20

// PesquisarTickets procura na fila do helpdesk (leitura) tickets que casem
// com o termo: número do ticket (aceita com ou sem "#") ou título (nome do
// ticket) — o app busca em todas as views em aberto e filtra localmente,
// sem depender de endpoint de busca do portal. Código service de cliente NÃO
// entra no casamento: a pesquisa é de ticket, não de cliente.
func (c *Cliente) PesquisarTickets(ctx context.Context, q string) ([]Ticket360, error) {
	q = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(q), "#")))
	if q == "" {
		return []Ticket360{}, nil
	}
	vistos := map[string]bool{}
	var out []Ticket360
	for _, view := range []string{"em_aberto", "meus_em_aberto", "pendentes"} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		itens, _, err := c.TicketsDaFila(ctx, view, 1, 100)
		if err != nil {
			continue // uma view falha (ex.: sem permissão) — segue com as outras
		}
		for _, t := range itens {
			if vistos[t.ID] {
				continue
			}
			if strings.Contains(strings.ToLower(t.Numero), q) ||
				strings.Contains(strings.ToLower(t.Titulo), q) {
				vistos[t.ID] = true
				out = append(out, t)
				if len(out) >= maxResultadosPesquisaTickets {
					return out, nil
				}
			}
		}
	}
	return out, nil
}

// OSItem360 é uma ordem de serviço do cliente no os-funil — inclui as
// concluídas/fechadas, que o dashboard360 (osAtivas) não expõe.
type OSItem360 struct {
	Ordem          int64      `json:"Ordem"`
	Status         string     `json:"Status"` // A aberta | B baixada | F fechada | C cancelada
	Tipo           string     `json:"Tipo"`   // M corretiva | I interna | P preventiva | V instalação | A ampliação | R retirada
	DefeitoSigma   string     `json:"DefeitoSigma"`
	Descricao      string     `json:"Descricao"`
	DataAbertura   *time.Time `json:"DataAbertura"`
	DataFechamento *time.Time `json:"DataFechamento"`
	Tecnico        string     `json:"Tecnico"`
}

// OSFunil360 é o resultado de /api/cliente/<cod>/os-funil.
type OSFunil360 struct {
	Itens    []OSItem360 `json:"itens"`
	Contagem struct {
		Concluida              int `json:"concluida"`
		Cancelada              int `json:"cancelada"`
		FechadaSemProcessam    int `json:"fechada_sem_processamento"`
		AtendidaSemFechamento  int `json:"atendida_sem_fechamento"`
		SemAtendimento         int `json:"sem_atendimento"`
	} `json:"contagem"`
}

// OSFunil lista as ordens de serviço do cliente (todas as situações,
// mais recentes primeiro — o portal limita a ~50).
func (c *Cliente) OSFunil(ctx context.Context, codService string) (*OSFunil360, error) {
	if _, err := strconv.ParseInt(codService, 10, 64); err != nil {
		return nil, fmt.Errorf("código service inválido: %s", codService)
	}
	var out OSFunil360
	if err := c.get(ctx, "/api/cliente/"+codService+"/os-funil", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// StatusOS traduz o código de situação do ERP do 360 (os.go do portal:
// A aberta, B baixada, F fechada, C cancelada).
func StatusOS(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "A":
		return "aberta"
	case "B", "F":
		return "concluída"
	case "C":
		return "cancelada"
	case "":
		return "sem status"
	default:
		return "status " + s
	}
}

// TipoOS traduz o código do tipo de O.S. (M/I/P/V/A/R).
func TipoOS(t string) string {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case "M":
		return "corretiva"
	case "I":
		return "interna"
	case "P":
		return "preventiva"
	case "V":
		return "instalação"
	case "A":
		return "ampliação"
	case "R":
		return "retirada"
	default:
		return t
	}
}

// ServicoContratado360 é um serviço do cadastro (ex.: "MONITORAMENTO DE
// ALARME", "CÂMERA WIFI EXTERNA - GRÁTIS").
type ServicoContratado360 struct {
	Descricao  string  `json:"descricaoServico"`
	Valor      float64 `json:"valor"`
	Quantidade int     `json:"quantidade"`
}

// FaturamentoCadastro360 é o bloco de faturamento do cadastro — a fonte
// oficial da mensalidade e do dia de vencimento.
type FaturamentoCadastro360 struct {
	ValorNF       float64 `json:"valorNF"`
	DiaVencimento int     `json:"diaVencimento"`
}

// DadosCliente360 é o cadastro completo do cliente (Consulta Clientes →
// Cadastro Geral), incluindo as observações do cadastro (textos datados
// com histórico de ampliações, combinados e faturamentos).
type DadosCliente360 struct {
	Nome         string                 `json:"nome"`
	Situacao     string                 `json:"situacao"`
	Observacoes  string                 `json:"observacoes"`
	AvisosAtivos any                    `json:"avisosAtivos"`
	Servicos     []ServicoContratado360 `json:"servicos"`
	Faturamento  FaturamentoCadastro360 `json:"faturamento"`
	AtivoDesde   time.Time              `json:"ativoDesde"`
}

// DadosCliente busca o cadastro completo do cliente por código service.
func (c *Cliente) DadosCliente(ctx context.Context, codService string) (*DadosCliente360, error) {
	if _, err := strconv.ParseInt(codService, 10, 64); err != nil {
		return nil, fmt.Errorf("código service inválido: %s", codService)
	}
	var d DadosCliente360
	if err := c.get(ctx, "/api/cliente/"+codService+"/dados", &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// Contrato360 é um contrato do cliente (Consulta Clientes → aba Contratos) —
// fonte da mensalidade exata, do dia de vencimento e da vigência.
type Contrato360 struct {
	Nome            string    `json:"Nome"`
	DataAssinatura  time.Time `json:"DataAssinatura"`
	TerminoContrato time.Time `json:"TérminoContrato"`
	DiaVcto         int       `json:"DiaVcto"`
	Prazo           int       `json:"Prazo"` // meses de vigência
	Situacao        string    `json:"Situacao"`
	ValorME         float64   `json:"ValorME"` // mensalidade
	ValorNF         float64   `json:"ValorNF"`
	Internet        bool      `json:"Internet"`
}

// ContratosCliente lista os contratos do cliente por código service.
func (c *Cliente) ContratosCliente(ctx context.Context, codService string) ([]Contrato360, error) {
	if _, err := strconv.ParseInt(codService, 10, 64); err != nil {
		return nil, fmt.Errorf("código service inválido: %s", codService)
	}
	var out []Contrato360
	if err := c.get(ctx, "/api/cliente/"+codService+"/contratos", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ---- Detalhe do ticket (GET /api/helpdesk/tickets/<uuid>) ----

// MensagemTicket360 é uma interação do ticket (descrição, resposta pública,
// anotação interna ou mensagem de sistema).
type MensagemTicket360 struct {
	Tipo     string `json:"tipo"` // "descricao" | "publica" | "interna" | "sistema"
	Autor    string `json:"autor_nome"`
	Corpo    string `json:"corpo"`
	CriadoEm string `json:"criado_em"`
}

// TicketDetalhe360 é o ticket completo, com o histórico de mensagens.
type TicketDetalhe360 struct {
	ID              string              `json:"id"`
	Numero          string              `json:"numero"`
	Titulo          string              `json:"titulo"`
	Status          string              `json:"status"`
	CodService      string              `json:"codigo_service"`
	Solicitante     string              `json:"solicitante_nome"`
	CategoriaNome   string              `json:"categoria_nome"`
	AssuntoNome     string              `json:"assunto_nome"`
	ResponsavelNome string              `json:"responsavel_nome"`
	ContatoNome     string              `json:"contato_nome"`
	ContatoTelefone string              `json:"contato_telefone"`
	CriadoEm        string              `json:"criado_em"`
	Mensagens       []MensagemTicket360 `json:"mensagens"`
}

// DetalheTicket busca o ticket completo (com interações) pelo uuid na URL.
func (c *Cliente) DetalheTicket(ctx context.Context, uuid string) (*TicketDetalhe360, error) {
	if uuid == "" {
		return nil, fmt.Errorf("uuid do ticket vazio")
	}
	var t TicketDetalhe360
	if err := c.get(ctx, "/api/helpdesk/tickets/"+url.PathEscape(uuid), &t); err != nil {
		return nil, err
	}
	return &t, nil
}
