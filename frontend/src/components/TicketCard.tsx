import { useSortable } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { useState } from 'react';
import type { Ticket } from '../wails-types';

const badgePorTipo: Record<string, string> = {
  'Manutenção': 'bg-amber-100 text-amber-800',
  'Instalação': 'bg-sky-100 text-sky-800',
  'Suporte': 'bg-violet-100 text-violet-800',
  'Outro': 'bg-slate-200 text-slate-700',
};

interface Props {
  ticket: Ticket;
  qtdAcordos?: number;
  onAbrir: () => void;
  onExcluir: (ticket: Ticket) => void;
}

export default function TicketCard({ ticket, qtdAcordos = 0, onAbrir, onExcluir }: Props) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: ticket.id });
  const [menuAberto, setMenuAberto] = useState(false);

  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={`bg-white dark:bg-slate-700 shadow hover:shadow-md rounded-lg p-3 text-sm relative ${
        isDragging ? 'opacity-50' : ''
      } focus-visible:outline focus-visible:outline-2 focus-visible:outline-blue-500`}
      {...attributes}
      {...listeners}
      onClick={onAbrir}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === 'Enter') onAbrir();
      }}
    >
      <div className="flex items-start justify-between gap-2">
        <span className="font-medium text-slate-800 dark:text-slate-100 break-words">{ticket.nomeCliente || 'Sem nome'}</span>
        <button
          aria-label="Opções do card"
          className="text-slate-400 hover:text-slate-600 px-1 rounded focus-visible:outline focus-visible:outline-2 focus-visible:outline-blue-500"
          onClick={(e) => {
            e.stopPropagation();
            setMenuAberto((v) => !v);
          }}
        >
          ⋯
        </button>
      </div>
      <div className="mt-1 flex flex-wrap items-center gap-1">
        <span className={`text-xs px-1.5 py-0.5 rounded ${badgePorTipo[ticket.tipoTicket] ?? badgePorTipo['Outro']}`}>
          {ticket.tipoTicket}
        </span>
        {ticket.codService && <span className="text-xs text-slate-500">{ticket.codService}</span>}
      </div>
      {qtdAcordos > 0 && <div className="mt-1 text-xs text-slate-500">{qtdAcordos} acordo(s)</div>}

      {menuAberto && (
        <div
          className="absolute right-1 top-7 z-10 bg-white dark:bg-slate-700 border border-slate-200 dark:border-slate-600 rounded shadow-lg text-xs"
          onClick={(e) => e.stopPropagation()}
        >
          <button
            className="block w-full text-left px-3 py-2 hover:bg-red-50 text-red-600 rounded"
            onClick={() => {
              setMenuAberto(false);
              onExcluir(ticket);
            }}
          >
            Excluir
          </button>
        </div>
      )}
    </div>
  );
}
