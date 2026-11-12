package db

import "fmt"

func init() {
	migrations = append(migrations, migracao018())
}

// migration018: os gatilhos da fila de sync passam a carimbar a fila com o
// horário REAL da mudança (updated_at da linha) em vez do horário do
// enfileiramento (gatilho novo em migration017.go, mapa carimboLinha). Com o
// carimbo "agora", uma linha só reenfileirada (ex.: o reajuste de posição dos
// outros cards quando um card é movido) subia para a nuvem parecendo a
// alteração mais recente e a cópia velha dela vencia a mudança verdadeira —
// card movido no app voltava para a coluna anterior na conferência seguinte.
// SQLite não tem CREATE OR REPLACE TRIGGER: é preciso soltar e recriar.
func migracao018() string {
	b := ""
	for _, t := range tabelasSync {
		for _, sufixo := range []string{"ins", "upd", "del"} {
			b += fmt.Sprintf("DROP TRIGGER IF EXISTS %s_sync_%s;\n", t.nome, sufixo)
		}
	}
	b += "DROP TRIGGER IF EXISTS kanban_columns_sync_rename;\n"
	for _, t := range tabelasSync {
		b += gatilhosSync(t.nome, t.pk)
	}
	return b
}
