package main

// Lista colada de cancelamentos ("NOME - MOTIVO - CANCELADO").

// Regra de vínculo (o que manda é o ticket, não o nome):
//   - card do cliente COM ticket de cancelamento vinculado → mantém o card;
//   - card do cliente SEM vínculo de cancelamento → cria um SEGUNDO card
//     importado do 360 e vincula o ticket de cancelamento nele;
//   - sem card nenhum → importa do 360 e vincula, igual acima.

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"rota-smart/internal/db"
	"rota-smart/internal/rota360"
	"rota-smart/internal/service"
)

// LinhaImportCancel é uma linha da lista já interpretada e resolvida.
type LinhaImportCancel struct {
	Linha            int      `json:"linha"`
	Nome             string   `json:"nome"`
	Motivo           string   `json:"motivo"`
	Resultado        string   `json:"resultado"`
	Acao             string   `json:"acao"` // manter | criar
	TicketID         int64    `json:"ticketId"`
	CodService       string   `json:"codService"`
	Coluna           string   `json:"coluna"`
	Ticket360Numero  string   `json:"ticket360"`
	Ticket360UUID    string   `json:"ticket360Uuid"`
	Ticket360Assunto string   `json:"ticket360Assunto"`
	Candidatos       []string `json:"candidatos"`
}

// PreviaImportCancel é o que o modal mostra antes de gravar qualquer coisa.
type PreviaImportCancel struct {
	Cancelados  []LinhaImportCancel `json:"cancelados"`
	Revertidos  []LinhaImportCancel `json:"revertidos"`
	Tratamento  []LinhaImportCancel `json:"tratamento"`
	Ambiguos    []LinhaImportCancel `json:"ambiguos"`
	SemCard     []LinhaImportCancel `json:"semCard"` // 360 não respondeu / cliente não achado
	Erros       []string            `json:"erros"`
	TotalLinhas int                 `json:"totalLinhas"`
}

// ResumoImportCancel é o resultado do import de verdade.
type ResumoImportCancel struct {
	Registrados int      `json:"registrados"`
	Movidos     int      `json:"movidos"`
	Importados  int      `json:"importados"` // card criado do 360
	Vinculados  int      `json:"vinculados"` // ticket de cancelamento vinculado ao card novo
	JaFechados  int      `json:"jaFechados"` // card já estava na coluna de destino
	Falhas      []string `json:"falhas"`
}

// cardComCancelamento devolve o card (entre os candidatos) que tem ticket de
// assunto "Cancelamento" vinculado — é ele que recebe o registro. Mais de um
// card assim é ambíguo (não mexe).
func (a *App) cardComCancelamento(candidatos []service.CardResumo) (service.CardResumo, bool) {
	var achados []service.CardResumo
	for _, c := range candidatos {
		if a.vinculos == nil {
			continue
		}
		vs, err := a.vinculos.List(a.ctx, c.ID)
		if err != nil {
			continue
		}
		for _, v := range vs {
			if service.AssuntoCancelamento(v.Assunto) {
				achados = append(achados, c)
				break
			}
		}
	}
	if len(achados) == 1 {
		return achados[0], true
	}
	return service.CardResumo{}, false
}

// cardSemVinculoComCod devolve o candidato que não tem vínculo NENHUM e cujo
// cod service casa (ou está vazio) — card solto de import anterior.
func (a *App) cardSemVinculoComCod(candidatos []service.CardResumo, cod string) (service.CardResumo, bool) {
	if a.vinculos == nil {
		return service.CardResumo{}, false
	}
	var achados []service.CardResumo
	for _, c := range candidatos {
		if cod != "" && c.CodService != "" && c.CodService != cod {
			continue
		}
		vs, err := a.vinculos.List(a.ctx, c.ID)
		if err == nil && len(vs) == 0 {
			achados = append(achados, c)
		}
	}
	if len(achados) == 1 {
		return achados[0], true
	}
	return service.CardResumo{}, false
}

// ticketCancelamentoDo360 procura na fila de tickets abertos o ticket de
// cancelamento do cliente (assunto "cancelamento"; se o cod service é
// conhecido, exige que bata). Pega SEMPRE o ticket mais recente (maior ID / CriadoEm).
// ok=false = não achou.
func (a *App) ticketCancelamentoDo360(ctx context.Context, nome, cod string) (rota360.Ticket360, bool) {
	c := a.rotaAtual()
	if c == nil {
		return rota360.Ticket360{}, false
	}
	ts, err := c.PesquisarTickets(ctx, nome)
	if err != nil {
		return rota360.Ticket360{}, false
	}
	var maisRecente rota360.Ticket360
	achou := false
	for _, t := range ts {
		if cod != "" && t.CodigoService != "" && t.CodigoService != cod {
			continue
		}
		if service.AssuntoCancelamento(t.AssuntoNome) {
			if !achou {
				maisRecente = t
				achou = true
			} else {
				// Compara por CriadoEm ou pelo número do ticket (maior número = mais novo)
				if t.CriadoEm > maisRecente.CriadoEm || (t.CriadoEm == maisRecente.CriadoEm && t.Numero > maisRecente.Numero) {
					maisRecente = t
				}
			}
		}
	}
	return maisRecente, achou
}

// previaImportar interpreta o texto e resolve cada linha pela regra do
// ticket de cancelamento (sem gravar nada) — a prévia e o import usam a
// mesma leitura. As linhas que vão precisar criar card consultam o 360
// (código service + ticket de cancelamento) aqui, para a prévia mostrar
// exatamente o que vai acontecer.
func (a *App) previaImportar(texto string) (*PreviaImportCancel, []service.LinhaCancelamento, error) {
	if a.cancelamentos == nil {
		return nil, nil, fmt.Errorf("banco indisponível nesta instalação")
	}
	linhas, erros := service.ParseListaCancelamentos(texto)
	p := &PreviaImportCancel{
		Cancelados: []LinhaImportCancel{},
		Revertidos: []LinhaImportCancel{},
		Tratamento: []LinhaImportCancel{},
		Ambiguos:   []LinhaImportCancel{},
		SemCard:    []LinhaImportCancel{},
		Erros:      erros,
	}
	if len(linhas) == 0 {
		return p, linhas, nil
	}
	ts, err := a.tickets.List(a.ctx)
	if err != nil {
		return nil, nil, friendly("PreviaImportCancelamentos", err)
	}
	cards := make([]service.CardResumo, 0, len(ts))
	for _, t := range ts {
		cards = append(cards, service.CardResumo{ID: t.ID, NomeCliente: t.NomeCliente, CodService: t.CodService, ColunaAtual: t.ColunaAtual})
	}
	p.TotalLinhas = len(linhas)
	for _, l := range linhas {
		x := LinhaImportCancel{Linha: l.Linha, Nome: l.Nome, Motivo: l.Motivo, Resultado: l.Resultado}
		candidatos := service.CasarCardsPorNomeLista(l.Nome, cards)
		alvo, tem := a.cardComCancelamento(candidatos)
		if tem {
			x.Acao = "manter"
			x.TicketID, x.CodService, x.Coluna = alvo.ID, alvo.CodService, alvo.ColunaAtual
		} else {
			// Nenhum candidato tem ticket de cancelamento: cria um segundo
			// card (mesmo que já exista card do cliente). Consulta o 360.
			x.Acao = "criar"
			for _, c := range candidatos {
				x.Candidatos = append(x.Candidatos, c.NomeCliente)
			}
			ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
			cod := l.CodService
			var err error
			if cod == "" {
				cod, err = a.codServiceDo360(ctx, l.Nome)
			}
			if err != nil || cod == "" {
				cancel()
				p.SemCard = append(p.SemCard, x)
				continue
			}
			x.CodService = cod
			if t360, ok := a.ticketCancelamentoDo360(ctx, l.Nome, cod); ok {
				x.Ticket360Numero, x.Ticket360UUID, x.Ticket360Assunto = t360.Numero, t360.ID, t360.AssuntoNome
			}
			cancel()
			// Reimport idempotente: candidato SEM vínculo nenhum é um card
			// solto (ex.: criado por um import anterior interrompido) —
			// reusa em vez de criar um terceiro card do mesmo cliente.
			if reusavel, ok := a.cardSemVinculoComCod(candidatos, cod); ok {
				x.Acao = "manter"
				x.TicketID = reusavel.ID
				x.Coluna = reusavel.ColunaAtual
			}
		}
		switch l.Resultado {
		case service.CancelamentoPerda:
			p.Cancelados = append(p.Cancelados, x)
		case service.CancelamentoReversao:
			p.Revertidos = append(p.Revertidos, x)
		default:
			p.Tratamento = append(p.Tratamento, x)
		}
	}
	return p, linhas, nil
}

// PreviaImportarCancelamentos analisa a lista colada e devolve as listas
// para o modal mostrar — nada é gravado nem movido.
func (a *App) PreviaImportarCancelamentos(texto string) (*PreviaImportCancel, error) {
	p, _, err := a.previaImportar(texto)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// registrarCancelamentoLinho grava motivo (cria a tag se for nova) + resultado.
func (a *App) registrarCancelamentoLinho(ticketID int64, motivo, resultado string) error {
	if !service.CancelamentoResultadoValido(resultado) {
		return fmt.Errorf("resultado inválido: %s", resultado)
	}
	m, err := a.cancelamentos.MotivoCriarSeNaoExiste(a.ctx, motivo)
	if err != nil {
		return err
	}
	if err := a.cancelamentos.CancelarMarcar(a.ctx, ticketID, m.ID, resultado); err != nil {
		return err
	}
	if a.uiPronta {
		runtime.EventsEmit(a.ctx, "cancelamentos:mudou", nil)
	}
	return nil
}

// codServiceDo360 procura o cliente pelo nome no 360 e devolve o código
// service quando o nome confere (mesma conferência dos vínculos automáticos).
func (a *App) codServiceDo360(ctx context.Context, nome string) (string, error) {
	c := a.rotaAtual()
	if c == nil {
		return "", rota360.ErrSemSessao
	}
	cod := a.resolverCodService(ctx, c, nome, map[string]string{}, &strVazio)
	if cod == "" {
		return "", fmt.Errorf("cliente não encontrado no 360 pelo nome")
	}
	return cod, nil
}

var strVazio string

// destinoDaLinha devolve a coluna de destino do card (fechados, ou a coluna
// escolhida para os em tratamento) e se o destino já está definido.
func destinoDaLinha(resultado, destinoFechado, destinoTratamento string) (string, bool) {
	if resultado == service.CancelamentoTratamento {
		return destinoTratamento, destinoTratamento != ""
	}
	return destinoFechado, true
}

// ImportarCancelamentos processa a lista colada de verdade: para cada linha
// mantém o card com ticket de cancelamento vinculado (gravando motivo +
// resultado e movendo para o destino) ou cria um segundo card importado do
// 360 com o ticket de cancelamento vinculado. Ambíguos e falhas voltam na
// lista de falhas, sem parar o lote.
func (a *App) ImportarCancelamentos(texto, colunaTratamento string) (*ResumoImportCancel, error) {
	p, _, err := a.previaImportar(texto)
	if err != nil {
		return nil, err
	}
	if p.TotalLinhas == 0 {
		return nil, fmt.Errorf("nenhuma linha reconhecida na lista")
	}
	res := &ResumoImportCancel{Falhas: []string{}}
	for _, x := range p.Erros {
		res.Falhas = append(res.Falhas, x)
	}
	destinoFechado := ColunaTicketFechado
	destinoTratamento := ""
	if colunaTratamento != "" {
		if canon, ok := a.colunaCanonica(colunaTratamento); ok {
			colunaTratamento = canon
		}
		if !a.colunaValida(colunaTratamento) {
			return nil, service.ErrColunaInvalida
		}
		destinoTratamento = colunaTratamento
	}

	for _, x := range p.Ambiguos {
		res.Falhas = append(res.Falhas, fmt.Sprintf("linha %d (%s): mais de um card com ticket de cancelamento (%s) — resolva no quadro e importe de novo",
			x.Linha, x.Nome, strings.Join(x.Candidatos, ", ")))
	}
	for _, x := range p.SemCard {
		res.Falhas = append(res.Falhas, fmt.Sprintf("linha %d (%s): não achei o cliente no 360 — conecte a sessão e importe de novo", x.Linha, x.Nome))
	}

	// Um card pode receber várias linhas (variantes do mesmo nome) — conta e
	// move só na primeira vez.
	processados := map[int64]bool{}

	gravarManter := func(x LinhaImportCancel) {
		destino, ok := destinoDaLinha(x.Resultado, destinoFechado, destinoTratamento)
		if !ok {
			res.Falhas = append(res.Falhas, fmt.Sprintf("linha %d (%s): escolha a coluna de destino dos em tratamento", x.Linha, x.Nome))
			return
		}
		if err := a.registrarCancelamentoLinho(x.TicketID, x.Motivo, x.Resultado); err != nil {
			res.Falhas = append(res.Falhas, fmt.Sprintf("linha %d (%s): %v", x.Linha, x.Nome, err))
			return
		}
		res.Registrados++
		if processados[x.TicketID] {
			return
		}
		processados[x.TicketID] = true
		// Card reusado (import anterior sem vínculo): garante o vínculo do
		// ticket de cancelamento.
		if x.Ticket360UUID != "" && a.vinculos != nil {
			if err := a.vinculos.Upsert(a.ctx, x.TicketID, x.Ticket360UUID, x.Ticket360Numero, x.Ticket360Assunto, ""); err != nil {
				res.Falhas = append(res.Falhas, fmt.Sprintf("linha %d (%s): não vinculei o ticket #%s: %v", x.Linha, x.Nome, x.Ticket360Numero, err))
			} else {
				res.Vinculados++
			}
		}
		t, err := a.tickets.Get(a.ctx, x.TicketID)
		if err != nil {
			res.Falhas = append(res.Falhas, fmt.Sprintf("linha %d (%s): %v", x.Linha, x.Nome, err))
			return
		}
		if t.ColunaAtual == destino {
			res.JaFechados++
			return
		}
		if err := a.MoveTicket(x.TicketID, destino, 0); err != nil {
			res.Falhas = append(res.Falhas, fmt.Sprintf("linha %d (%s): gravou o motivo, mas não moveu: %v", x.Linha, x.Nome, err))
			return
		}
		res.Movidos++
	}

	criar := func(x LinhaImportCancel) {
		destino, ok := destinoDaLinha(x.Resultado, destinoFechado, destinoTratamento)
		if !ok {
			res.Falhas = append(res.Falhas, fmt.Sprintf("linha %d (%s): escolha a coluna de destino dos em tratamento", x.Linha, x.Nome))
			return
		}
		t, err := a.ImportarCliente360(x.CodService, destino)
		if err != nil {
			res.Falhas = append(res.Falhas, fmt.Sprintf("linha %d (%s): importou do 360 (cod %s), mas falhou: %v", x.Linha, x.Nome, x.CodService, err))
			return
		}
		res.Importados++
		if x.Ticket360UUID != "" && a.vinculos != nil {
			if err := a.vinculos.Upsert(a.ctx, t.ID, x.Ticket360UUID, x.Ticket360Numero, x.Ticket360Assunto, ""); err != nil {
				res.Falhas = append(res.Falhas, fmt.Sprintf("linha %d (%s): card criado, mas não vinculei o ticket #%s: %v", x.Linha, x.Nome, x.Ticket360Numero, err))
			} else {
				res.Vinculados++
			}
		}
		if err := a.registrarCancelamentoLinho(t.ID, x.Motivo, x.Resultado); err != nil {
			res.Falhas = append(res.Falhas, fmt.Sprintf("linha %d (%s): card criado (cod %s), mas falhou ao gravar o motivo: %v", x.Linha, x.Nome, x.CodService, err))
			return
		}
		res.Registrados++
	}

	executar := func(lista []LinhaImportCancel) {
		for _, x := range lista {
			if x.Acao == "manter" {
				gravarManter(x)
			} else {
				criar(x)
			}
		}
	}
	executar(p.Cancelados)
	executar(p.Revertidos)
	executar(p.Tratamento)

	// Sincroniza em background com o backend da nuvem (Render/Supabase) para o dashboard do site não ficar zerado.
	go a.sincronizarImportSite(texto, colunaTratamento)

	return res, nil
}

// importarLote roda a lista colada direto da linha de comando, sem abrir a
// janela — para lotes grandes. Uso: rotasuite.exe -lista arquivo.txt
// [-coluna "Em contato"]. Requer o banco fechado em outro programa e a
// sessão do 360 já conectada (cookie na config).
func importarLote(args []string) {
	fs := flag.NewFlagSet("importar", flag.ContinueOnError)
	arquivo := fs.String("lista", "", "arquivo de texto com a lista")
	coluna := fs.String("coluna", "Em contato", "coluna de destino dos cards em tratamento")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if *arquivo == "" {
		fmt.Fprintln(os.Stderr, "use -lista <arquivo>")
		os.Exit(2)
	}
	conteudo, err := os.ReadFile(*arquivo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ler lista: %v\n", err)
		os.Exit(1)
	}
	path, err := db.DefaultPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "caminho do banco: %v\n", err)
		os.Exit(1)
	}
	database, err := db.Init(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "abrir banco %s: %v\n", path, err)
		os.Exit(1)
	}
	defer database.Close()
	app := NewApp(database, service.NewPropostaService(
		func() time.Time { return time.Now() },
		func() bool { return false },
	))
	app.WireIntegracao(
		db.NewConfigRepo(database),
		db.NewParametroRepo(database),
		db.NewCliente360Repo(database),
		nil,
		nil,
	)
	if n, err := app.cancelamentos.DeduplicarMotivos(app.ctx); err == nil && n > 0 {
		fmt.Printf("motivos duplicados mesclados: %d\n", n)
	}
	res, err := app.ImportarCancelamentos(string(conteudo), *coluna)
	if err != nil {
		fmt.Fprintf(os.Stderr, "importar: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("registrados=%d movidos=%d importados=%d vinculados=%d ja_na_coluna=%d\n",
		res.Registrados, res.Movidos, res.Importados, res.Vinculados, res.JaFechados)
	for _, f := range res.Falhas {
		fmt.Println("falha:", f)
	}
}
