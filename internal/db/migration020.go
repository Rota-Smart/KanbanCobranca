package db

func init() {
	migrations = append(migrations, migracao020())
}

// migration020: carimbo da última geração de orçamento do card. A fila do dia
// precisa saber há quanto tempo um orçamento foi enviado sem resposta — como
// o envio é manual (o usuário copia o texto da proposta), marcamos a data na
// hora em que a proposta é gerada para o card (GerarOrcamento).
func migracao020() string {
	return `
ALTER TABLE tickets ADD COLUMN orcamento_gerado_em TEXT NOT NULL DEFAULT '';
`
}
