package db

func init() {
	migrations = append(migrations, migracao023())
}

// migration023: a lista colada de cancelamentos também registra quem está
// EM TRATAMENTO (ticket em aberto, motivo já escolhido). O CHECK antigo só
// aceitava reversao/perda — SQLite não altera CHECK, então recria a tabela
// preservando os registros.
func migracao023() string {
	return `
CREATE TABLE cancelamentos_nova (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  ticket_id INTEGER NOT NULL UNIQUE,
  motivo_id INTEGER NOT NULL REFERENCES motivos_cancelamento(id),
  resultado TEXT NOT NULL CHECK (resultado IN ('reversao','perda','em_tratamento')),
  criado_em TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
INSERT INTO cancelamentos_nova (id, ticket_id, motivo_id, resultado, criado_em)
  SELECT id, ticket_id, motivo_id, resultado, criado_em FROM cancelamentos;
DROP TABLE cancelamentos;
ALTER TABLE cancelamentos_nova RENAME TO cancelamentos;
`
}
