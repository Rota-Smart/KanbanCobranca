import { describe, expect, it } from 'vitest';
import { formatCents, parseCents } from './money';

describe('parseCents', () => {
  it.each([
    ['1.234,56', 123456],
    ['10', 1000],
    ['10,5', 1050],
    [',5', 50],
    ['R$ 10,00', 1000],
    ['0', 0],
  ])('%s → %i', (input, esperado) => {
    expect(parseCents(input)).toBe(esperado);
  });

  it.each(['abc', '', '1,234', '-5', '1.2.3,4a'])('rejeita %s', (input) => {
    expect(parseCents(input)).toBeNull();
  });
});

describe('formatCents', () => {
  it('formata para exibição pt-BR', () => {
    expect(formatCents(123456)).toBe('1.234,56');
  });
});
