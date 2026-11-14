// CRM do 360: funil de relacionamento (negociações). Leitura segue o padrão
// dos GETs do client.go; escrita (criar/mover) segue o padrão do escrita.go —
// só roda em chamada explícita do usuário, nunca em automação.
// Endpoints e campos confirmados contra o painel (v1.146.0, 14/11/2026);
// mudança do lado deles aparece como erro HTTP claro aqui.
package rota360

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// ---- Funis e etapas (GET /api/crm/funis) ----

// FunilCRM é um funil de negócios do CRM (Comercial, Relacionamento...).
type FunilCRM struct {
	ID       string `json:"id"`
	Nome     string `json:"nome"`
	Area     string `json:"area"`
	Segmento string `json:"segmento"`
	Chave    string `json:"chave"`
	Ordem    int    `json:"ordem"`
	Tipo     string `json:"tipo"`
	Situacao string `json:"situacao"`
}

// EtapaCRM é uma coluna do funil.
type EtapaCRM struct {
	ID         string `json:"id"`
	FunilID    string `json:"funil_id"`
	Nome       string `json:"nome"`
	Ordem      int    `json:"ordem"`
	Tipo       string `json:"tipo"` // "ganho", "perda" ou ""
	PrazoDias  int    `json:"prazo_dias"`
	Cor        string `json:"cor"`
}

// MotivoPerdaCRM é um motivo de perda selecionável ao perder negociação.
type MotivoPerdaCRM struct {
	ID   string `json:"id"`
	Nome string `json:"nome"`
}

// FunisCRM devolve funis + etapas + motivos de perda num pedido só.
// O painel pede ?tipo=negocio&area=comercial&segmento=b2c; sem segmento o
// 360 devolve tudo que o usuário tem escopo para ver.
func (c *Cliente) FunisCRM(ctx context.Context) ([]FunilCRM, []EtapaCRM, []MotivoPerdaCRM, error) {
	var out struct {
		Funis        []FunilCRM       `json:"funis"`
		Etapas       []EtapaCRM       `json:"etapas"`
		MotivosPerda []MotivoPerdaCRM `json:"motivos_perda"`
	}
	if err := c.get(ctx, "/api/crm/funis?tipo=negocio", &out); err != nil {
		return nil, nil, nil, err
	}
	return out.Funis, out.Etapas, out.MotivosPerda, nil
}

// ---- Negociações (GET /api/crm/negociacoes) ----

// EnderecoCRM é o endereço embutido na negociação.
type EnderecoCRM struct {
	Logradouro string `json:"logradouro"`
	Numero     string `json:"numero"`
	Bairro     string `json:"bairro"`
	Cidade     string `json:"cidade"`
	UF         string `json:"uf"`
	CEP        string `json:"cep"`
}

// VisitaCRM é o resumo de visita do card no CRM (quando existe): data,
// status (agendada/vencida/realizada) e estado da agenda do SDR.
type VisitaCRM struct {
	Data         string `json:"data"`
	Status       string `json:"status"`
	DiasVencida  int    `json:"dias_vencida"`
	EstadoAgenda string `json:"estado_agenda"`
	Origem       string `json:"origem"`
}

// NegociacaoCRM é um card do funil. Campos essenciais para o espelho; o que
// não interessa ao app é omitido de propósito (o 360 devolve muito mais).
type NegociacaoCRM struct {
	ID             string     `json:"id"`
	FunilID        string     `json:"funil_id"`
	FunilNome      string     `json:"funil_nome"`
	EtapaID        string     `json:"etapa_id"`
	EtapaNome      string     `json:"etapa_nome"`
	EtapaOrdem     int        `json:"etapa_ordem"`
	Titulo         string     `json:"titulo"`
	ClienteNome    string     `json:"cliente_nome"`
	Document       *string    `json:"documento"`
	Telefone       string     `json:"telefone_norm"`
	Endereco       *EnderecoCRM `json:"endereco"`
	UnidadeNome    string     `json:"unidade_nome"`
	ResponsavelID  string     `json:"responsavel_id"`
	Responsavel    string     `json:"responsavel_nome"`
	Status         string     `json:"status"`
	Saude          string     `json:"saude"` // verde/amarelo/vermelho
	ValorMensal    *float64   `json:"valor_mensal"`
	ValorPontual   *float64   `json:"valor_pontual"`
	DiasNaEtapa    int        `json:"dias_na_etapa"`
	DiasSemIntera  int        `json:"dias_sem_interacao"`
	TicketNumero   *string    `json:"ticket_numero"`
	OrigemNegocia  string     `json:"origem_negociacao"` // "ticket", "lead", ...
	CodCliente     *int64     `json:"cod_cliente_service"`
	CodClienteBase *int64     `json:"cod_cliente_base"`
	ExigeAcao      *struct {
		Motivos []string `json:"motivos"`
	} `json:"exige_acao"`
	Esfriando       bool    `json:"esfriando"`
	PausadaEm       *string `json:"pausada_em"`
	RetornoEm       *string `json:"retorno_em"`
	ProximaTarefa   *string `json:"proxima_tarefa"`
	FechamentoPrev  *string `json:"fechamento_previsto"`
	AtualizadoEm    string  `json:"atualizado_em"`
	AtividadeUltima *string `json:"ultima_atividade_em"`
	Visita          *VisitaCRM `json:"visita"`
}

// EnderecoTexto monta o endereço do card em uma linha (para o kanban).
func (n *NegociacaoCRM) EnderecoTexto() string {
	if n.Endereco == nil {
		return ""
	}
	parts := []string{}
	for _, p := range []string{n.Endereco.Logradouro + " " + n.Endereco.Numero, n.Endereco.Bairro, n.Endereco.Cidade} {
		if s := strings.TrimSpace(p); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// PaginaNegociacoes é o envelope do GET /api/crm/negociacoes.
type PaginaNegociacoes struct {
	Itens  []NegociacaoCRM `json:"itens"`
	Total  int             `json:"total"`
	Limite int             `json:"limite"`
	Offset int             `json:"offset"`
}

// NegociacoesCRM lista cards, opcionalmente por funil e status.
// status: "aberta" (padrão), "pausada", "ganha", "perdida", "todas".
// funilID vazio devolve tudo dentro do escopo do usuário.
func (c *Cliente) NegociacoesCRM(ctx context.Context, funilID, status string) (*PaginaNegociacoes, error) {
	q := url.Values{}
	if funilID != "" {
		q.Set("funil_id", funilID)
	}
	if status == "" {
		status = "aberta"
	}
	q.Set("status", status)
	q.Set("limit", "500")
	var out PaginaNegociacoes
	if err := c.get(ctx, "/api/crm/negociacoes?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---- Card do ticket (GET /api/helpdesk/tickets/<uuid>/card-crm) ----

// CardCrmTicket devolve a negociação que o 360 criou a partir do ticket.
// Corpo cru (RawMessage) porque o formato pode trazer negociação, null
// (ticket sem card) ou erro de escopo — quem chama decide.
func (c *Cliente) CardCrmTicket(ctx context.Context, uuid string) ([]byte, bool, error) {
	if !c.Autenticado() {
		return nil, false, ErrSemSessao
	}
	corpo, err := c.envia(ctx, "GET", "/api/helpdesk/tickets/"+url.PathEscape(uuid)+"/card-crm", nil)
	if err != nil {
		return nil, false, err
	}
	vazio := corpo == nil || strings.TrimSpace(string(corpo)) == "" ||
		strings.TrimSpace(string(corpo)) == "null" ||
		strings.TrimSpace(string(corpo)) == "{}"
	return corpo, !vazio, nil
}

// ---- Escrita: criar e mover (POST) ----

// CriarNegociacao cria um card no funil. Os campos seguem o formulário do
// painel; clienteNome e funilID são obrigatórios, o resto opcional.
func (c *Cliente) CriarNegociacao(ctx context.Context, corpo map[string]any) (*NegociacaoCRM, error) {
	if corpo == nil {
		return nil, fmt.Errorf("corpo vazio")
	}
	if funil, _ := corpo["funil_id"].(string); strings.TrimSpace(funil) == "" {
		return nil, fmt.Errorf("funil_id obrigatório para criar negociação")
	}
	saida, err := c.envia(ctx, "POST", "/api/crm/negociacoes", corpo)
	if err != nil {
		return nil, err
	}
	var n NegociacaoCRM
	if err := json.Unmarshal(bytes.TrimSpace(saida), &n); err != nil {
		// o 360 devolve o card criado; se mudar o formato, o erro mostra o corpo
		return nil, fmt.Errorf("resposta inesperada ao criar: %w", err)
	}
	return &n, nil
}

// MoverNegociacao troca o card de etapa (arrastar no painel do 360).
func (c *Cliente) MoverNegociacao(ctx context.Context, id, etapaID string) error {
	if id == "" || etapaID == "" {
		return fmt.Errorf("id da negociação e etapa são obrigatórios")
	}
	_, err := c.envia(ctx, "POST",
		"/api/crm/negociacoes/"+url.PathEscape(id)+"/mover",
		map[string]string{"para_etapa_id": etapaID})
	return err
}
