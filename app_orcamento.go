package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"rota-smart/internal/orcamento"
)

// configPlanilhaOrcamento guarda o caminho da planilha-calculadora em uso.
const configPlanilhaOrcamento = "orcamento_planilha"

// ColunaOrcamento é a coluna do board onde ficam os cards com orçamento
// pendente; é garantida no startup.
const ColunaOrcamento = "Orçamento"

// garantirColunaOrcamento cria a coluna "Orçamento" no board se ela ainda
// não existir (o usuário pediu a coluna fixa no sistema).
func (a *App) garantirColunaOrcamento() {
	cs, err := a.colunas.List(a.ctx)
	if err != nil || len(cs) == 0 {
		return
	}
	for _, c := range cs {
		if c == ColunaOrcamento {
			return
		}
	}
	if _, err := a.colunas.Add(a.ctx, ColunaOrcamento); err != nil {
		slog.Warn("coluna Orçamento", "err", err)
	}
}

// caminhoPlanilha devolve o caminho salvo nas configurações (vazio se não há).
func (a *App) caminhoPlanilha() string {
	return a.configGet(a.ctx, configPlanilhaOrcamento)
}

// OrcamentoTabela devolve a planilha-calculadora carregada para o formulário.
// Erro claro se nenhuma planilha foi escolhida ainda.
func (a *App) OrcamentoTabela() (*orcamento.Tabela, error) {
	caminho := a.caminhoPlanilha()
	if caminho == "" {
		return nil, fmt.Errorf("escolha a planilha da calculadora em ⚙ Configurações")
	}
	t, err := orcamento.LerTabela(caminho)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// SelecionarPlanilhaOrcamento abre o diálogo de arquivo, copia a planilha
// para a pasta de dados do app (para sobreviver a mudanças na pasta original),
// valida o parse e grava o caminho nas configurações. Devolve o resumo.
func (a *App) SelecionarPlanilhaOrcamento() (string, error) {
	caminho, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Escolher planilha da calculadora",
		Filters: []runtime.FileFilter{
			{DisplayName: "Planilha Excel (*.xlsx)", Pattern: "*.xlsx"},
		},
	})
	if err != nil {
		return "", friendly("SelecionarPlanilhaOrcamento", err)
	}
	if caminho == "" {
		return "", nil // cancelado
	}

	// valida antes de copiar: planilha ruim não entra na config
	if _, err := orcamento.LerTabela(caminho); err != nil {
		return "", err
	}

	destino, err := copiarParaDados(caminho)
	if err != nil {
		return "", friendly("SelecionarPlanilhaOrcamento", err)
	}
	if a.config != nil {
		if err := a.config.Set(a.ctx, configPlanilhaOrcamento, destino); err != nil {
			return "", friendly("SelecionarPlanilhaOrcamento", err)
		}
	}

	t, err := orcamento.LerTabela(destino)
	if err != nil {
		return "", err
	}
	nomes := make([]string, 0, len(t.Tipos))
	for _, tt := range t.Tipos {
		n := len(tt.Secoes)
		nomes = append(nomes, fmt.Sprintf("%s (%d seções)", tt.Nome, n))
	}
	return "Planilha carregada: " + strings.Join(nomes, ", "), nil
}

// copiarParaDados copia o arquivo escolhido para RotaSmart/ no UserConfigDir
// e devolve o caminho do destino (nome fixo orcamento.xlsx).
func copiarParaDados(origem string) (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cfg, "RotaSmart")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	destino := filepath.Join(dir, "orcamento.xlsx")
	dados, err := os.ReadFile(origem)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(destino, dados, 0o600); err != nil {
		return "", err
	}
	return destino, nil
}

// GerarOrcamento calcula o orçamento da seleção sobre a planilha em uso e
// devolve o texto da proposta. ticketID>0 personaliza com o nome do cliente.
func (a *App) GerarOrcamento(ticketID int64, selecao orcamento.Selecao) (string, error) {
	caminho := a.caminhoPlanilha()
	if caminho == "" {
		return "", fmt.Errorf("escolha a planilha da calculadora em ⚙ Configurações")
	}
	t, err := orcamento.LerTabela(caminho)
	if err != nil {
		return "", err
	}
	res, err := orcamento.Calcular(t, selecao)
	if err != nil {
		return "", err
	}
	cliente := ""
	if ticketID > 0 {
		if t, err := a.tickets.Get(a.ctx, ticketID); err == nil {
			cliente = t.NomeCliente
		}
	}
	return res.PropostaTexto(cliente), nil
}

// planilhaParaAgente é o mesmo parse, para a tool do agente (silenciosa).
func (a *App) planilhaParaAgente(ctx context.Context) (*orcamento.Tabela, error) {
	caminho := a.configGet(ctx, configPlanilhaOrcamento)
	if caminho == "" {
		return nil, fmt.Errorf("planilha da calculadora não configurada (⚙ Configurações)")
	}
	return orcamento.LerTabela(caminho)
}
