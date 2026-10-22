// Parsing e formatação de moeda por manipulação de texto: nenhum valor
// financeiro passa por float antes de virar centavos inteiros.

export function parseCents(input: string): number | null {
  const s = input.trim().replace(/^R\$\s?/, '').replace(/\./g, '');
  if (!/^(?:\d+(,\d{0,2})?|\d*,\d{1,2})$/.test(s)) return null;
  const [reais, cents = ''] = s.split(',');
  return Number(reais) * 100 + Number(cents.padEnd(2, '0'));
}

const brl = new Intl.NumberFormat('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
// Somente para exibição; decisões de valor ficam sempre em centavos.
export const formatCents = (c: number) => brl.format(c / 100);
