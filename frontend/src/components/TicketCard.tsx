import { useSortable } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { useState } from 'react';
import * as bindings from '../../wailsjs/go/main/App';
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

// visitaResumo junta dia/turno/horário para o badge 📅 do card.
function visitaResumo(t: Ticket): string {
  const dia = t.visitaData ? t.visitaData.slice(8, 10) + '/' + t.visitaData.slice(5, 7) : '';
  return [dia, t.visitaTurno, t.visitaHora].filter(Boolean).join(' · ');
}

// Avisa o App que o card mudou (vínculo com o ticket 360), para o board
// atualizar sem prop drilling por coluna.
function notificarCardAtualizado(t: Ticket) {
  window.dispatchEvent(new CustomEvent('ticket-atualizado', { detail: t }));
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
  /** Abre o diálogo de agendamento de visita (coluna "Visita Agendada"). */
  onAgendarVisita?: (ticket: Ticket) => void;
}

// Card estilo app de delivery: avatar com iniciais, faixa colorida do tipo,
// código do pedido, endereço/telefone e badge de acordos.
export default function TicketCard({ ticket, qtdAcordos = 0, modo, onModo, onAbrir, onExcluir, onAgendarVisita }: Props) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: ticket.id });
  const [menu, setMenu] = useState<{ right: number; top: number } | null>(null);
  const [vinculando, setVinculando] = useState(false);
  const [entradaTicket, setEntradaTicket] = useState('');
  const [erroVinculo, setErroVinculo] = useState('');
  const cor = corPorTipo[ticket.tipoTicket] ?? corPorTipo['Outro'];

  // Abre o menu ⋯ ancorado no botão. Usa position: fixed em vez de absolute
  // porque o card tem overflow-hidden, que cortava o menu comprido.
  function abrirMenu(e: React.MouseEvent) {
    e.stopPropagation();
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    setMenu((m) => (m ? null : { right: window.innerWidth - r.right, top: r.bottom + 4 }));
  }

  // Impede que o pointerdown no botão ative o drag do card (dnd-kit), o que
  // engolia o clique e nunca deixava o menu abrir.
  function pararDrag(e: React.PointerEvent) {
    e.stopPropagation();
  }

  // Vincula (ou desvincula, com entrada vazia) o ticket do 360 ao card.
  async function vincular(entrada: string) {
    setErroVinculo('');
    try {
      const t = await bindings.VincularTicket360(ticket.id, entrada);
      notificarCardAtualizado(t);
      setVinculando(false);
      setEntradaTicket('');
    } catch (e) {
      setErroVinculo(String(e));
    }
  }

  async function abrirTicket360() {
    setErroVinculo('');
    try {
      await bindings.AbrirTicket360DoCard(ticket.ticket360Id ?? '');
    } catch (e) {
      setErroVinculo(String(e));
    }
  }

  const itemMenu =
    'block w-full text-left px-3 py-2 hover:bg-slate-500/10 text-slate-700 dark:text-slate-200';

  // Menu de opções do card, compartilhado entre os modos min/resumido.
  // Fundo sólido (o glass ficava transparente), scroll interno e backdrop
  // para fechar ao clicar fora.
  const menuEl = menu && (
    <>
      <div
        className="fixed inset-0 z-[99]"
        onPointerDown={(e) => e.stopPropagation()}
        onWheel={(e) => {
          // o backdrop engoliria o scroll: repassa para o scrollable atrás
          const alvo = (e.currentTarget as HTMLElement).parentElement?.closest('.overflow-y-auto, .overflow-x-auto, [data-scroll]');
          if (alvo) alvo.scrollTop += e.deltaY;
        }}
        onClick={(e) => {
          e.stopPropagation();
          setMenu(null);
        }}
      />
      <div
        className="fixed z-[100] rounded-xl text-xs bg-white dark:bg-slate-800 border border-slate-200/80 dark:border-slate-600/60 shadow-xl overflow-y-auto max-h-[60vh] min-w-[13rem]"
        style={{ right: menu.right, top: menu.top }}
        onPointerDown={(e) => e.stopPropagation()}
        onClick={(e) => e.stopPropagation()}
      >
        <button
          className={itemMenu}
          onClick={() => {
            setMenu(null);
            onModo(ticket, 'min');
          }}
        >
          Minimizar card
        </button>
        <button
          className={itemMenu}
          onClick={() => {
            setMenu(null);
            onModo(ticket, 'resumo');
          }}
        >
          Deixar resumido
        </button>
        <button
          className={itemMenu}
          onClick={() => {
            setMenu(null);
            onModo(ticket, 'completo');
          }}
        >
          Abrir completo
        </button>
        <button
          className={itemMenu}
          onClick={() => {
            setErroVinculo('');
            setVinculando((v) => !v);
          }}
        >
          {ticket.ticket360Numero ? `Trocar ticket 360 (agora #${ticket.ticket360Numero})` : 'Vincular ticket 360…'}
        </button>
        {vinculando && (
          <form
            className="px-3 py-2 border-t border-slate-500/10"
            onSubmit={(e) => {
              e.preventDefault();
              vincular(entradaTicket.trim());
            }}
          >
            <input
              autoFocus
              className="w-full border rounded px-2 py-1 text-xs bg-white/70 dark:bg-slate-800/70 text-slate-800 dark:text-slate-100"
              placeholder="nº, #nº, uuid ou URL do ticket"
              value={entradaTicket}
              onChange={(e) => setEntradaTicket(e.target.value)}
            />
            <p className="mt-1 text-[10px] text-slate-500">
              Cole a URL do ticket no 360, o uuid ou só o número. Vazio + Enter desvincula.
            </p>
          </form>
        )}
        {ticket.ticket360Numero && (
          <button
            className={itemMenu}
            onClick={() => {
              setMenu(null);
              vincular('');
            }}
          >
            Desvincular ticket (#{ticket.ticket360Numero})
          </button>
        )}
        {erroVinculo && (
          <p className="px-3 py-2 text-[10px] text-red-600 dark:text-red-400 max-w-[16rem] break-words">
            {erroVinculo}
          </p>
        )}
        <button
          className={itemMenu}
          onClick={() => {
            setMenu(null);
            onAgendarVisita?.(ticket);
          }}
        >
          {ticket.visitaData ? `Editar visita (${visitaResumo(ticket)})` : '📅 Agendar visita…'}
        </button>
        <button
          className="block w-full text-left px-3 py-2 hover:bg-red-500/10 text-red-600 dark:text-red-400"
          onClick={() => {
            setMenu(null);
            onExcluir(ticket);
          }}
        >
          Excluir
        </button>
      </div>
    </>
  );

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
          {ticket.ticket360Numero && (
            <span className="text-[11px] shrink-0" title={`Ticket 360 #${ticket.ticket360Numero}`}>🎫</span>
          )}
          {ticket.observacoes?.trim() && (
            <span className="text-[11px] shrink-0" title="Tem observações internas">🗒️</span>
          )}
          {ticket.visitaData && (
            <span className="text-[11px] px-1.5 rounded-full font-medium bg-orange-100/80 text-orange-800 dark:bg-orange-500/20 dark:text-orange-200 shrink-0" title={`Visita: ${visitaResumo(ticket)}`}>
              📅
            </span>
          )}
          {qtdAcordos > 0 && (
            <span className="text-[11px] px-1.5 rounded-full font-medium bg-emerald-100/80 text-emerald-800 dark:bg-emerald-500/20 dark:text-emerald-200 shrink-0">
              ✓{qtdAcordos}
            </span>
          )}
          <button
            aria-label="Opções do card"
            className="text-slate-400 hover:text-slate-600 dark:hover:text-white px-1 rounded shrink-0 touch-none"
            onPointerDown={pararDrag}
            onClick={abrirMenu}
          >
            ⋯
          </button>
        </div>
        {menuEl}
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
                className="text-slate-400 hover:text-slate-600 dark:hover:text-white px-1 rounded shrink-0 touch-none"
                onPointerDown={pararDrag}
                onClick={abrirMenu}
              >
                ⋯
              </button>
            </div>

            <div className="mt-1.5 flex flex-wrap items-center gap-1">
              <span className={`text-[11px] px-2 py-0.5 rounded-full font-medium ${cor.pill}`}>
                {ticket.tipoTicket}
              </span>
              {ticket.ticket360Numero && (
                <button
                  title={`Ticket 360 #${ticket.ticket360Numero} — clique para abrir no navegador`}
                  className="text-[11px] px-2 py-0.5 rounded-full font-medium bg-indigo-100/80 text-indigo-800 dark:bg-indigo-500/20 dark:text-indigo-200 hover:bg-indigo-200/80"
                  onClick={(e) => {
                    e.stopPropagation();
                    abrirTicket360();
                  }}
                >
                  🎫 #{ticket.ticket360Numero}
                </button>
              )}
              {qtdAcordos > 0 && (
                <span className="text-[11px] px-2 py-0.5 rounded-full font-medium bg-emerald-100/80 text-emerald-800 dark:bg-emerald-500/20 dark:text-emerald-200">
                  ✓ {qtdAcordos} acordo{qtdAcordos > 1 ? 's' : ''}
                </span>
              )}
              {ticket.observacoes?.trim() && (
                <span
                  className="text-[11px] px-2 py-0.5 rounded-full font-medium bg-slate-200/80 text-slate-700 dark:bg-slate-500/25 dark:text-slate-200"
                  title={ticket.observacoes}
                >
                  🗒️ obs
                </span>
              )}
              {ticket.visitaData && (
                <button
                  className="text-[11px] px-2 py-0.5 rounded-full font-medium bg-orange-100/80 text-orange-800 dark:bg-orange-500/20 dark:text-orange-200 hover:bg-orange-200/80"
                  title={`Visita ${visitaResumo(ticket)}${ticket.visitaEndereco ? ` — ${ticket.visitaEndereco}` : ''} — clique para editar`}
                  onClick={(e) => {
                    e.stopPropagation();
                    onAgendarVisita?.(ticket);
                  }}
                >
                  📅 {visitaResumo(ticket)}
                </button>
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

      {menuEl}
    </div>
  );
}
