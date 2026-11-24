package db

func init() {
	migrations = append(migrations, migracao024())
}

// migration024: coluna apagado no ticket — o painel 📊 filtra os cards
// apagados das contagens, mas a coluna nunca foi criada (o filtro entrou
// sem a migration e o painel quebrava com "no such column").
func migracao024() string {
	return `
ALTER TABLE tickets ADD COLUMN apagado INTEGER NOT NULL DEFAULT 0 CHECK (apagado IN (0,1));
`
}
