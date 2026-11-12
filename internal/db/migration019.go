package db

func init() {
	migrations = append(migrations, migracao019())
}

// migration019: coluna de origem da visita — quando o card entra em
// "Visita Agendada" vindo de outra coluna, grava de onde veio. Cancelar a
// visita devolve o card pra coluna de origem em vez de deixá-lo parado
// ("Visita Agendada" sem data eternamente).
func migracao019() string {
	return `
ALTER TABLE tickets ADD COLUMN visita_origem TEXT NOT NULL DEFAULT '';
`
}
