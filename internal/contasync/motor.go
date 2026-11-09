package contasync

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"time"
)

// API é o pedaço da nuvem que o ciclo usa. Os testes trocam por um dublê.
type API interface {
	Enviar(ctx context.Context, token string, alts []Alteracao) ([]Resultado, error)
	Buscar(ctx context.Context, token, desde string) (Pacote, error)
}

// Motor dá uma volta: sobe o que está na fila e desce o que mudou na nuvem.
type Motor struct {
	DB    *sql.DB
	API   API
	Token func() (string, error)
	Agora func() time.Time
}

func (m *Motor) agora() time.Time {
	if m.Agora != nil {
		return m.Agora()
	}
	return time.Now()
}

// Ciclo não trava quem chamou à espera de rede. Erro de rede vira "aguardando".
func (m *Motor) Ciclo(ctx context.Context) (Relatorio, error) {
	token, err := m.Token()
	if err != nil {
		return Relatorio{Estado: EstadoReconectar}, nil
	}
	if strings.TrimSpace(token) == "" {
		return Relatorio{}, nil
	}
	if err := enfileirarBase(ctx, m.DB); err != nil {
		return Relatorio{}, err
	}

	for rodada := 0; rodada < 30; rodada++ {
		itens, err := pendentes(ctx, m.DB, m.agora(), 40)
		if err != nil {
			return Relatorio{}, err
		}
		if len(itens) == 0 {
			break
		}
		alts := make([]Alteracao, 0, len(itens))
		for _, it := range itens {
			alt, err := montar(ctx, m.DB, it)
			if err != nil {
				return Relatorio{}, err
			}
			alts = append(alts, alt)
		}
		resultados, err := m.API.Enviar(ctx, token, alts)
		if err != nil {
			if EhNaoAutorizado(err) {
				return Relatorio{Estado: EstadoReconectar}, nil
			}
			if EhRede(err) {
				n, _ := ContarPendentes(ctx, m.DB)
				return Relatorio{Estado: EstadoAguardando, Pendentes: n}, nil
			}
			slog.Warn("sync envio recusado", "err", err)
			if err := adiar(ctx, m.DB, itens, m.agora().Add(5*time.Minute)); err != nil {
				return Relatorio{}, err
			}
			break
		}
		if err := m.tratarEnvio(ctx, itens, resultados); err != nil {
			return Relatorio{}, err
		}
	}

	desde, err := cursor(ctx, m.DB)
	if err != nil {
		return Relatorio{}, err
	}
	pacote, err := m.API.Buscar(ctx, token, desde)
	if err != nil {
		if EhNaoAutorizado(err) {
			return Relatorio{Estado: EstadoReconectar}, nil
		}
		if EhRede(err) {
			n, _ := ContarPendentes(ctx, m.DB)
			return Relatorio{Estado: EstadoAguardando, Pendentes: n}, nil
		}
		slog.Warn("sync busca recusada", "err", err)
		n, _ := ContarPendentes(ctx, m.DB)
		return Relatorio{Estado: EstadoOk, Pendentes: n}, nil
	}
	aplicou, err := m.aplicarPacote(ctx, pacote.Alteracoes)
	if err != nil {
		return Relatorio{}, err
	}
	if pacote.Agora != "" {
		if err := gravarCursor(ctx, m.DB, pacote.Agora); err != nil {
			return Relatorio{}, err
		}
	}
	if err := reservarFaixa(ctx, m.DB); err != nil {
		return Relatorio{}, err
	}
	n, _ := ContarPendentes(ctx, m.DB)
	return Relatorio{Estado: EstadoOk, Aplicou: aplicou, Pendentes: n}, nil
}

func (m *Motor) tratarEnvio(ctx context.Context, itens []Item, resultados []Resultado) error {
	porChave := map[string]Resultado{}
	for _, r := range resultados {
		porChave[r.Tabela+"\x00"+r.ID] = r
	}
	var antigas []Alteracao
	err := Suprimir(ctx, m.DB, func(tx *sql.Tx) error {
		for _, it := range itens {
			res, ok := porChave[it.Tabela+"\x00"+it.RegistroID]
			if !ok {
				continue
			}
			switch res.Resultado {
			case "aplicada", "igual":
				alt := Alteracao{Tabela: it.Tabela, ID: it.RegistroID, AtualizadoEm: res.AtualizadoEm, Versao: res.Versao}
				if alt.AtualizadoEm == "" {
					alt.AtualizadoEm = it.AtualizadoEm
					alt.Versao = it.Versao
				}
				if err := gravarEstado(ctx, tx, alt); err != nil {
					return err
				}
				if err := removerFilaSe(ctx, tx, it.Tabela, it.RegistroID, it.Versao); err != nil {
					return err
				}
			case "antiga":
				// Só desfaz se a fila ainda é a versão que perdeu.
				// Se a pessoa editou de novo durante o envio, a versão mudou e fica na fila.
				fila, tem, err := lerFila(ctx, tx, it.Tabela, it.RegistroID)
				if err != nil {
					return err
				}
				if tem && fila.Versao != it.Versao {
					continue
				}
				if err := registrarHistorico(ctx, tx, it.Tabela, it.RegistroID, "remoto-venceu", "a nuvem tinha uma alteração mais recente"); err != nil {
					return err
				}
				antigas = append(antigas, Alteracao{
					Tabela: res.Tabela, ID: res.ID, Apagado: res.Apagado,
					AtualizadoEm: res.AtualizadoEm, Versao: res.Versao, Dados: res.Dados,
				})
				if err := removerFilaSe(ctx, tx, it.Tabela, it.RegistroID, it.Versao); err != nil {
					return err
				}
			case "erro":
				if err := adiar(ctx, tx, []Item{it}, m.agora().Add(tempoRecuo(it.Tentativas+1))); err != nil {
					return err
				}
			}
		}
		for _, alt := range antigas {
			if err := gravarAlteracao(ctx, tx, alt); err != nil {
				return err
			}
			if err := gravarEstado(ctx, tx, alt); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

func (m *Motor) aplicarPacote(ctx context.Context, alts []Alteracao) (int, error) {
	if len(alts) == 0 {
		return 0, nil
	}
	ordenadas := ordenar(alts)
	aplicou := 0
	err := Suprimir(ctx, m.DB, func(tx *sql.Tx) error {
		for _, alt := range ordenadas {
			acao, err := decidir(ctx, tx, alt)
			if err != nil {
				return err
			}
			if acao == "manter" {
				if err := registrarHistorico(ctx, tx, alt.Tabela, alt.ID, "local-venceu", "este computador tinha uma alteração mais recente"); err != nil {
					return err
				}
				continue
			}
			if acao == "igual" {
				if err := gravarEstado(ctx, tx, alt); err != nil {
					return err
				}
				continue
			}
			if _, err := tx.ExecContext(ctx, "SAVEPOINT sp_sync"); err != nil {
				return err
			}
			if err := gravarAlteracao(ctx, tx, alt); err != nil {
				_, _ = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT sp_sync")
				slog.Warn("sync não aplicou registro", "tabela", alt.Tabela, "id", alt.ID, "err", err)
				if err := registrarHistorico(ctx, tx, alt.Tabela, alt.ID, "falha", "não deu para aplicar esta linha"); err != nil {
					return err
				}
				continue
			}
			if _, err := tx.ExecContext(ctx, "RELEASE SAVEPOINT sp_sync"); err != nil {
				return err
			}
			// A fila local desta linha perde se a nuvem for mais nova — decidir já filtrou isso.
			fila, tem, err := lerFila(ctx, tx, alt.Tabela, alt.ID)
			if err != nil {
				return err
			}
			if tem {
				if err := removerFilaSe(ctx, tx, alt.Tabela, alt.ID, fila.Versao); err != nil {
					return err
				}
				if err := registrarHistorico(ctx, tx, alt.Tabela, alt.ID, "remoto-venceu", "a nuvem tinha uma alteração mais recente"); err != nil {
					return err
				}
			}
			if err := gravarEstado(ctx, tx, alt); err != nil {
				return err
			}
			aplicou++
		}
		return nil
	})
	return aplicou, err
}

// decidir: aplicar, manter (a fila local é mais nova) ou igual.
func decidir(ctx context.Context, tx *sql.Tx, alt Alteracao) (string, error) {
	fila, temFila, err := lerFila(ctx, tx, alt.Tabela, alt.ID)
	if err != nil {
		return "", err
	}
	if temFila {
		switch Comparar(alt.AtualizadoEm, alt.Versao, fila.AtualizadoEm, fila.Versao) {
		case -1:
			return "manter", nil
		case 0:
			return "aplicar", nil
		default:
			return "aplicar", nil
		}
	}
	em, ver, tem, err := lerEstado(ctx, tx, alt.Tabela, alt.ID)
	if err != nil {
		return "", err
	}
	if !tem {
		return "aplicar", nil
	}
	switch Comparar(alt.AtualizadoEm, alt.Versao, em, ver) {
	case -1:
		return "manter", nil
	case 0:
		return "igual", nil
	default:
		return "aplicar", nil
	}
}

func ordenar(alts []Alteracao) []Alteracao {
	pos := map[string]int{}
	for i, nome := range ordemUpsert {
		pos[nome] = i
	}
	var upserts, deletes []Alteracao
	for _, a := range alts {
		if a.Apagado {
			deletes = append(deletes, a)
		} else {
			upserts = append(upserts, a)
		}
	}
	menos := func(a, b Alteracao) bool {
		pa, oka := pos[a.Tabela]
		pb, okb := pos[b.Tabela]
		if !oka {
			pa = 99
		}
		if !okb {
			pb = 99
		}
		return pa < pb
	}
	sortEstavel(upserts, menos)
	// Exclusão: filhos antes do card, para a chave estrangeira não reclamar.
	sortEstavel(deletes, func(a, b Alteracao) bool { return menos(b, a) })
	return append(upserts, deletes...)
}

func sortEstavel(alts []Alteracao, less func(a, b Alteracao) bool) {
	for i := 1; i < len(alts); i++ {
		j := i
		for j > 0 && less(alts[j], alts[j-1]) {
			alts[j], alts[j-1] = alts[j-1], alts[j]
			j--
		}
	}
}

func tempoRecuo(tentativas int) time.Duration {
	d := 5 * time.Second
	for i := 1; i < tentativas; i++ {
		d *= 2
		if d > 5*time.Minute {
			return 5 * time.Minute
		}
	}
	return d
}
