import { useDroppable } from '@dnd-kit/core';
import { SortableContext, verticalListSortingStrategy } from '@dnd-kit/sortable';
import { useState } from 'react';
import type { Ticket } from '../wails-types';
import TicketCard from './TicketCard';

interface Props {
  coluna: string;
  tickets: Ticket[];
  acordosPorTicket: Record<number, number>;
  onAbrir: (ticket: Ticket) => void;
  onExcluir: (ticket: Ticket) => void;
  onNovoCard: (coluna: string, nome: string, tipo: string) => void;
  onBuscar360: (coluna: string) => void;
}

export default function KanbanColumn({ coluna, tickets, acordosPorTicket, onAbrir, onExcluir, onNovoCard, onBuscar360 }: Props) {
  const { setNodeRef, isOver } = useDroppable({ id: coluna });
  const [recolhida, setRecolhida] = useState(false);
  const [novoAberto, setNovoAberto] = useState(false);
  const [nome, setNome] = useState('');
  const [tipo, setTipo] = useState('Outro');

  if (recolhida) {
    return (
      <div className="bg-slate-100 dark:bg-slate-800 rounded-xl w-12 shrink-0 flex flex-col items-center py-2 gap-2">
        <button
          aria-label={`Expandir coluna ${coluna}`}
          className="text-xs text-slate-500 hover:text-slate-700"
          onClick={() => setRecolhida(false)}
        >
          ▶
        </button>
        <span className="text-xs text-slate-500 [writing-mode:vertical-rl]">{coluna} ({tickets.length})</span>
      </div>
    );
  }

  return (
    <div className={`bg-slate-100 dark:bg-slate-800 rounded-xl w-72 shrink-0 flex flex-col p-2 ${isOver ? 'ring-2 ring-blue-400' : ''}`}>
      <div className="flex items-center justify-between px-1 pb-2">
        <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-200">
          {coluna} <span className="text-slate-400">({tickets.length})</span>
        </h2>
        <button
          aria-label={`Recolher coluna ${coluna}`}
          className="text-xs text-slate-400 hover:text-slate-600"
          onClick={() => setRecolhida(true)}
        >
          ◀
        </button>
      </div>

      <div ref={setNodeRef} className="flex flex-col gap-2 flex-1 min-h-[4rem] overflow-y-auto">
        <SortableContext items={tickets.map((t) => t.id)} strategy={verticalListSortingStrategy}>
          {tickets.map((t) => (
            <TicketCard
              key={t.id}
              ticket={t}
              qtdAcordos={acordosPorTicket[t.id]}
              onAbrir={() => onAbrir(t)}
              onExcluir={onExcluir}
            />
          ))}
        </SortableContext>
      </div>

      {novoAberto ? (
        <form
          className="mt-2 flex flex-col gap-1 bg-white dark:bg-slate-700 rounded-lg p-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (!nome.trim()) return;
            onNovoCard(coluna, nome.trim(), tipo);
            setNome('');
            setNovoAberto(false);
          }}
        >
          <input
            className="border rounded px-2 py-1 text-sm dark:bg-slate-800 dark:border-slate-600 dark:text-slate-100"
            placeholder="Nome do cliente"
            maxLength={120}
            value={nome}
            onChange={(e) => setNome(e.target.value)}
            autoFocus
          />
          <select className="border rounded px-2 py-1 text-sm dark:bg-slate-800 dark:border-slate-600 dark:text-slate-100" value={tipo} onChange={(e) => setTipo(e.target.value)}>
            {['Manutenção', 'Instalação', 'Suporte', 'Outro'].map((t) => (
              <option key={t}>{t}</option>
            ))}
          </select>
          <div className="flex gap-1">
            <button type="submit" className="flex-1 bg-blue-600 text-white rounded px-2 py-1 text-sm">
              Criar
            </button>
            <button type="button" className="text-sm text-slate-500 px-2" onClick={() => setNovoAberto(false)}>
              Cancelar
            </button>
          </div>
        </form>
      ) : (
        <div className="mt-2 flex items-center justify-between px-1">
          <button
            className="text-sm text-slate-500 hover:text-slate-700 py-1 text-left rounded focus-visible:outline focus-visible:outline-2 focus-visible:outline-blue-500"
            onClick={() => setNovoAberto(true)}
          >
            + Novo card
          </button>
          <button
            className="text-xs text-blue-600 hover:text-blue-700 py-1 rounded focus-visible:outline focus-visible:outline-2 focus-visible:outline-blue-500"
            aria-label={`Buscar cliente do 360 para a coluna ${coluna}`}
            onClick={() => onBuscar360(coluna)}
          >
            Buscar no 360
          </button>
        </div>
      )}
    </div>
  );
}
