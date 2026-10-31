import { describe, expect, it } from 'vitest';
import { boardFromTickets, moveCard, ordenarFechados } from './kanban';
import type { Ticket } from './wails-types';

function t(id: number, coluna: string, posicao: number): Ticket {
  return {
    id,
    nomeCliente: `Cliente ${id}`,
    tipoTicket: 'Outro',
    numeroTicket: '',
    codService: '',
    endereco: '',
    telefone: '',
    ticket360Id: '',
    ticket360Numero: '',
    observacoes: '',
    visitaData: '',
    visitaTurno: '',
    visitaHora: '',
    visitaEndereco: '',
    visitaRealizada: '',
        tentativas: 0,
    ultimaTentativa: '',
colunaAtual: coluna,
    posicao,
    createdAt: '',
    updatedAt: '',
  };
}

const colunas = ['A', 'B'];
const board = boardFromTickets([t(1, 'A', 0), t(2, 'A', 1), t(3, 'B', 0)], colunas);

describe('moveCard', () => {
  it('move entre colunas para o fim', () => {
    const novo = moveCard(board, 1, 'B', board['B'].length);
    expect(novo['A'].map((x) => x.id)).toEqual([2]);
    expect(novo['A'][0].posicao).toBe(0);
    expect(novo['B'].map((x) => x.id)).toEqual([3, 1]);
    expect(novo['B'][1].colunaAtual).toBe('B');
  });

  it('reordena dentro da mesma coluna', () => {
    const novo = moveCard(board, 2, 'A', 0);
    expect(novo['A'].map((x) => x.id)).toEqual([2, 1]);
  });

  it('remove e renumera a coluna de origem', () => {
    const novo = moveCard(board, 2, 'B', 0);
    expect(novo['A'].map((x) => x.id)).toEqual([1]);
    expect(novo['B'].map((x) => x.id)).toEqual([2, 3]);
  });

  it('não perde cards com posição fora do intervalo', () => {
    const novo = moveCard(board, 1, 'B', 99);
    expect(novo['B'].map((x) => x.id)).toEqual([3, 1]);
  });
});

describe('ordenarFechados', () => {
  const mk = (id: number, posicao: number): Ticket => ({ id, posicao } as Ticket);

  it('ordena pela ordem do histórico (mais recente primeiro)', () => {
    const ts = [mk(1, 0), mk(2, 1), mk(3, 2)];
    const hist = [{ cardId: 3 }, { cardId: 2 }, { cardId: 1 }];
    expect(ordenarFechados(ts, hist).map((t) => t.id)).toEqual([3, 2, 1]);
  });

  it('card fora do histórico (recém-arrastado) vem primeiro', () => {
    const ts = [mk(1, 0), mk(2, 1), mk(9, 2)];
    const hist = [{ cardId: 2 }, { cardId: 1 }];
    expect(ordenarFechados(ts, hist).map((t) => t.id)).toEqual([9, 2, 1]);
  });

  it('dois cards fora do histórico mantêm a ordem do board', () => {
    const ts = [mk(7, 0), mk(1, 1), mk(8, 2), mk(2, 3)];
    const hist = [{ cardId: 2 }, { cardId: 1 }];
    expect(ordenarFechados(ts, hist).map((t) => t.id)).toEqual([7, 8, 2, 1]);
  });
});
