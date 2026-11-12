package contasync

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// intervaloConferencia separa duas conferências completas com a nuvem.
const intervaloConferencia = 2 * time.Minute

// Conferencia resume a última conferência completa.
type Conferencia struct {
	Comparadas   int
	Enfileiradas int
}

// conferir compara cada linha local com a cópia da nuvem e põe na fila o que
// ficou diferente sem estar na fila. É a rede de segurança dos gatilhos: se
// uma alteração local não entrou na fila por qualquer motivo, entra aqui.
//
// Vale o local porque o ciclo acabou de puxar tudo o que a nuvem tinha de
// novo (desde): sobra diferença só onde este PC mudou e não enviou.
// Linha da nuvem mais nova que o cursor fica para o próximo ciclo.
func conferir(ctx context.Context, m *Motor, token, cursorAtual string) (Conferencia, error) {
	var res Conferencia
	pacote, err := m.API.Buscar(ctx, token, "1970-01-01T00:00:00Z")
	if err != nil {
		return res, err
	}
	nuvem := make(map[string]Alteracao, len(pacote.Alteracoes))
	for _, a := range pacote.Alteracoes {
		nuvem[a.Tabela+"\x00"+a.ID] = a
	}
	limite := parseCarimbo(cursorAtual)
	agora := carimboAgora(m.agora())

	for _, nome := range ordemUpsert {
		spec := tabelas[nome]
		ids, err := idsLocais(ctx, m, spec)
		if err != nil {
			return res, err
		}
		for _, id := range ids {
			if _, tem, err := lerFila(ctx, m.DB, spec.nome, id); err != nil {
				return res, err
			} else if tem {
				continue
			}
			local, ok, err := lerDados(ctx, m.DB, spec.nome, id)
			if err != nil || !ok {
				continue
			}
			res.Comparadas++
			remota, existe := nuvem[spec.nome+"\x00"+id]
			if existe {
				if remota.Apagado {
					// Apagado na nuvem e vivo aqui: o puxar ainda não aplicou.
					// Não ressuscita a linha.
					continue
				}
				if t := parseCarimbo(remota.AtualizadoEm); !limite.IsZero() && t.After(limite) {
					continue
				}
				if iguais(spec, local, remota.Dados) {
					continue
				}
			}
			versao := remota.Versao
			if _, v, tem, err := lerEstado(ctx, m.DB, spec.nome, id); err == nil && tem && v > versao {
				versao = v
			}
			// Carimba com o horário real da mudança da linha (mesma regra
			// dos gatilhos): com "agora", a cópia local vencia na nuvem uma
			// alteração mais recente que ela mesma — card voltava de coluna.
			carimbo := agora
			if c, err := carimboLinhaLocal(ctx, m.DB, spec, id); err == nil && c != "" {
				carimbo = c
			}
			if _, err := m.DB.ExecContext(ctx,
				`INSERT INTO sync_fila (tabela, registro_id, op, atualizado_em, versao, tentativas, proxima_em)
				 VALUES (?, ?, 'upsert', ?, ?, 0, ?)
				 ON CONFLICT(tabela, registro_id) DO NOTHING`,
				spec.nome, id, carimbo, versao+1, agora); err != nil {
				return res, err
			}
			res.Enfileiradas++
		}
	}
	if res.Enfileiradas > 0 {
		slog.Info("conferência com a nuvem", "comparadas", res.Comparadas, "enfileiradas", res.Enfileiradas)
	}
	return res, nil
}

// carimboLinhaLocal devolve o horário real da mudança da linha (mesma regra
// dos gatilhos) para as tabelas com updated_at; nas demais devolve "" e a
// conferência usa o horário corrente.
func carimboLinhaLocal(ctx context.Context, db executor, spec specTabela, id string) (string, error) {
	if !strings.Contains(spec.carimboLocal, "updated_at") {
		return "", nil
	}
	pk, err := idArg(spec, id)
	if err != nil {
		return "", err
	}
	var carimbo string
	err = db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT %s FROM %s WHERE %s = ?", spec.carimboLocal, spec.nome, spec.pk), pk).Scan(&carimbo)
	return carimbo, err
}

func idsLocais(ctx context.Context, m *Motor, spec specTabela) ([]string, error) {
	rows, err := m.DB.QueryContext(ctx, fmt.Sprintf("SELECT %s FROM %s", exprIDSQL(spec, spec.nome), spec.nome))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// iguais compara os campos de conteúdo. Datas ficam de fora: a nuvem
// reformata e preenche data vazia com o carimbo, o que não é mudança de verdade.
func iguais(spec specTabela, local, remota map[string]any) bool {
	for _, c := range spec.campos {
		if c.tipo == campoTempo {
			continue
		}
		switch c.tipo {
		case campoInt, campoBool:
			if comoInt(local[c.nome]) != comoInt(remota[c.nome]) {
				return false
			}
		default:
			l := strings.ReplaceAll(comoTexto(local[c.nome]), "\x00", "")
			if l != comoTexto(remota[c.nome]) {
				return false
			}
		}
	}
	return true
}
