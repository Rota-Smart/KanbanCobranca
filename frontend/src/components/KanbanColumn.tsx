import { useDroppable } from '@dnd-kit/core';
import { SortableContext, verticalListSortingStrategy } from '@dnd-kit/sortable';
import { useState } from 'react';
import type { Ticket } from '../wails-types';
import TicketCard, { type ModoCard } from './TicketCard';

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
  modoDoCard: (ticket: Ticket) => ModoCard;
  onModoCard: (ticket: Ticket, modo: ModoCard | 'completo') => void;
  modoColuna: ModoCard;
  onModoColuna: (coluna: string, modo: ModoCard) => void;
  onAbrir: (ticket: Ticket) => void;
  onExcluir: (ticket: Ticket) => void;
  /** Abre o diálogo de agendamento de visita (coluna "Visita Agendada"). */
  onAgendarVisita?: (ticket: Ticket) => void;
  onNovoCard: (coluna: string, nome: string, tipo: string) => void;
  onBuscar360: (coluna: string) => void;
  onRenomear: (antigo: string, novo: string) => void;
  onRemover: (coluna: string) => void;
  onMover: (coluna: string, direcao: -1 | 1) => void;
  /** Alça de arrastar a coluna (reordenação), renderizada no início do cabeçalho. */
  alca?: React.ReactNode;
  /** Contagem real de cards (quando a coluna exibe só uma fatia deles). */
  contagem?: number;
  /** Conteúdo extra entre os cards e o rodapé padrão (ex.: botão de histórico). */
  rodape?: React.ReactNode;
  /** Seleção múltipla: repassa direto aos cards. */
  selecionado?: (t: Ticket) => boolean;
  emSelecao?: boolean;
  onAlternarSelecao?: (ticket: Ticket) => void;
}

// Botãozinho de modo (minimizar/resumir) aplicado a todos os cards da coluna.
function BotaoModo({ ativo, titulo, children, onClick }: { ativo: boolean; titulo: string; children: React.ReactNode; onClick: () => void }) {
  return (
    <button
      aria-label={titulo}
      title={titulo}
      onClick={onClick}
      className={`p-0.5 rounded text-[13px] leading-none transition ${
        ativo
          ? 'bg-sky-500/20 text-sky-600 dark:text-sky-300'
          : 'text-slate-400 hover:text-slate-600 dark:hover:text-white'
      }`}
    >
      {children}
    </button>
  );
}

export default function KanbanColumn({ coluna, tickets, acordosPorTicket, modoDoCard, onModoCard, modoColuna, onModoColuna, onAbrir, onExcluir, onAgendarVisita, onNovoCard, onBuscar360, onRenomear, onRemover, onMover, alca, contagem, rodape, selecionado, emSelecao, onAlternarSelecao }: Props) {
  const { setNodeRef, isOver } = useDroppable({ id: coluna });
  const [recolhida, setRecolhida] = useState(false);
  const [novoAberto, setNovoAberto] = useState(false);
  const [nome, setNome] = useState('');
  const [tipo, setTipo] = useState('Outro');
  const [menuAberto, setMenuAberto] = useState(false);
  const [editando, setEditando] = useState(false);
  const [nomeNovo, setNomeNovo] = useState(coluna);

  function confirmarRenomear() {
    const n = nomeNovo.trim();
    setEditando(false);
    setMenuAberto(false);
    if (!n || n === coluna) return;
    onRenomear(coluna, n);
  }

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
          {coluna} ({contagem ?? tickets.length})
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
          {alca}
          <span className={`w-2.5 h-2.5 rounded-full shrink-0 ${corPorColuna[coluna] ?? 'bg-slate-400'}`} aria-hidden />
          {editando ? (
            <form
              className="flex items-center gap-1 min-w-0"
              onSubmit={(e) => {
                e.preventDefault();
                confirmarRenomear();
              }}
            >
              <input
                className="border-0 rounded-lg px-2 py-1 text-sm bg-white/70 dark:bg-slate-800/70 focus:ring-2 focus:ring-sky-400 outline-none w-40"
                value={nomeNovo}
                maxLength={40}
                onChange={(e) => setNomeNovo(e.target.value)}
                onBlur={confirmarRenomear}
                autoFocus
                onFocus={(e) => e.currentTarget.select()}
              />
            </form>
          ) : (
            <>
              <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-200 truncate">{coluna}</h2>
              <span className="text-[11px] font-medium text-slate-500 bg-slate-500/10 rounded-full px-2 py-0.5 shrink-0">
                {contagem ?? tickets.length}
              </span>
            </>
          )}
        </div>
        <div className="flex items-center gap-1 shrink-0">
          <BotaoModo ativo={modoColuna === 'min'} titulo={`Minimizar todos os cards de ${coluna}`} onClick={() => onModoColuna(coluna, 'min')}>
            ▁
          </BotaoModo>
          <BotaoModo ativo={modoColuna === 'resumo'} titulo={`Deixar resumidos todos os cards de ${coluna}`} onClick={() => onModoColuna(coluna, 'resumo')}>
            ▤
          </BotaoModo>
          <div className="relative">
            <button
              aria-label={`Opções da coluna ${coluna}`}
              title="Renomear, mover ou apagar a coluna"
              className="p-0.5 rounded text-[13px] leading-none text-slate-400 hover:text-slate-600 dark:hover:text-white"
              onClick={() => setMenuAberto((v) => !v)}
            >
              ⋯
            </button>
            {menuAberto && (
              <>
                <div className="fixed inset-0 z-40" onClick={() => setMenuAberto(false)} />
                <div className="absolute right-0 top-6 z-50 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-xl shadow-xl py-1 w-44 text-sm">
                  <button
                    className="block w-full text-left px-3 py-1.5 hover:bg-sky-500/10"
                    onClick={() => {
                      setNomeNovo(coluna);
                      setEditando(true);
                      setMenuAberto(false);
                    }}
                  >
                    ✏️ Renomear
                  </button>
                  <button
                    className="block w-full text-left px-3 py-1.5 hover:bg-sky-500/10"
                    onClick={() => {
                      setMenuAberto(false);
                      onMover(coluna, -1);
                    }}
                  >
                    ← Mover para a esquerda
                  </button>
                  <button
                    className="block w-full text-left px-3 py-1.5 hover:bg-sky-500/10"
                    onClick={() => {
                      setMenuAberto(false);
                      onMover(coluna, 1);
                    }}
                  >
                    → Mover para a direita
                  </button>
                  <button
                    className="block w-full text-left px-3 py-1.5 text-red-600 hover:bg-red-500/10"
                    onClick={() => {
                      setMenuAberto(false);
                      if (window.confirm(`Apagar a coluna "${coluna}"? Só funciona se ela estiver vazia.`))
                        onRemover(coluna);
                    }}
                  >
                    🗑 Apagar coluna
                  </button>
                </div>
              </>
            )}
          </div>
          <button
            aria-label={`Recolher coluna ${coluna}`}
            onClick={() => setRecolhida(true)}
            className="p-0.5 shrink-0"
          >
            <Seta aberta={true} />
          </button>
        </div>
      </div>

      <div ref={setNodeRef} className="flex flex-col gap-2.5 flex-1 min-h-[4rem] overflow-y-auto pr-0.5">
        <SortableContext items={tickets.map((t) => t.id)} strategy={verticalListSortingStrategy}>
          {tickets.map((t) => (
            <TicketCard
              key={t.id}
              ticket={t}
              qtdAcordos={acordosPorTicket[t.id]}
              modo={modoDoCard(t)}
              onModo={onModoCard}
              onAbrir={() => onAbrir(t)}
              onExcluir={onExcluir}
              onAgendarVisita={onAgendarVisita}
              selecionado={selecionado?.(t)}
              emSelecao={emSelecao}
              onAlternarSelecao={onAlternarSelecao}
            />
          ))}
        </SortableContext>
      </div>

      {rodape}

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
