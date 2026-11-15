import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import {
  TTL_CONTRATOS_MS,
  chaveCacheContratos,
  formatTempoContrato,
  gravarCacheContratos,
  lerCacheContratos,
  lerComConcorrencia,
} from './fidelidade';

describe('formatTempoContrato', () => {
  it('formata anos e meses', () => {
    expect(formatTempoContrato(27)).toBe('2a 3m');
    expect(formatTempoContrato(24)).toBe('2a');
    expect(formatTempoContrato(5)).toBe('5m');
  });
  it('devolve vazio para zero/inválido', () => {
    expect(formatTempoContrato(0)).toBe('');
    expect(formatTempoContrato(-3)).toBe('');
    expect(formatTempoContrato(NaN)).toBe('');
  });
});

describe('cache de contratos', () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => localStorage.clear());

  it('grava e lê dentro do TTL', () => {
    const chave = chaveCacheContratos('Maria');
    gravarCacheContratos(chave, { '12': { CodCliente: 12, MesesDeContrato: 5, PrimeiraAssinatura: '01/06/2026' } as never });
    const dados = lerCacheContratos(chave, Date.now());
    expect(dados?.['12']?.MesesDeContrato).toBe(5);
  });

  it('expira depois de 15 minutos', () => {
    const chave = chaveCacheContratos('Maria');
    gravarCacheContratos(chave, { '12': { CodCliente: 12 } as never }, Date.now() - TTL_CONTRATOS_MS - 1);
    expect(lerCacheContratos(chave)).toBeNull();
  });

  it('ignora cache corrompido', () => {
    const chave = chaveCacheContratos('X');
    localStorage.setItem(chave, '{{quebrado');
    expect(lerCacheContratos(chave)).toBeNull();
    localStorage.setItem(chave, '"so-uma-string"');
    expect(lerCacheContratos(chave)).toBeNull();
  });

  it('chave difere por termo', () => {
    expect(chaveCacheContratos('  Maria ')).toBe(chaveCacheContratos('maria'));
    expect(chaveCacheContratos('Maria')).not.toBe(chaveCacheContratos('Joao'));
  });
});

describe('lerComConcorrencia', () => {
  it('roda em lotes e devolve resultados', async () => {
    const concluidos: number[] = [];
    const ok = await lerComConcorrencia(
      [1, 2, 3, 4, 5],
      2,
      async (n) => n * 10,
      (_n, res) => concluidos.push(res),
      () => false,
    );
    expect(ok).toBe(true);
    expect(concluidos.sort((a, b) => a - b)).toEqual([10, 20, 30, 40, 50]);
  });

  it('cancela entre lotes', async () => {
    let chamadas = 0;
    const concluidos: number[] = [];
    const ok = await lerComConcorrencia(
      [1, 2, 3, 4, 5],
      1,
      async (n) => {
        chamadas++;
        return n;
      },
      (_n, res) => concluidos.push(res),
      () => chamadas >= 2,
    );
    expect(ok).toBe(false);
    expect(chamadas).toBe(2);
    expect(concluidos.length).toBe(2);
  });

  it('segue após falha e devolve null', async () => {
    const resultados: (number | null)[] = [];
    const ok = await lerComConcorrencia(
      [1, 2],
      2,
      async (n) => {
        if (n === 1) throw new Error('falhou');
        return n;
      },
      (_n, res) => resultados.push(res),
      () => false,
    );
    expect(ok).toBe(true);
    expect(resultados.sort((a, b) => (a ?? 0) - (b ?? 0))).toEqual([null, 2]);
  });
});
