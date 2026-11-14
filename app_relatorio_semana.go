package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"log/slog"
	"os"
	"strings"
	"time"

	"rota-smart/internal/appdata"
)

// Relatório semanal automático: ao abrir o app, se o último resumo semanal
// tem mais de 7 dias, o app gera o da semana sozinho (mesmo cálculo do
// GerarRelatorio manual) e guarda no histórico local. O frontend mostra só
// um banner discreto — nada abre sem clique. O envio usa o WhatsApp via
// wa.me no navegador padrão, para o número do dono configurado (ou sem
// número, deixando o usuário escolher o contato).

const (
	// cfgWhatsappDono: número do destinatário do relatório (só dígitos, DDI+DDD).
	cfgWhatsappDono = "whatsapp_dono"
	// cfgRelatorioSemanalUltimo: AAAA-MM-DD da última geração automática.
	cfgRelatorioSemanalUltimo = "relatorio_semanal_ultimo"
)

// maxRelatoriosHistorico: quantos resumos semanais ficam guardados no disco.
const maxRelatoriosHistorico = 12

// RelatorioSemanal é o resultado da checagem de abertura do app.
type RelatorioSemanal struct {
	Novo   bool   `json:"novo"`   // true = gerou agora; o frontend mostra o banner
	Texto  string `json:"texto"`  // texto pronto (quando novo)
	Quando string `json:"quando"` // AAAA-MM-DD da geração
}

// ResumoSalvo é um relatório semanal do histórico (lista nas Configurações).
type ResumoSalvo struct {
	Quando string `json:"quando"` // AAAA-MM-DD da geração
	Texto  string `json:"texto"`
}

// historicoRelatorios lê a lista do arquivo de dados do app (vazia se não existe).
func (a *App) historicoRelatorios() []ResumoSalvo {
	caminho, err := a.caminhoHistoricoRelatorios()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(caminho)
	if err != nil {
		return nil
	}
	var lista []ResumoSalvo
	if json.Unmarshal(b, &lista) != nil {
		return nil
	}
	return lista
}

// caminhoHistoricoRelatorios resolve o arquivo; teste pode apontar pra pasta própria.
func (a *App) caminhoHistoricoRelatorios() (string, error) {
	if a.caminhoRelatorios != "" {
		return a.caminhoRelatorios, nil
	}
	return appdata.Caminho("relatorios_semanais.json")
}

// salvarResumoSemanal anexa o relatório ao histórico (mais recente primeiro,
// com teto de maxRelatoriosHistorico) e grava o carimbo da última geração.
func (a *App) salvarResumoSemanal(r ResumoSalvo) error {
	lista := append([]ResumoSalvo{r}, a.historicoRelatorios()...)
	if len(lista) > maxRelatoriosHistorico {
		lista = lista[:maxRelatoriosHistorico]
	}
	b, err := json.Marshal(lista)
	if err != nil {
		return err
	}
	caminho, err := a.caminhoHistoricoRelatorios()
	if err != nil {
		return err
	}
	if err := os.WriteFile(caminho, b, 0o600); err != nil {
		return err
	}
	return a.salvarCarimboSemanal(r.Quando)
}

// salvarCarimboSemanal grava a data da última geração no config (best effort
// quando não há config — testes antigos; o histórico em arquivo continua valendo).
func (a *App) salvarCarimboSemanal(quando string) error {
	if a.config == nil {
		return nil
	}
	return a.config.Set(a.ctx, cfgRelatorioSemanalUltimo, quando)
}

// RelatorioSemanalAuto: chamado pelo frontend na abertura do app. Se nunca
// gerou (ou o carimbo tem mais de 7 dias), gera o resumo dos últimos 7 dias
// (mesmo cálculo do manual), acrescenta a comparação com a semana anterior,
// guarda no histórico e devolve novo=true para o banner. Caso contrário não
// faz nada — o app abre silencioso.
func (a *App) RelatorioSemanalAuto() (RelatorioSemanal, error) {
	agora := time.Now()
	hoje := agora.Format("2006-01-02")
	ultimo := a.configGet(a.ctx, cfgRelatorioSemanalUltimo)
	if ultimo != "" {
		if t, err := time.Parse("2006-01-02", ultimo); err == nil {
			if agora.Sub(t) < 7*24*time.Hour {
				return RelatorioSemanal{Novo: false}, nil
			}
		}
	}
	texto, err := a.GerarRelatorio("", "") // últimos 7 dias, fim hoje
	if err != nil {
		return RelatorioSemanal{}, err
	}
	if comp := a.linhaComparativaSemanal(); comp != "" {
		texto += comp
	}
	r := ResumoSalvo{Quando: hoje, Texto: texto}
	if err := a.salvarResumoSemanal(r); err != nil {
		// Sem histórico em disco o banner ainda vale — só registra o problema.
		slog.Warn("relatorio semanal: falhou ao salvar historico", "err", err)
	}
	return RelatorioSemanal{Novo: true, Texto: texto, Quando: hoje}, nil
}

// linhaComparativaSemanal gera a linha final "vs semana anterior: …"
// comparando os contadores da semana corrente (últimos 7 dias) com os da
// janela imediatamente anterior. Só menciona o que mudou.
func (a *App) linhaComparativaSemanal() string {
	hoje := time.Now()
	fim := hoje.Format("2006-01-02")
	inicio := hoje.AddDate(0, 0, -7).Format("2006-01-02")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	atual := a.contadoresRelatorio(ctx, inicio, fim)

	fimAnt := hoje.AddDate(0, 0, -8).Format("2006-01-02")
	inicioAnt := hoje.AddDate(0, 0, -14).Format("2006-01-02")
	ant := a.contadoresRelatorio(ctx, inicioAnt, fimAnt)

	diff := func(a, b int) string {
		switch {
		case a > b:
			return fmt.Sprintf("+%d", a-b)
		case a < b:
			return fmt.Sprintf("-%d", b-a)
		}
		return ""
	}
	pares := []struct {
		nome string
		val  string
	}{
		{"acordos", diff(atual.acordos, ant.acordos)},
		{"visitas", diff(atual.visitas, ant.visitas)},
		{"fechados", diff(atual.fechados, ant.fechados)},
		{"tentativas", diff(atual.tentativas, ant.tentativas)},
		{"novos cards", diff(atual.novos, ant.novos)},
	}
	var ordenadas []string
	for _, p := range pares {
		if p.val != "" {
			ordenadas = append(ordenadas, p.nome+" "+p.val)
		}
	}
	if len(ordenadas) == 0 {
		return "\n\nvs semana anterior: igual"
	}
	return "\n\nvs semana anterior: " + strings.Join(ordenadas, ", ")
}

// SetWhatsappDono grava o número do destinatário (só dígitos; DDI+DDD).
func (a *App) SetWhatsappDono(numero string) error {
	if a.config == nil {
		return fmt.Errorf("configuração indisponível")
	}
	limpo := somenteDigitos(numero)
	if len(limpo) > 0 && (len(limpo) < 10 || len(limpo) > 15) {
		return fmt.Errorf("número inválido: use DDI+DDD+número, só dígitos (ex.: 5511999998888)")
	}
	return a.config.Set(a.ctx, cfgWhatsappDono, limpo)
}

// somenteDigitos tira tudo que não é dígito (espaços, +, traços…).
func somenteDigitos(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// AbrirRelatorioWhatsapp abre o navegador padrão em wa.me com o texto do
// relatório pré-preenchido. Com número do dono salvo, já vai para o chat
// dele; sem número, o WhatsApp Web deixa escolher o contato.
func (a *App) AbrirRelatorioWhatsapp(texto string) error {
	if strings.TrimSpace(texto) == "" {
		return fmt.Errorf("não há resumo para enviar")
	}
	numero := somenteDigitos(a.configGet(a.ctx, cfgWhatsappDono))
	link := "https://wa.me/" + numero + "?text=" + url.QueryEscape(texto)
	return abrirNoNavegador(link)
}

// HistoricoRelatorios devolve os resumos semanais guardados (mais recente
// primeiro) para a lista nas Configurações.
func (a *App) HistoricoRelatorios() []ResumoSalvo {
	return a.historicoRelatorios()
}
