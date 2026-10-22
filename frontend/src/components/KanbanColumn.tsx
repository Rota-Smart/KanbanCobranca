import { useDroppable } from '@dnd-kit/core';
import { SortableContext, verticalListSortingStrategy } from '@dnd-kit/sortable';
import { useState } from 'react';
import type { Ticket } from '../wails-types';
import TicketCard from './TicketCard';

// Ponto colorido da coluna, estilo ClickUp (uma cor por status).
const corPorColuna: Record<string, string> = {
  'Fazer contato': 'bg-sky-400',
  'Contatado': 'bg-violet-400',
  'Cobrança': 'bg-amber-400',
  'Acordo': 'bg-emerald-400',
  'Concluído': 'bg-slate-400',
};

// Seta de recolher/expandir: chevron que gira, estilo ClickUp.
function Seta({ aberta }: { aberta: boolean }) {
  return (
    <svg
      className={`seta-coluna w-4 h-4 text-slate-400 hover:text-slate-600 dark:hover:text-white ${aberta ? '' : 'rotate-180'}`}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <path d="M10 3 5 8l5 5" />
    </svg>
  );
}

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
      <div className="glass-soft rounded-2xl w-12 shrink-0 flex flex-col items-center py-3 gap-3">
        <button
          aria-label={`Expandir coluna ${coluna}`}
          onClick={() => setRecolhida(false)}
          className="p-0.5"
        >
          <Seta aberta={false} />
        </button>
        <span className="text-xs text-slate-500 [writing-mode:vertical-rl] font-medium tracking-wide">
          {coluna} ({tickets.length})
        </span>
      </div>
    );
  }

  return (
    <div
      className={`glass rounded-2xl w-72 shrink-0 flex flex-col p-2.5 transition-shadow ${
        isOver ? 'ring-2 ring-sky-400/70' : ''
      }`}
    >
      <div className="flex items-center justify-between px-1.5 pb-2.5">
        <div className="flex items-center gap-2 min-w-0">
          <span className={`w-2.5 h-2.5 rounded-full shrink-0 ${corPorColuna[coluna] ?? 'bg-slate-400'}`} aria-hidden />
          <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-200 truncate">{coluna}</h2>
          <span className="text-[11px] font-medium text-slate-500 bg-slate-500/10 rounded-full px-2 py-0.5 shrink-0">
            {tickets.length}
          </span>
        </div>
        <button
          aria-label={`Recolher coluna ${coluna}`}
          onClick={() => setRecolhida(true)}
          className="p-0.5 shrink-0"
        >
          <Seta aberta={true} />
        </button>
      </div>

      <div ref={setNodeRef} className="flex flex-col gap-2.5 flex-1 min-h-[4rem] overflow-y-auto pr-0.5">
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
          className="mt-2.5 flex flex-col gap-1.5 glass-soft rounded-xl p-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (!nome.trim()) return;
            onNovoCard(coluna, nome.trim(), tipo);
            setNome('');
            setNovoAberto(false);
          }}
        >
          <input
            className="border-0 rounded-lg px-2.5 py-1.5 text-sm bg-white/60 dark:bg-slate-800/60 focus:ring-2 focus:ring-sky-400 outline-none placeholder:text-slate-400"
            placeholder="Nome do cliente"
            maxLength={120}
            value={nome}
            onChange={(e) => setNome(e.target.value)}
            autoFocus
          />
          <select
            className="border-0 rounded-lg px-2.5 py-1.5 text-sm bg-white/60 dark:bg-slate-800/60 focus:ring-2 focus:ring-sky-400 outline-none"
            value={tipo}
            onChange={(e) => setTipo(e.target.value)}
          >
            {['Manutenção', 'Instalação', 'Suporte', 'Outro'].map((t) => (
              <option key={t}>{t}</option>
            ))}
          </select>
          <div className="flex gap-1">
            <button
              type="submit"
              className="flex-1 bg-gradient-to-r from-sky-500 to-blue-600 text-white rounded-lg px-2 py-1.5 text-sm font-medium shadow hover:brightness-110 transition"
            >
              Criar
            </button>
            <button type="button" className="text-sm text-slate-500 hover:text-slate-700 dark:hover:text-white px-2" onClick={() => setNovoAberto(false)}>
              Cancelar
            </button>
          </div>
        </form>
      ) : (
        <div className="mt-2.5 flex items-center justify-between px-1.5">
          <button
            className="text-sm text-slate-500 hover:text-slate-700 dark:hover:text-white py-1 text-left rounded font-medium focus-visible:outline focus-visible:outline-2 focus-visible:outline-sky-500"
            onClick={() => setNovoAberto(true)}
          >
            + Novo card
          </button>
          <button
            className="text-xs text-sky-600 dark:text-sky-300 hover:text-sky-700 dark:hover:text-sky-200 py-1 rounded font-medium focus-visible:outline focus-visible:outline-2 focus-visible:outline-sky-500"
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
