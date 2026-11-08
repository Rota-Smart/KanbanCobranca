package main

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"rota-smart/internal/nuvem"
)

// Chaves da tabela config para a nuvem (Etapa 1 do plano web-360).
const (
	cfgNuvemDSN    = "nuvem_dsn"
	cfgNuvemEnvio  = "nuvem_ultimo_envio"
	cfgNuvemTotal  = "nuvem_ultimo_total"
)

// NuvemInfo é o estado da nuvem exposto ao frontend.
type NuvemInfo struct {
	Configurado  bool   `json:"configurado"`
	UltimoEnvio  string `json:"ultimoEnvio"`
	UltimoTotal  int    `json:"ultimoTotal"`
}

func (a *App) nuvemConfigurado() error {
	if a.config == nil {
		return fmt.Errorf("configuração indisponível")
	}
	if a.db == nil {
		return fmt.Errorf("banco indisponível nesta instalação")
	}
	return nil
}

// NuvemStatus devolve o estado atual da integração com a nuvem.
func (a *App) NuvemStatus() NuvemInfo {
	info := NuvemInfo{}
	if err := a.nuvemConfigurado(); err != nil {
		return info
	}
	dsn, _ := a.config.Get(a.ctx, cfgNuvemDSN)
	info.Configurado = dsn != ""
	info.UltimoEnvio, _ = a.config.Get(a.ctx, cfgNuvemEnvio)
	total, _ := a.config.Get(a.ctx, cfgNuvemTotal)
	fmt.Sscanf(total, "%d", &info.UltimoTotal)
	return info
}

// SetNuvemConfig grava a linha de conexão do Postgres na nuvem
// (postgresql://rotasuite_api:…@db.…supabase.co:5432/postgres).
func (a *App) SetNuvemConfig(dsn string) error {
	if err := a.nuvemConfigurado(); err != nil {
		return err
	}
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return fmt.Errorf("cole a linha de conexão da nuvem")
	}
	if err := nuvem.TestarConexao(a.ctx, dsn); err != nil {
		return friendly("SetNuvemConfig", err)
	}
	if err := a.config.Set(a.ctx, cfgNuvemDSN, dsn); err != nil {
		return friendly("SetNuvemConfig", err)
	}
	slog.Info("nuvem: conexão configurada")
	return nil
}

// EnviarParaNuvem sobe TODAS as tabelas do banco local para o Postgres na
// nuvem. Idempotente — pode repetir sem medo; o último envio vence.
func (a *App) EnviarParaNuvem() (ResultadoNuvem, error) {
	var res ResultadoNuvem
	if err := a.nuvemConfigurado(); err != nil {
		return res, err
	}
	dsn, err := a.config.Get(a.ctx, cfgNuvemDSN)
	if err != nil || dsn == "" {
		return res, fmt.Errorf("configure a conexão da nuvem antes de enviar")
	}
	r, err := nuvem.Enviar(a.ctx, a.db, dsn)
	if err != nil {
		return res, friendly("EnviarParaNuvem", err)
	}
	quando := time.Now().Format("02/01/2006 15:04")
	if err := a.config.Set(a.ctx, cfgNuvemEnvio, quando); err != nil {
		return res, friendly("EnviarParaNuvem", err)
	}
	if err := a.config.Set(a.ctx, cfgNuvemTotal, fmt.Sprintf("%d", r.Total)); err != nil {
		return res, friendly("EnviarParaNuvem", err)
	}
	slog.Info("nuvem: envio concluído", "total", r.Total, "duracao_ms", r.DuracaoMs)
	res = ResultadoNuvem{
		Acao:     "enviado",
		Mensagem: fmt.Sprintf("%d registros enviados para a nuvem em %ds.", r.Total, r.DuracaoMs/1000+1),
		Total:    r.Total,
	}
	return res, nil
}

// ResultadoNuvem descreve o que o botão Enviar fez.
type ResultadoNuvem struct {
	Acao     string `json:"acao"` // "enviado"
	Mensagem string `json:"mensagem"`
	Total    int    `json:"total"`
}