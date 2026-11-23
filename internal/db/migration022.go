package db

func init() {
	migrations = append(migrations, migracao022())
}

// migration022: setor do helpdesk 360 no vínculo — alimenta o novo filtro
// por setor do board (mesmo filtro da tela "novo ticket" do 360). O assunto
// já vive na coluna assunto; o setor vem junto na fila e no detalhe.
func migracao022() string {
	return `
ALTER TABLE tickets_vinculos ADD COLUMN setor TEXT NOT NULL DEFAULT '';
`
}
