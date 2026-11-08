import { useSortable } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import * as bindings from '../../wailsjs/go/main/App';
import type { Comissao, TentativaContato, Ticket } from '../wails-types';
import { formatCents } from '../money';

// Cores por tipo: faixa lateral + avatar, estilo card de pedido (delivery).
const corPorTipo: Record<string, { faixa: string; avatar: string; pill: string }> = {
  'Manutenção': {
    faixa: 'bg-gradient-to-b from-amber-400 to-orange-500',
    avatar: 'bg-gradient-to-br from-amber-400 to-orange-500',
    pill: 'bg-amber-400/75 text-white shadow-sm',
  },
  'Instalação': {
    faixa: 'bg-gradient-to-b from-sky-400 to-blue-500',
    avatar: 'bg-gradient-to-br from-sky-400 to-blue-500',
    pill: 'bg-teal-500/70 text-white shadow-sm',
  },
  'Suporte': {
    faixa: 'bg-gradient-to-b from-violet-400 to-purple-500',
    avatar: 'bg-gradient-to-br from-violet-400 to-purple-500',
    pill: 'bg-violet-500/65 text-white shadow-sm',
  },
  'Outro': {
    faixa: 'bg-gradient-to-b from-slate-400 to-slate-500',
    avatar: 'bg-gradient-to-br from-slate-400 to-slate-500',
    pill: 'bg-slate-500/55 text-white shadow-sm',
  },
};

function iniciais(nome: string): string {
  const partes = nome.trim().split(/\s+/).filter(Boolean);
  if (partes.length === 0) return '?';
  return ((partes[0][0] ?? '') + (partes.length > 1 ? partes[partes.length - 1][0] ?? '' : '')).toUpperCase();
}

// SeloItem é um símbolo do card (ticket, visita, acordo...): ícone curto
// exibido no card e nome completo mostrado no popover do overflow.
export interface SeloItem {
  key: string;
  icone: string;
  titulo: string;
  /** Pílula completa (modo resumido); se ausente, mostra só o ícone. */
  node?: React.ReactNode;
}

const LIMITE_SELOS = 2;

// Selos do card com overflow: mostra os 2 primeiros e, se houver mais, um
// botão "…+" que abre um popover com TODOS os selos e seus nomes (o nome
// também aparece no tooltip de cada linha e de cada ícone visível).
function Selos({ itens }: { itens: SeloItem[] }) {
  const [aberto, setAberto] = useState(false);
  const ancora = useRef<HTMLSpanElement | null>(null);
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null);

  if (itens.length === 0) return null;
  const visiveis = itens.slice(0, LIMITE_SELOS);
  const extras = itens.length - visiveis.length;

  function alternar() {
    if (aberto) {
      setAberto(false);
      return;
    }
    const r = ancora.current?.getBoundingClientRect();
    if (r) {
      setPos({
        left: Math.max(8, Math.min(r.left, window.innerWidth - 232)),
        top: Math.min(r.bottom + 4, window.innerHeight - 200),
      });
    }
    setAberto(true);
  }

  return (
    <span ref={ancora} className="relative inline-flex items-center gap-1 shrink-0">
      {visiveis.map((s) =>
        s.node ? (
          <span key={s.key}>{s.node}</span>
        ) : (
          <span key={s.key} className="text-[11px] shrink-0" title={s.titulo}>
            {s.icone}
          </span>
        )
      )}
      {extras > 0 && (
        <button
          aria-label={`Mais ${extras} selo(s) — ver todos`}
          title={`Mais ${extras} selo(s) — clique para ver todos`}
          className="text-[10px] leading-none px-1 py-0.5 rounded-full bg-slate-200/80 text-slate-600 dark:bg-slate-500/25 dark:text-slate-300 hover:bg-slate-300/80 dark:hover:bg-slate-500/40 shrink-0 touch-none"
          onPointerDown={(e) => e.stopPropagation()}
          onClick={(e) => {
            e.stopPropagation();
            alternar();
          }}
        >
          …+
        </button>
      )}
      {aberto &&
        createPortal(
          <>
            <div
              className="fixed inset-0 z-[99]"
              onPointerDown={(e) => e.stopPropagation()}
              onClick={(e) => {
                e.stopPropagation();
                setAberto(false);
              }}
            />
            <div
              className="fixed z-[100] rounded-xl text-xs bg-white dark:bg-slate-800 border border-slate-200/80 dark:border-slate-600/60 shadow-xl p-1.5 min-w-[13rem]"
              style={{ left: pos?.left ?? 8, top: pos?.top ?? 8 }}
              onPointerDown={(e) => e.stopPropagation()}
              onClick={(e) => e.stopPropagation()}
            >
              <p className="px-2 py-1 text-[10px] font-medium text-slate-400">Selos do card</p>
              {itens.map((s) =>
                s.node ? (
                  <div key={s.key} className="px-2 py-1 flex items-center gap-2" title={s.titulo}>
                    {s.node}
                  </div>
                ) : (
                  <div
                    key={s.key}
                    className="px-2 py-1 flex items-center gap-2 rounded-lg hover:bg-slate-500/10 text-slate-700 dark:text-slate-200"
                    title={s.titulo}
                  >
                    <span className="text-[13px] w-5 text-center shrink-0">{s.icone}</span>
                    <span className="min-w-0">{s.titulo}</span>
                  </div>
                )
              )}
            </div>
          </>,
          document.body
        )}
    </span>
  );
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
  /** Comissão do card (coluna Upgrade): mostra selo 💰/✅ e ações no menu. */
  comissao?: Comissao | null;
  onReceberComissao?: (ticket: Ticket) => void;
  onEditarComissao?: (ticket: Ticket) => void;
  /** Seleção múltipla: card marcado, modo seleção ativo e alternância. */
  selecionado?: boolean;
  emSelecao?: boolean;
  onAlternarSelecao?: (ticket: Ticket) => void;
}

// Botãozinho de checkbox do modo seleção múltipla. stopPropagation no
// pointerdown evita que o dnd-kit transforme o toque em arrastar do card.
function CheckSelecao({ ticket, selecionado, onAlternar }: { ticket: Ticket; selecionado: boolean; onAlternar: (t: Ticket) => void }) {
  return (
    <button
      aria-label={selecionado ? 'Desmarcar card' : 'Selecionar card'}
      title={selecionado ? 'Desmarcar card' : 'Selecionar card'}
      className={`shrink-0 w-5 h-5 rounded-md border flex items-center justify-center text-[11px] font-bold leading-none transition touch-none ${
        selecionado
          ? 'bg-indigo-500 border-indigo-500 text-white'
          : 'bg-white/70 dark:bg-slate-800/70 border-slate-400/70 dark:border-slate-500/70 text-transparent opacity-50 hover:opacity-100'
      }`}
      onPointerDown={(e) => e.stopPropagation()}
      onClick={(e) => {
        e.stopPropagation();
        onAlternar(ticket);
      }}
    >
      ✓
    </button>
  );
}

// Card estilo app de delivery: avatar com iniciais, faixa colorida do tipo,
// código do pedido, endereço/telefone e badge de acordos.
export default function TicketCard({ ticket, qtdAcordos = 0, modo, onModo, onAbrir, onExcluir, onAgendarVisita, comissao, onReceberComissao, onEditarComissao, selecionado = false, emSelecao = false, onAlternarSelecao }: Props) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: ticket.id });
  const [menu, setMenu] = useState<{ right: number; top: number } | null>(null);
  const [erroVisita, setErroVisita] = useState('');
  const [vinculando, setVinculando] = useState(false);
  const [entradaTicket, setEntradaTicket] = useState('');
  const [erroVinculo, setErroVinculo] = useState('');
  const [tentativas, setTentativas] = useState<TentativaContato[]>([]);
  const cor = corPorTipo[ticket.tipoTicket] ?? corPorTipo['Outro'];

  // Abre o menu ⋯ ancorado no botão. Usa position: fixed em vez de absolute
  // porque o card tem overflow-hidden, que cortava o menu comprido.
  function abrirMenu(e: React.MouseEvent) {
    e.stopPropagation();
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    setMenu((m) => (m ? null : { right: window.innerWidth - r.right, top: r.bottom + 4 }));
  }

  // Tentativas de contato: a lista carrega ao abrir o menu; registrar grava
  // uma tentativa agora (data automática) e devolve o card atualizado.
  useEffect(() => {
    if (!menu) return;
    bindings.ListarTentativasContato(ticket.id)
      .then((l) => setTentativas(l as TentativaContato[]))
      .catch(() => setTentativas([]));
  }, [menu, ticket.id, ticket.tentativas]);

  async function registrarTentativa(canal: string) {
    try {
      const t = await bindings.RegistrarTentativaContato(ticket.id, canal, '');
      notificarCardAtualizado(t as Ticket);
    } catch (e) {
      setErroVinculo(String(e));
    }
  }

  async function removerTentativa(id: number) {
    try {
      const t = await bindings.RemoverTentativaContato(id);
      notificarCardAtualizado(t as Ticket);
    } catch (e) {
      setErroVinculo(String(e));
    }
  }

  const menuBox = useRef<HTMLDivElement | null>(null);
  // Menu perto do rodapé abriria cortado pela janela: depois de medir,
  // sobe o suficiente para caber inteiro.
  useEffect(() => {
    const el = menuBox.current;
    if (menu && el) {
      const t = Math.min(menu.top, window.innerHeight - el.offsetHeight - 8);
      if (t > 8 && t !== menu.top) setMenu({ ...menu, top: t });
    }
  }, [menu]);

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
  // para fechar ao clicar fora. Vai em <body> via portal: o backdrop-filter
  // do .glass transforma o card no containing block do position:fixed e o
  // overflow-hidden corta o menu (flicava e sumia).
  const menuEl = menu && createPortal(
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
        ref={menuBox}
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
            onAlternarSelecao?.(ticket);
          }}
        >
          {selecionado ? 'Desmarcar card' : '☑ Selecionar (ações em massa)'}
        </button>
        <button
          className={itemMenu}
          onClick={() => {
            setMenu(null);
            onAgendarVisita?.(ticket);
          }}
        >
          {ticket.visitaData ? `Editar visita (${visitaResumo(ticket)})` : '📅 Agendar visita…'}
        </button>
        {ticket.visitaData && !ticket.visitaRealizada && (
          <button
            className="block w-full text-left px-3 py-2 hover:bg-emerald-500/10 text-emerald-700 dark:text-emerald-300"
            onClick={async () => {
              setMenu(null);
              try {
                const t = await bindings.MarcarVisitaRealizada(ticket.id);
                notificarCardAtualizado(t as Ticket);
              } catch (e) {
                setErroVisita(String(e) || 'Não foi possível marcar como realizada.');
              }
            }}
          >
            ✅ Marcar visita realizada
          </button>
        )}
        {ticket.visitaRealizada && (
          <button
            className={itemMenu}
            onClick={async () => {
              setMenu(null);
              try {
                const t = await bindings.DesmarcarVisitaRealizada(ticket.id);
                notificarCardAtualizado(t as Ticket);
              } catch (e) {
                setErroVisita(String(e) || 'Não foi possível desmarcar a visita.');
              }
            }}
          >
            ↩ Desmarcar visita realizada
          </button>
        )}
        {erroVisita && (
          <p className="px-3 py-2 text-[10px] text-red-600 dark:text-red-400 max-w-[16rem] break-words">
            {erroVisita}
          </p>
        )}
        {onEditarComissao && (
          <button
            className={itemMenu}
            onClick={() => {
              setMenu(null);
              onEditarComissao(ticket);
            }}
          >
            {comissao ? `💰 Comissão (${formatCents(comissao.comissaoCents)})…` : '💰 Comissão de upgrade…'}
          </button>
        )}
        {comissao && !comissao.recebido && onReceberComissao && (
          <button
            className="block w-full text-left px-3 py-2 hover:bg-emerald-500/10 text-emerald-700 dark:text-emerald-300"
            onClick={() => {
              setMenu(null);
              onReceberComissao(ticket);
            }}
          >
            ✅ Marcar comissão como recebida
          </button>
        )}
        <button
          className="block w-full text-left px-3 py-2 hover:bg-sky-500/10 text-sky-700 dark:text-sky-300"
          onClick={() => registrarTentativa('')}
        >
          📞 Registrar tentativa de contato
        </button>
        {tentativas.length > 0 && (
          <div className="px-3 py-1.5 border-t border-slate-500/10 text-[10px] text-slate-500 dark:text-slate-400">
            <p className="font-medium">Tentativas ({tentativas.length}) — clique para remover:</p>
            {tentativas.slice(0, 5).map((t) => (
              <button
                key={t.id}
                className="block w-full text-left py-0.5 hover:text-red-600 dark:hover:text-red-400"
                title={`Remover tentativa de ${t.createdAt.slice(8, 10)}/${t.createdAt.slice(5, 7)}${t.canal ? ` (${t.canal})` : ''}`}
                onClick={() => removerTentativa(t.id)}
              >
                {t.createdAt.slice(8, 10)}/{t.createdAt.slice(5, 7)} {t.createdAt.slice(11, 16)}
                {t.canal ? ` — ${t.canal}` : ''}
              </button>
            ))}
            {tentativas.length > 5 && <p>+ {tentativas.length - 5} mais antigas</p>}
          </div>
        )}
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
    </>, document.body);

  // Selos do card: símbolos ao lado do nome (modo min) e pílulas (resumo).
  // O 🤝 novo marca acordo de renegociação salvo (ativa ou histórico).
  const selosMin: SeloItem[] = [];
  if (ticket.ticket360Numero) {
    selosMin.push({ key: 'ticket360', icone: '🎫', titulo: `Ticket 360 #${ticket.ticket360Numero}` });
  }
  if (ticket.observacoes?.trim()) {
    selosMin.push({ key: 'obs', icone: '🗒️', titulo: 'Tem observações internas' });
  }
  if ((ticket.tentativas ?? 0) > 0) {
    selosMin.push({
      key: 'tentativas',
      icone: `📞${ticket.tentativas}`,
      titulo: `${ticket.tentativas} tentativa(s) de contato${ticket.ultimaTentativa ? ` — última: ${ticket.ultimaTentativa.slice(8, 10)}/${ticket.ultimaTentativa.slice(5, 7)}` : ''}`,
    });
  }
  if (ticket.visitaData) {
    selosMin.push({
      key: 'visita',
      icone: ticket.visitaRealizada ? '✅' : '📅',
      titulo: `Visita ${ticket.visitaRealizada ? 'realizada' : `agendada: ${visitaResumo(ticket)}`}`,
    });
  }
  if (qtdAcordos > 0) {
    selosMin.push({
      key: 'acordo',
      icone: `🤝${qtdAcordos > 1 ? qtdAcordos : ''}`,
      titulo: `Acordo de renegociação salvo (${qtdAcordos})`,
    });
  }
  if (comissao) {
    selosMin.push({
      key: 'comissao',
      icone: comissao.recebido ? '✅' : '💰',
      titulo: comissao.recebido
        ? `Comissão recebida: ${formatCents(comissao.comissaoCents)}`
        : `Comissão a receber: ${formatCents(comissao.comissaoCents)}`,
    });
  }
  const selosResumo: SeloItem[] = [];
  if (ticket.ticket360Numero) {
    selosResumo.push({
      key: 'ticket360',
      icone: '🎫',
      titulo: `Ticket 360 #${ticket.ticket360Numero} — clique para abrir no navegador`,
      node: (
        <button
          className="text-[11px] px-2 py-0.5 rounded-full font-medium bg-indigo-100/80 text-indigo-800 dark:bg-indigo-500/20 dark:text-indigo-200 hover:bg-indigo-200/80"
          onClick={(e) => {
            e.stopPropagation();
            abrirTicket360();
          }}
        >
          🎫 #{ticket.ticket360Numero}
        </button>
      ),
    });
  }
  if (qtdAcordos > 0) {
    selosResumo.push({
      key: 'acordo',
      icone: '🤝',
      titulo: `Acordo de renegociação salvo (${qtdAcordos})`,
      node: (
        <span className="text-[11px] px-2 py-0.5 rounded-full font-medium bg-emerald-100/80 text-emerald-800 dark:bg-emerald-500/20 dark:text-emerald-200">
          🤝 {qtdAcordos} acordo{qtdAcordos > 1 ? 's' : ''}
        </span>
      ),
    });
  }
  if (ticket.observacoes?.trim()) {
    selosResumo.push({
      key: 'obs',
      icone: '🗒️',
      titulo: ticket.observacoes,
      node: (
        <span className="text-[11px] px-2 py-0.5 rounded-full font-medium bg-slate-200/80 text-slate-700 dark:bg-slate-500/25 dark:text-slate-200" title={ticket.observacoes}>
          🗒️ obs
        </span>
      ),
    });
  }
  if (ticket.visitaData) {
    selosResumo.push({
      key: 'visita',
      icone: ticket.visitaRealizada ? '✅' : '📅',
      titulo: `Visita ${visitaResumo(ticket)}${ticket.visitaEndereco ? ` — ${ticket.visitaEndereco}` : ''} — clique para editar`,
      node: (
        <button
          className="text-[11px] px-2 py-0.5 rounded-full font-medium bg-orange-100/80 text-orange-800 dark:bg-orange-500/20 dark:text-orange-200 hover:bg-orange-200/80"
          onClick={(e) => {
            e.stopPropagation();
            onAgendarVisita?.(ticket);
          }}
        >
          {ticket.visitaRealizada ? '✅' : '📅'} {visitaResumo(ticket)}
        </button>
      ),
    });
  }
  if (comissao && onEditarComissao) {
    selosResumo.push({
      key: 'comissao',
      icone: comissao.recebido ? '✅' : '💰',
      titulo: comissao.recebido
        ? `Comissão recebida em ${comissao.recebidoEm} — clique para ver`
        : `Comissão a receber: ${formatCents(comissao.comissaoCents)} — clique para editar`,
      node: (
        <button
          className={`text-[11px] px-2 py-0.5 rounded-full font-medium ${comissao.recebido ? 'bg-emerald-100/80 text-emerald-800 dark:bg-emerald-500/20 dark:text-emerald-200' : 'bg-teal-100/80 text-teal-800 dark:bg-teal-500/20 dark:text-teal-200'} hover:brightness-95`}
          onClick={(e) => {
            e.stopPropagation();
            if (!comissao.recebido) onEditarComissao(ticket);
          }}
        >
          {comissao.recebido ? '✅ Recebida' : `💰 ${formatCents(comissao.comissaoCents)}`}
        </button>
      ),
    });
  }

  if (modo === 'min') {
    return (
      <div
        ref={setNodeRef}
        style={{ transform: CSS.Translate.toString(transform), transition }}
        className={`glass card-tile card-delivery rounded-2xl relative overflow-hidden text-sm cursor-pointer ${isDragging ? 'opacity-50 scale-95' : ''} ${selecionado ? 'ring-2 ring-indigo-500' : ''}`}
        {...attributes}
        {...listeners}
        onClick={onAbrir}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === 'Enter') onAbrir();
        }}
      >
        <div className="pl-3.5 pr-2 py-2.5 flex items-center gap-2.5">
          <div
            className={`shrink-0 w-7 h-7 rounded-full ${cor.avatar} text-white flex items-center justify-center text-[10px] font-bold shadow-inner select-none`}
            aria-hidden
          >
            {iniciais(ticket.nomeCliente || '?')}
          </div>
          <span className="font-semibold text-slate-800 dark:text-slate-100 truncate flex-1 min-w-0">
            {ticket.nomeCliente || 'Sem nome'}
          </span>
          <Selos itens={selosMin} />
          {(emSelecao || selecionado) && onAlternarSelecao && (
            <CheckSelecao ticket={ticket} selecionado={selecionado} onAlternar={onAlternarSelecao} />
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
      className={`glass card-tile card-delivery rounded-2xl relative overflow-hidden text-sm ${isDragging ? 'opacity-50 scale-95' : ''} ${selecionado ? 'ring-2 ring-indigo-500' : ''}`}
      {...attributes}
      {...listeners}
      onClick={onAbrir}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === 'Enter') onAbrir();
      }}
    >
      <div className="px-3.5 pt-3 pb-2.5">
        <div className="flex items-start justify-between gap-1">
          <div className="min-w-0">
            <span className="font-medium text-[15px] text-slate-800 dark:text-slate-100 break-words leading-snug block">
              {ticket.nomeCliente || 'Sem nome'}
            </span>
            {ticket.codService && (
              <span className="text-[11px] text-slate-500 font-mono">#{ticket.codService}</span>
            )}
          </div>
          <div className="flex items-center gap-1 shrink-0 -mr-0.5">
            {(emSelecao || selecionado) && onAlternarSelecao && (
              <CheckSelecao ticket={ticket} selecionado={selecionado} onAlternar={onAlternarSelecao} />
            )}
            <button
              aria-label="Opções do card"
              className="text-slate-400 hover:text-slate-700 dark:hover:text-white px-1 rounded shrink-0 touch-none"
              onPointerDown={pararDrag}
              onClick={abrirMenu}
            >
              ⋯
            </button>
          </div>
        </div>

        <div className="mt-2 flex flex-wrap items-center gap-1.5">
          <span className={`text-xs px-2.5 py-1 rounded-full font-medium ${cor.pill}`}>
            {ticket.tipoTicket}
          </span>
          <Selos itens={selosResumo} />
        </div>

        {/* Rodapé: linhas de contato à esquerda e avatar do cliente à direita */}
        <div className="mt-2.5 flex items-end justify-between gap-2">
          <div className="min-w-0 space-y-0.5">
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
          <div
            className={`relative shrink-0 w-8 h-8 rounded-full ${cor.avatar} text-white flex items-center justify-center text-[11px] font-bold ring-2 ring-white/80 dark:ring-white/20 shadow select-none`}
            aria-hidden
          >
            {iniciais(ticket.nomeCliente || '?')}
            {(ticket.tentativas ?? 0) > 0 && (
              <span className="absolute -top-0.5 -right-0.5 w-2.5 h-2.5 rounded-full bg-sky-400 ring-2 ring-white dark:ring-slate-800" />
            )}
          </div>
        </div>
      </div>

      {menuEl}
    </div>
  );
}
