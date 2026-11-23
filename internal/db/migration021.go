package db

func init() {
	migrations = append(migrations, migracao021())
}

// migration021: motivo obrigatório de cancelamento. Todo card com ticket de
// assunto "Cancelamento" só pode entrar em "Ticket Fechado" depois de um
// registro aqui: motivo (tag criável pelo usuário) + resultado
// (reversão = cliente ficou, perda = cliente saiu). Um registro por card —
// sair da coluna de fechados apaga o registro (o card voltou).
func migracao021() string {
	return `
CREATE TABLE IF NOT EXISTS motivos_cancelamento (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  nome      TEXT NOT NULL,
  nome_chave TEXT NOT NULL UNIQUE,
  criado_em TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE TABLE IF NOT EXISTS cancelamentos (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  ticket_id INTEGER NOT NULL UNIQUE,
  motivo_id INTEGER NOT NULL REFERENCES motivos_cancelamento(id),
  resultado TEXT NOT NULL CHECK (resultado IN ('reversao','perda')),
  criado_em TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
`
}
