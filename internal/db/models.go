package db

// Ticket é um card do Kanban. A lista de colunas válidas é responsabilidade
// do pacote service; o banco não faz CHECK nas colunas.
type Ticket struct {
	ID              int64  `json:"id"`
	NomeCliente     string `json:"nomeCliente"`
	TipoTicket      string `json:"tipoTicket"`
	NumeroTicket    string `json:"numeroTicket"`
	CodService      string `json:"codService"`
	Endereco        string `json:"endereco"`
	Telefone        string `json:"telefone"`
	ColunaAtual     string `json:"colunaAtual"`
	Posicao         int    `json:"posicao"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
	Ticket360ID     string `json:"ticket360Id"`     // uuid do ticket no helpdesk 360 (vazio = sem vínculo)
	Ticket360Numero string `json:"ticket360Numero"` // número exibido no 360 (ex.: "12345")
	Observacoes     string `json:"observacoes"`     // observações internas do card
	VisitaData      string `json:"visitaData"`      // agendamento de visita (yyyy-mm-dd, vazio = sem visita)
	VisitaTurno     string `json:"visitaTurno"`     // Manhã | Tarde | Noite
	VisitaHora      string `json:"visitaHora"`      // horário livre opcional (HH:MM)
	VisitaEndereco  string `json:"visitaEndereco"`  // endereço da visita; base do link do Google Maps
}

// Vinculo360 é um ticket do helpdesk 360 ligado a um card — um card pode ter
// vários. O assunto alimenta a aba Cliente; o uuid abre o ticket no navegador.
type Vinculo360 struct {
	ID              int64  `json:"id"`
	TicketID        int64  `json:"ticketId"`
	Ticket360ID     string `json:"ticket360Id"`
	Ticket360Numero string `json:"ticket360Numero"`
	Assunto         string `json:"assunto"`
	CriadoEm        string `json:"criadoEm"`
}

// Cobranca é um acordo de reparcelamento; valores em int64 centavos.
// Os campos derivados (parcelado, última, final) são snapshot do acordo.
type Cobranca struct {
	ID                 int64  `json:"id"`
	TicketID           int64  `json:"ticketId"`
	DataContrato       string `json:"dataContrato"`
	DataVencimento     string `json:"dataVencimento"`
	TotalAbertoCents   int64  `json:"totalAbertoCents"`
	EntradaCents       int64  `json:"entradaCents"`
	NumeroVezes        int    `json:"numeroVezes"`
	ParcelaBaseCents   int64  `json:"parcelaBaseCents"`
	ParceladoCents     int64  `json:"parceladoCents"`
	UltimaParcelaCents int64  `json:"ultimaParcelaCents"`
	FinalCents         int64  `json:"finalCents"`
	Status             string `json:"status"`
	Ativa              bool   `json:"ativa"`
	VersaoRegra        int    `json:"versaoRegra"`
}

// Cliente360 é o cache local do cadastro buscado no 360.rotagroup.com.br.
// Os blocos brutos (faturas, score, histórico) ficam em JSON para a
// avaliação e a proposta consumirem sem re-buscar na rede.
type Cliente360 struct {
	CodService    string `json:"codService"`
	Nome          string `json:"nome"`
	CNPJ          string `json:"cnpj"`
	Telefone      string `json:"telefone"`
	Endereco      string `json:"endereco"`
	Unidade       string `json:"unidade"`
	SaldoCents    int64  `json:"saldoCents"`
	FaturasJSON   string `json:"faturasJson"`
	ScoreJSON     string `json:"scoreJson"`
	HistoricoJSON string `json:"historicoJson"`
	ContratosJSON string `json:"contratosJson"`
	SyncedAt      string `json:"syncedAt"`
}

// ParametroNegociacao é uma regra de negociação — digitada pelo usuário
// ou extraída de arquivo anexado (txt/docx/pdf).
type ParametroNegociacao struct {
	ID          int64  `json:"id"`
	Fonte       string `json:"fonte"` // "texto" | "arquivo"
	NomeArquivo string `json:"nomeArquivo"`
	Conteudo    string `json:"conteudo"`
	CreatedAt   string `json:"createdAt"`
}
