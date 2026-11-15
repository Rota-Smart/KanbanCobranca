// Fidelidade: helpers puros do dashboard de Fidelidade — tempo de contrato e
// cache local dos resumos de contratos (1 request por cliente no 360, então
// guardamos em localStorage por 15 minutos, padrão do site).

import type { ContratoResumo } from './wails-types';

// TTL do cache dos resumos de contratos: 15 minutos (padrão do site).
export const TTL_CONTRATOS_MS = 15 * 60 * 1000;

// formatTempoContrato: "2a 3m" (anos+meses), "5m" (só meses) ou "" se zero/inválido.
export function formatTempoContrato(meses: number): string {
  if (!Number.isFinite(meses) || meses <= 0) return '';
  const anos = Math.floor(meses / 12);
  const rest = meses % 12;
  if (anos > 0 && rest > 0) return `${anos}a ${rest}m`;
  if (anos > 0) return `${anos}a`;
  return `${rest}m`;
}

// parseDataBR: "02/01/2006" → Date (null se inválido).
export function parseDataBR(s: string | undefined): Date | null {
  if (!s) return null;
  const m = /^(\d{2})\/(\d{2})\/(\d{4})$/.exec(s.trim());
  if (!m) return null;
  const d = new Date(Number(m[3]), Number(m[2]) - 1, Number(m[1]));
  return Number.isNaN(d.getTime()) ? null : d;
}

type CacheContratos = { quando: number; dados: Record<string, ContratoResumo | null> };

// chaveCacheContratos: uma chave por termo de busca (a base muda por busca).
export function chaveCacheContratos(termo: string): string {
  return `fidelidade-contratos:${termo.trim().toLowerCase()}`;
}

// lerCacheContratos: devolve os dados se o cache é válido (≤15 min), senão null.
export function lerCacheContratos(chave: string, agora = Date.now()): Record<string, ContratoResumo | null> | null {
  try {
    const bruto = localStorage.getItem(chave);
    if (!bruto) return null;
    const cache = JSON.parse(bruto) as CacheContratos;
    if (typeof cache.quando !== 'number' || typeof cache.dados !== 'object' || cache.dados === null) return null;
    if (agora - cache.quando > TTL_CONTRATOS_MS) return null;
    return cache.dados;
  } catch {
    return null;
  }
}

// gravarCacheContratos: grava com carimbo de tempo; falha silenciosa (modo
// privado etc.) — sem cache o app continua funcionando, só relê os contratos.
export function gravarCacheContratos(chave: string, dados: Record<string, ContratoResumo | null>, agora = Date.now()): void {
  try {
    const cache: CacheContratos = { quando: agora, dados };
    localStorage.setItem(chave, JSON.stringify(cache));
  } catch {
    // sem localStorage, segue sem cache
  }
}

// lerComConcorrencia: executa tarefas em pequenos lotes paralelos e devolve
// os resultados à medida que chegam (via aoConcluir). A função parar() entre
// lotes permite cancelar — devolve false quando o usuário cancelou.
export async function lerComConcorrencia<T, R>(
  itens: T[],
  limite: number,
  tarefa: (item: T) => Promise<R>,
  aoConcluir: (item: T, res: R) => void,
  parar: () => boolean,
): Promise<boolean> {
  let i = 0;
  let cancelado = false;
  const proximoLote = async () => {
    while (i < itens.length) {
      if (parar()) {
        cancelado = true;
        return;
      }
      const item = itens[i++];
      try {
        const res = await tarefa(item);
        aoConcluir(item, res);
      } catch {
        aoConcluir(item, null as unknown as R);
      }
    }
  };
  await Promise.all(Array.from({ length: Math.min(limite, itens.length) }, proximoLote));
  return !cancelado;
}
