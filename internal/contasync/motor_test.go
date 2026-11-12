package contasync

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"rota-smart/internal/db"
)

func banco(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestCompararValeAUltima(t *testing.T) {
	if Comparar("2026-05-02T10:00:00Z", 1, "2026-05-01T10:00:00Z", 9) != 1 {
		t.Fatal("horário mais novo ganha mesmo com versão menor")
	}
	if Comparar("2026-05-01T10:00:00Z", 2, "2026-05-01T10:00:00.000Z", 1) != 1 {
		t.Fatal("mesmo horário: versão maior ganha")
	}
	if Comparar("2026-05-01T10:00:00.9Z", 1, "2026-05-01T10:00:00.10Z", 1) != 1 {
		t.Fatal("fração de segundo compara como horário, não como texto")
	}
	if Comparar("2026-05-01T10:00:00Z", 3, "2026-05-01T10:00:00Z", 3) != 0 {
		t.Fatal("empate")
	}
}

func TestFilaNoInsertESupressao(t *testing.T) {
	database := banco(t)
	ctx := context.Background()
	if _, err := database.ExecContext(ctx,
		`INSERT INTO tickets (nome_cliente, tipo_ticket, coluna_atual) VALUES ('Ana', 'Cobrança', 'Fazer contato')`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sync_fila WHERE tabela = 'tickets' AND op = 'upsert'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("fila = %d, quero 1", n)
	}
	// Segunda edição não duplica a linha: só aumenta a versão.
	if _, err := database.ExecContext(ctx, `UPDATE tickets SET nome_cliente = 'Ana Maria' WHERE nome_cliente = 'Ana'`); err != nil {
		t.Fatal(err)
	}
	var versao int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*), MAX(versao) FROM sync_fila WHERE tabela = 'tickets'`).Scan(&n, &versao); err != nil {
		t.Fatal(err)
	}
	if n != 1 || versao < 2 {
		t.Fatalf("fila %d versão %d", n, versao)
	}

	err := Suprimir(ctx, database, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO tickets (nome_cliente, tipo_ticket, coluna_atual) VALUES ('Vinda da nuvem', 'Cobrança', 'Fazer contato')`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sync_fila WHERE tabela = 'tickets'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("a gravação da nuvem entrou na fila: %d", n)
	}
}

func TestExcluirEnfileiraCardEAcordo(t *testing.T) {
	database := banco(t)
	ctx := context.Background()
	res, err := database.ExecContext(ctx,
		`INSERT INTO tickets (nome_cliente, tipo_ticket, coluna_atual) VALUES ('Ana', 'Cobrança', 'Fazer contato')`)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	if _, err := database.ExecContext(ctx,
		`INSERT INTO cobrancas (ticket_id, total_aberto_cents, entrada_cents, numero_vezes, parcela_base_cents, parcelado_cents, ultima_parcela_cents, final_cents, status, ativa)
		 VALUES (?, 1000, 0, 1, 1000, 1000, 1000, 1000, 'proposta', 1)`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `DELETE FROM tickets WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	var apagaTicket, apagaAcordo int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sync_fila WHERE tabela = 'tickets' AND registro_id = ? AND op = 'delete'`, id).Scan(&apagaTicket); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sync_fila WHERE tabela = 'cobrancas' AND op = 'delete'`).Scan(&apagaAcordo); err != nil {
		t.Fatal(err)
	}
	if apagaTicket != 1 || apagaAcordo != 1 {
		t.Fatalf("ticket %d acordo %d", apagaTicket, apagaAcordo)
	}
}

type apiFalsa struct {
	errEnvio   error
	errBusca   error
	enviadas   []Alteracao
	pacote     Pacote
	decidir    func([]Alteracao) []Resultado
	tokenVisto string
	ordem      []string // "busca" e "envio" na ordem em que o ciclo chamou
}

func (f *apiFalsa) Enviar(_ context.Context, token string, alts []Alteracao) ([]Resultado, error) {
	f.tokenVisto = token
	f.ordem = append(f.ordem, "envio")
	f.enviadas = append(f.enviadas, alts...)
	if f.errEnvio != nil {
		return nil, f.errEnvio
	}
	if f.decidir != nil {
		return f.decidir(alts), nil
	}
	out := make([]Resultado, len(alts))
	for i, a := range alts {
		out[i] = Resultado{Tabela: a.Tabela, ID: a.ID, Resultado: "aplicada", AtualizadoEm: a.AtualizadoEm, Versao: a.Versao}
	}
	return out, nil
}

func (f *apiFalsa) Buscar(context.Context, string, string) (Pacote, error) {
	f.ordem = append(f.ordem, "busca")
	if f.errBusca != nil {
		return Pacote{}, f.errBusca
	}
	return f.pacote, nil
}

func motor(database *sql.DB, api API, token string) *Motor {
	return &Motor{
		DB:    database,
		API:   api,
		Token: func() (string, error) { return token, nil },
		Agora: func() time.Time { return parseCarimbo("2099-01-01T00:00:00Z") },
	}
}

func TestCicloOfflineNaoPrecisaDeRede(t *testing.T) {
	database := banco(t)
	ctx := context.Background()
	if _, err := database.ExecContext(ctx,
		`INSERT INTO tickets (nome_cliente, tipo_ticket, coluna_atual) VALUES ('Ana', 'Cobrança', 'Fazer contato')`); err != nil {
		t.Fatal(err)
	}
	var nome string
	if err := database.QueryRowContext(ctx, `SELECT nome_cliente FROM tickets`).Scan(&nome); err != nil {
		t.Fatal(err)
	}
	if nome != "Ana" {
		t.Fatal(nome)
	}
	n, err := ContarPendentes(ctx, database)
	if err != nil || n == 0 {
		t.Fatalf("pendentes %d err %v", n, err)
	}
}

func TestCicloRedeCaidaMantemFila(t *testing.T) {
	database := banco(t)
	ctx := context.Background()
	if _, err := database.ExecContext(ctx,
		`INSERT INTO tickets (nome_cliente, tipo_ticket, coluna_atual) VALUES ('Ana', 'Cobrança', 'Fazer contato')`); err != nil {
		t.Fatal(err)
	}
	antes, _ := ContarPendentes(ctx, database)
	api := &apiFalsa{errEnvio: &url.Error{Op: "Post", URL: "https://api", Err: errors.New("connection refused")}}
	rel, err := motor(database, api, "token-de-teste").Ciclo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Estado != EstadoAguardando {
		t.Fatalf("estado %q", rel.Estado)
	}
	depois, _ := ContarPendentes(ctx, database)
	if depois < antes {
		t.Fatalf("fila encolheu %d -> %d", antes, depois)
	}
	var tentativas int
	if err := database.QueryRowContext(ctx, `SELECT COALESCE(MAX(tentativas), 0) FROM sync_fila`).Scan(&tentativas); err != nil {
		t.Fatal(err)
	}
	if tentativas != 0 {
		t.Fatalf("rede caída não deve aumentar a punição, tentativas=%d", tentativas)
	}
}

func TestCicloAplicaRemotoMaisNovoERegistra(t *testing.T) {
	database := banco(t)
	ctx := context.Background()
	res, err := database.ExecContext(ctx,
		`INSERT INTO tickets (nome_cliente, tipo_ticket, coluna_atual) VALUES ('Ana', 'Cobrança', 'Fazer contato')`)
	if err != nil {
		t.Fatal(err)
	}
	idNum, _ := res.LastInsertId()
	id := intParaTexto(idNum)
	if _, err := database.ExecContext(ctx,
		`UPDATE sync_fila SET atualizado_em = '2020-01-01T00:00:00Z', versao = 1 WHERE tabela = 'tickets' AND registro_id = ?`, id); err != nil {
		t.Fatal(err)
	}
	api := &apiFalsa{
		pacote: Pacote{Agora: "2026-08-01T12:00:00Z"},
		decidir: func(alts []Alteracao) []Resultado {
			out := make([]Resultado, 0, len(alts))
			for _, a := range alts {
				if a.Tabela == "tickets" && a.ID == id {
					out = append(out, Resultado{
						Tabela: "tickets", ID: id, Resultado: "antiga",
						AtualizadoEm: "2026-06-01T12:00:00Z", Versao: 4,
						Dados: map[string]any{
							"nome_cliente": "Bia", "tipo_ticket": "Cobrança", "coluna_atual": "Fazer contato",
						},
					})
					continue
				}
				out = append(out, Resultado{Tabela: a.Tabela, ID: a.ID, Resultado: "aplicada", AtualizadoEm: a.AtualizadoEm, Versao: a.Versao})
			}
			return out
		},
	}
	token := "token-que-nao-pode-ir-pro-banco"
	rel, err := motor(database, api, token).Ciclo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Estado != EstadoOk {
		t.Fatalf("estado %q", rel.Estado)
	}
	var nome string
	if err := database.QueryRowContext(ctx, `SELECT nome_cliente FROM tickets WHERE id = ?`, idNum).Scan(&nome); err != nil {
		t.Fatal(err)
	}
	if nome != "Bia" {
		t.Fatalf("nome %q, a nuvem mais nova devia vencer sem perguntar", nome)
	}
	var motivos int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sync_historico WHERE motivo = 'remoto-venceu'`).Scan(&motivos); err != nil {
		t.Fatal(err)
	}
	if motivos == 0 {
		t.Fatal("conflito não foi para o histórico")
	}
	var vazou int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sync_meta WHERE valor LIKE '%' || ? || '%'`, token).Scan(&vazou); err != nil {
		t.Fatal(err)
	}
	if vazou != 0 {
		t.Fatal("token foi parar no banco")
	}
	if api.tokenVisto != token {
		t.Fatal("não enviou o token")
	}
}

func TestCicloBaixaCardNovo(t *testing.T) {
	database := banco(t)
	ctx := context.Background()
	api := &apiFalsa{pacote: Pacote{
		Agora: "2026-08-01T12:00:01Z",
		Alteracoes: []Alteracao{{
			Tabela: "tickets", ID: "424242", Apagado: false,
			AtualizadoEm: "2026-07-01T00:00:00Z", Versao: 2,
			Dados: map[string]any{
				"nome_cliente": "Outro PC", "tipo_ticket": "Cobrança", "coluna_atual": "Negociando", "posicao": 0,
			},
		}},
	}}
	if _, err := motor(database, api, "t").Ciclo(ctx); err != nil {
		t.Fatal(err)
	}
	var nome, coluna string
	if err := database.QueryRowContext(ctx, `SELECT nome_cliente, coluna_atual FROM tickets WHERE id = 424242`).Scan(&nome, &coluna); err != nil {
		t.Fatal(err)
	}
	if nome != "Outro PC" || coluna != "Negociando" {
		t.Fatalf("%s / %s", nome, coluna)
	}
	var desde string
	if err := database.QueryRowContext(ctx, `SELECT desde FROM sync_cursor WHERE id = 1`).Scan(&desde); err != nil {
		t.Fatal(err)
	}
	if desde != "2026-08-01T12:00:01Z" {
		t.Fatal(desde)
	}
}

func TestProximoDaFaixa(t *testing.T) {
	if got := proximoDaFaixa(500, 42); got%faixaStride != 42 || got <= 500 {
		t.Fatalf("candidato %d", got)
	}
	a := proximoDaFaixa(500, 42)
	b := proximoDaFaixa(500, 77)
	if a == b {
		t.Fatal("dois PCs cairiam no mesmo id")
	}
}

func TestParear404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/parear" {
			t.Errorf("caminho %s", r.URL.Path)
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	_, err := Parear(context.Background(), srv.URL, "ABC123")
	if !errors.Is(err, ErrPareamentoIndisponivel) {
		t.Fatal(err)
	}
}

func TestParearTrocaCodigo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Fatal("parear não manda token ainda")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"longo-e-secreto","usuario":{"nome":"Luis","email":"luis@rota"}}`))
	}))
	defer srv.Close()
	conta, err := Parear(context.Background(), srv.URL, "ABC123")
	if err != nil {
		t.Fatal(err)
	}
	if conta.Token != "longo-e-secreto" || conta.Nome != "Luis" {
		t.Fatalf("%+v", conta)
	}
}

func TestDoisBancosLocais(t *testing.T) {
	ctx := context.Background()
	pc1 := banco(t)
	pc2 := banco(t)
	nuvem := &nuvemMemoria{linhas: map[string]Alteracao{}}
	if _, err := pc1.ExecContext(ctx,
		`INSERT INTO tickets (nome_cliente, tipo_ticket, coluna_atual) VALUES ('Ana', 'Cobrança', 'Negociando')`); err != nil {
		t.Fatal(err)
	}
	if _, err := motor(pc1, nuvem, "token-a").Ciclo(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := motor(pc2, nuvem, "token-b").Ciclo(ctx); err != nil {
		t.Fatal(err)
	}
	var nome, coluna string
	err := pc2.QueryRowContext(ctx,
		`SELECT nome_cliente, coluna_atual FROM tickets WHERE nome_cliente = 'Ana'`).Scan(&nome, &coluna)
	if err != nil {
		t.Fatal(err)
	}
	if nome != "Ana" || coluna != "Negociando" {
		t.Fatalf("o segundo banco ficou %s / %s", nome, coluna)
	}
}

// nuvemMemoria imita o POST/GET /sync para dois bancos na mesma máquina.
type nuvemMemoria struct {
	mu     sync.Mutex
	linhas map[string]Alteracao
}

func (n *nuvemMemoria) Enviar(_ context.Context, _ string, alts []Alteracao) ([]Resultado, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]Resultado, 0, len(alts))
	for _, alt := range alts {
		chave := alt.Tabela + "\x00" + alt.ID
		atual, ok := n.linhas[chave]
		res := Resultado{Tabela: alt.Tabela, ID: alt.ID, AtualizadoEm: alt.AtualizadoEm, Versao: alt.Versao, Apagado: alt.Apagado}
		if !ok {
			if !alt.Apagado {
				n.linhas[chave] = alt
			}
			res.Resultado = "aplicada"
			out = append(out, res)
			continue
		}
		switch Comparar(alt.AtualizadoEm, alt.Versao, atual.AtualizadoEm, atual.Versao) {
		case 1:
			n.linhas[chave] = alt
			res.Resultado = "aplicada"
		case 0:
			res.Resultado = "igual"
			res.AtualizadoEm = atual.AtualizadoEm
			res.Versao = atual.Versao
		default:
			res.Resultado = "antiga"
			res.AtualizadoEm = atual.AtualizadoEm
			res.Versao = atual.Versao
			res.Apagado = atual.Apagado
			res.Dados = atual.Dados
		}
		out = append(out, res)
	}
	return out, nil
}

func (n *nuvemMemoria) Buscar(_ context.Context, _, desde string) (Pacote, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	var alts []Alteracao
	for _, alt := range n.linhas {
		if Comparar(alt.AtualizadoEm, alt.Versao, desde, 0) > 0 {
			alts = append(alts, alt)
		}
	}
	return Pacote{Agora: "2099-06-01T00:00:00Z", Alteracoes: alts}, nil
}

func intParaTexto(n int64) string {
	return fmtInt(n)
}

func fmtInt(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// O ciclo baixa as novidades da nuvem ANTES de subir a fila: subindo antes,
// uma cópia local velha de uma linha só reenfileirada (ex.: reajuste de
// posição dos outros cards) partia para a nuvem e vencia a mudança mais
// recente — card movido voltava de coluna.
func TestCicloBaixaAntesDeSubir(t *testing.T) {
	database := banco(t)
	ctx := context.Background()
	if _, err := database.ExecContext(ctx,
		`INSERT INTO tickets (nome_cliente, tipo_ticket, coluna_atual) VALUES ('Ana', 'Cobrança', 'Fazer contato')`); err != nil {
		t.Fatal(err)
	}
	api := &apiFalsa{}
	if _, err := motor(database, api, "t").Ciclo(ctx); err != nil {
		t.Fatal(err)
	}
	if len(api.ordem) < 2 || api.ordem[0] != "busca" {
		t.Fatalf("ordem do ciclo: %v — queria busca antes de envio", api.ordem)
	}
}
