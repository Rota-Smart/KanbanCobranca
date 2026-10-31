// Lógica pura do board, sem DOM — testável sem arrasto.
import type { Ticket } from './wails-types';

export type Board = Record<string, Ticket[]>;

export function boardFromTickets(tickets: Ticket[], colunas: string[]): Board {
  const board: Board = {};
  for (const c of colunas) board[c] = [];
  for (const t of [...tickets].sort((a, b) => a.posicao - b.posicao)) {
    (board[t.colunaAtual] ??= []).push(t);
  }
  return board;
}

export function moveCard(board: Board, id: number, colunaDestino: string, posicao: number): Board {
  const origem = Object.keys(board).find((c) => board[c].some((t) => t.id === id));
  if (!origem) return board;
  const card = board[origem].find((t) => t.id === id)!;
  const restante = board[origem].filter((t) => t.id !== id);

  const destino = origem === colunaDestino ? restante : [...board[colunaDestino]];
  const idx = Math.max(0, Math.min(posicao, destino.length));
  destino.splice(idx, 0, { ...card, colunaAtual: colunaDestino });
  const renumerado = destino.map((t, i) => ({ ...t, posicao: i }));

  const novo: Board = { ...board, [origem]: restante.map((t, i) => ({ ...t, posicao: i })), [colunaDestino]: renumerado };
  return novo;
}

// ordenarFechados põe a coluna Ticket Fechado na ordem do histórico (mais
// recente primeiro) e mostra só os últimos N. Card que ainda não está no
// histórico (acabou de ser arrastado p/ cá) vem ANTES de todos — é o mais
// recente por definição; antes ele caía pro fim da coluna até o histórico
// recarregar, sumindo do topo durante o arrasto.
export function ordenarFechados(tickets: Ticket[], historico: { cardId: number }[]): Ticket[] {
  const ordem = new Map(historico.map((h, i) => [h.cardId, i]));
  return [...tickets]
    .map((t, i) => ({ t, i }))
    .sort((a, b) => (ordem.get(a.t.id) ?? -1) - (ordem.get(b.t.id) ?? -1) || a.i - b.i)
    .map((x) => x.t);
}
