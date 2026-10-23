import { useSortable } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { useState } from 'react';
import type { Ticket } from '../wails-types';

// Cores por tipo: faixa lateral + avatar, estilo card de pedido (delivery).
const corPorTipo: Record<string, { faixa: string; avatar: string; pill: string }> = {
  'Manutenção': {
    faixa: 'bg-gradient-to-b from-amber-400 to-orange-500',
    avatar: 'bg-gradient-to-br from-amber-400 to-orange-500',
    pill: 'bg-amber-100/80 text-amber-800 dark:bg-amber-500/20 dark:text-amber-200',
  },
  'Instalação': {
    faixa: 'bg-gradient-to-b from-sky-400 to-blue-500',
    avatar: 'bg-gradient-to-br from-sky-400 to-blue-500',
    pill: 'bg-sky-100/80 text-sky-800 dark:bg-sky-500/20 dark:text-sky-200',
  },
  'Suporte': {
    faixa: 'bg-gradient-to-b from-violet-400 to-purple-500',
    avatar: 'bg-gradient-to-br from-violet-400 to-purple-500',
    pill: 'bg-violet-100/80 text-violet-800 dark:bg-violet-500/20 dark:text-violet-200',
  },
  'Outro': {
    faixa: 'bg-gradient-to-b from-slate-400 to-slate-500',
    avatar: 'bg-gradient-to-br from-slate-400 to-slate-500',
    pill: 'bg-slate-200/80 text-slate-700 dark:bg-slate-500/25 dark:text-slate-200',
  },
};

function iniciais(nome: string): string {
  const partes = nome.trim().split(/\s+/).filter(Boolean);
  if (partes.length === 0) return '?';
  return ((partes[0][0] ?? '') + (partes.length > 1 ? partes[partes.length - 1][0] ?? '' : '')).toUpperCase();
}

// Modo de visualização do card: compacto (só avatar + nome) ou resumido (atual).
export type ModoCard = 'min' | 'resumo';

interface Props {
  ticket: Ticket;
  qtdAcordos?: number;
  modo: ModoCard;
  onModo: (ticket: Ticket, modo: ModoCard | 'completo') => void;
  onAbrir: () => void;
  onExcluir: (ticket: Ticket) => void;
}

// Card estilo app de delivery: avatar com iniciais, faixa colorida do tipo,
// código do pedido, endereço/telefone e badge de acordos.
export default function TicketCard({ ticket, qtdAcordos = 0, modo, onModo, onAbrir, onExcluir }: Props) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: ticket.id });
  const [menuAberto, setMenuAberto] = useState(false);
  const cor = corPorTipo[ticket.tipoTicket] ?? corPorTipo['Outro'];

  if (modo === 'min') {
    return (
      <div
        ref={setNodeRef}
        style={{ transform: CSS.Translate.toString(transform), transition }}
        className={`glass card-delivery rounded-2xl relative overflow-hidden text-sm cursor-pointer ${isDragging ? 'opacity-50 scale-95' : ''}`}
        {...attributes}
        {...listeners}
        onClick={onAbrir}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === 'Enter') onAbrir();
        }}
      >
        <div className={`absolute left-0 top-0 bottom-0 w-1.5 ${cor.faixa}`} />
        <div className="pl-4 pr-2 py-2 flex items-center gap-2.5">
          <div
            className={`shrink-0 w-7 h-7 rounded-full ${cor.avatar} text-white flex items-center justify-center text-[10px] font-bold shadow-inner select-none`}
            aria-hidden
          >
            {iniciais(ticket.nomeCliente || '?')}
          </div>
          <span className="font-semibold text-slate-800 dark:text-slate-100 truncate flex-1 min-w-0">
            {ticket.nomeCliente || 'Sem nome'}
          </span>
          {qtdAcordos > 0 && (
            <span className="text-[11px] px-1.5 rounded-full font-medium bg-emerald-100/80 text-emerald-800 dark:bg-emerald-500/20 dark:text-emerald-200 shrink-0">
              ✓{qtdAcordos}
            </span>
          )}
        </div>
      </div>
    );
  }

  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={`glass card-delivery rounded-2xl relative overflow-hidden text-sm ${isDragging ? 'opacity-50 scale-95' : ''}`}
      {...attributes}
      {...listeners}
      onClick={onAbrir}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === 'Enter') onAbrir();
      }}
    >
      {/* Faixa colorida do tipo, como a tarja do card de pedido */}
      <div className={`absolute left-0 top-0 bottom-0 w-1.5 ${cor.faixa}`} />

      <div className="pl-4 pr-2 py-3">
        <div className="flex items-start gap-2.5">
          {/* Avatar com iniciais do cliente */}
          <div
            className={`shrink-0 w-9 h-9 rounded-full ${cor.avatar} text-white flex items-center justify-center text-xs font-bold shadow-inner select-none`}
            aria-hidden
          >
            {iniciais(ticket.nomeCliente || '?')}
          </div>

          <div className="flex-1 min-w-0">
            <div className="flex items-start justify-between gap-1">
              <div className="min-w-0">
                <span className="font-semibold text-slate-800 dark:text-slate-100 break-words leading-tight block">
                  {ticket.nomeCliente || 'Sem nome'}
                </span>
                {ticket.codService && (
                  <span className="text-[11px] text-slate-500 font-mono">#{ticket.codService}</span>
                )}
              </div>
              <button
                aria-label="Opções do card"
                className="text-slate-400 hover:text-slate-600 dark:hover:text-white px-1 rounded shrink-0"
                onClick={(e) => {
                  e.stopPropagation();
                  setMenuAberto((v) => !v);
                }}
              >
                ⋯
              </button>
            </div>

            <div className="mt-1.5 flex flex-wrap items-center gap-1">
              <span className={`text-[11px] px-2 py-0.5 rounded-full font-medium ${cor.pill}`}>
                {ticket.tipoTicket}
              </span>
              {qtdAcordos > 0 && (
                <span className="text-[11px] px-2 py-0.5 rounded-full font-medium bg-emerald-100/80 text-emerald-800 dark:bg-emerald-500/20 dark:text-emerald-200">
                  ✓ {qtdAcordos} acordo{qtdAcordos > 1 ? 's' : ''}
                </span>
              )}
            </div>

            {/* Linhas de "entrega": endereço e telefone, como no card do pedido */}
            {(ticket.endereco || ticket.telefone) && (
              <div className="mt-2 space-y-0.5">
                {ticket.endereco && (
                  <div className="flex items-center gap-1 text-[11px] text-slate-500">
                    <span aria-hidden>📍</span>
                    <span className="truncate">{ticket.endereco}</span>
                  </div>
                )}
                {ticket.telefone && (
                  <div className="flex items-center gap-1 text-[11px] text-slate-500">
                    <span aria-hidden>📞</span>
                    <span className="truncate">{ticket.telefone}</span>
                  </div>
                )}
              </div>
            )}
          </div>
        </div>
      </div>

      {menuAberto && (
        <div
          className="glass absolute right-1 top-9 z-10 rounded-xl text-xs overflow-hidden"
          onClick={(e) => e.stopPropagation()}
        >
          <button
            className="block w-full text-left px-3 py-2 hover:bg-slate-500/10 text-slate-700 dark:text-slate-200"
            onClick={() => {
              setMenuAberto(false);
              onModo(ticket, 'min');
            }}
          >
            Minimizar card
          </button>
          <button
            className="block w-full text-left px-3 py-2 hover:bg-slate-500/10 text-slate-700 dark:text-slate-200"
            onClick={() => {
              setMenuAberto(false);
              onModo(ticket, 'resumo');
            }}
          >
            Deixar resumido
          </button>
          <button
            className="block w-full text-left px-3 py-2 hover:bg-slate-500/10 text-slate-700 dark:text-slate-200"
            onClick={() => {
              setMenuAberto(false);
              onModo(ticket, 'completo');
            }}
          >
            Abrir completo
          </button>
          <button
            className="block w-full text-left px-3 py-2 hover:bg-red-500/10 text-red-600 dark:text-red-400"
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
