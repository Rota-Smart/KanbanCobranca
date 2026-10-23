import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  closestCorners,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragOverEvent,
} from '@dnd-kit/core';
import {
  SortableContext,
  arrayMove,
  horizontalListSortingStrategy,
  sortableKeyboardCoordinates,
  useSortable,
} from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { useEffect, useState } from 'react';
import * as bindings from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';
import type { ResultadoCobranca, Ticket } from './wails-types';
import BuscarCliente360Dialog from './components/BuscarCliente360Dialog';
import ChatDrawer from './components/ChatDrawer';
import Configuracoes from './components/Configuracoes';
import KanbanColumn from './components/KanbanColumn';
import NegotiationModal from './components/NegotiationModal';
import type { ModoCard } from './components/TicketCard';
import { boardFromTickets, moveCard, type Board } from './kanban';

const fallbackColunas = ['Fazer contato'];

// Modo de visualização dos cards. Precedência: card > coluna > global.
// 'completo' não é modo persistente: é só abrir o modal do card.
interface ModosCards {
  global: ModoCard;
  colunas: Record<string, ModoCard>;
  cards: Record<number, ModoCard>;
}

const modosIniciais: ModosCards = { global: 'resumo', colunas: {}, cards: {} };

function modosDoStorage(): ModosCards {
  try {
    const bruto = JSON.parse(localStorage.getItem('modosCards') ?? '');
    if (bruto && typeof bruto === 'object' && (bruto.global === 'min' || bruto.global === 'resumo')) {
      return { global: bruto.global, colunas: bruto.colunas ?? {}, cards: bruto.cards ?? {} };
    }
  } catch {
    // sem valor salvo ou inválido: usa o padrão
  }
  return modosIniciais;
}

// TicketDaTela: payload do evento "atalho:tela" (Shift+F2 capturou a tela).
interface TicketDaTela {
  achou: boolean;
  fonte: string;
  ticketId: number;
  nomeCliente: string;
  codService: string;
  titulo: string;
  mensagem: string;
}

// mensagemChatDaTela devolve o briefing montado no backend (o que o ticket
// pede, interações e o que o agente deve buscar no 360). Sem código, nada.
function mensagemChatDaTela(d: TicketDaTela): string | null {
  if (d.mensagem && (d.achou || d.codService)) return d.mensagem;
  return null;
}

// Tema claro/escuro persistido; aplicado na <html> via classe 'dark'.
function temaInicial(): 'claro' | 'escuro' {
  const salvo = localStorage.getItem('tema');
  if (salvo === 'claro' || salvo === 'escuro') return salvo;
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'escuro' : 'claro';
}

// colunaDisparaCobranca: mover para cá puxa o financeiro do 360 e gera a proposta.
const colunaDisparaCobranca = 'Cobrança';

// ColunaOrdenavel torna a coluna reordenável por arraste. Só a alça do
// cabeçalho ativa o arraste (listeners nela), para não brigar com o arraste
// dos cards, que acontece em outro DndContext.
function ColunaOrdenavel(props: React.ComponentProps<typeof KanbanColumn>) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } =
    useSortable({ id: props.coluna });
  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={isDragging ? 'relative z-10 opacity-80' : 'relative'}
    >
      <KanbanColumn
        {...props}
        alca={
          <button
            ref={setActivatorNodeRef}
            {...attributes}
            {...listeners}
            aria-label={`Arrastar coluna ${props.coluna}`}
            title="Arraste para reordenar as colunas"
            className="cursor-grab active:cursor-grabbing text-slate-400 hover:text-slate-600 dark:hover:text-white leading-none select-none touch-none px-0.5 text-xs"
          >
            ⋮⋮
          </button>
        }
      />
    </div>
  );
}

export default function App() {
  const [colunas, setColunas] = useState<string[]>(fallbackColunas);
  const [board, setBoard] = useState<Board>({});
  const [carregando, setCarregando] = useState(true);
  const [erro, setErro] = useState('');
  const [toast, setToast] = useState('');
  const [aberto, setAberto] = useState<Ticket | null>(null);
  const [preCobranca, setPreCobranca] = useState<ResultadoCobranca | null>(null);
  const [buscar360Coluna, setBuscar360Coluna] = useState<string | null>(null);
  const [configAberta, setConfigAberta] = useState(false);
  const [chatAberto, setChatAberto] = useState(false);
  const [chatMsgInicial, setChatMsgInicial] = useState<string | null>(null);
  const [chatKey, setChatKey] = useState(0); // cada Shift+F2 remonta o drawer p/ reenviar
  const [tema, setTema] = useState<'claro' | 'escuro'>(temaInicial);
  const [sincronizando, setSincronizando] = useState(false);
  const [modos, setModos] = useState<ModosCards>(modosDoStorage);

  useEffect(() => {
    localStorage.setItem('modosCards', JSON.stringify(modos));
  }, [modos]);

  // modoDoCard resolve card > coluna > global.
  function modoDoCard(t: Ticket): ModoCard {
    return modos.cards[t.id] ?? modos.colunas[t.colunaAtual] ?? modos.global;
  }

  function setModoCard(t: Ticket, modo: ModoCard | 'completo') {
    if (modo === 'completo') {
      setPreCobranca(null);
      setAberto(t);
      return;
    }
    setModos((m) => {
      const cards = { ...m.cards };
      const atual = m.cards[t.id] ?? m.colunas[t.colunaAtual] ?? m.global;
      if (atual === modo) delete cards[t.id]; // clicou de volta: limpa o override
      else cards[t.id] = modo;
      return { ...m, cards };
    });
  }

  function setModoColuna(coluna: string, modo: ModoCard) {
    setModos((m) => {
      const ids = (board[coluna] ?? []).map((t) => t.id);
      const cards = { ...m.cards };
      for (const id of ids) delete cards[id]; // a decisão da coluna vence
      return { ...m, cards, colunas: { ...m.colunas, [coluna]: modo } };
    });
  }

  function setModoGlobal(modo: ModoCard) {
    setModos({ global: modo, colunas: {}, cards: {} });
  }

  // Sincroniza com o repo GitHub (banco-de-dados): envia/baixa conforme quem
  // mudou; em conflito, pergunta qual lado vence.
  async function sincronizar() {
    setSincronizando(true);
    try {
      const r = await bindings.Sincronizar();
      if (r.acao === 'conflito') {
        if (window.confirm(r.mensagem + '\n\nOK = enviar ESTE PC (sobrescreve o repo)\nCancelar = baixar do repo (sobrescreve este PC)')) {
          showToast((await bindings.SyncEnviar()).mensagem);
        } else {
          showToast((await bindings.SyncReceber()).mensagem);
        }
      } else {
        showToast(r.mensagem);
      }
    } catch (e) {
      showToast(String(e));
    } finally {
      setSincronizando(false);
    }
  }

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  useEffect(() => {
    document.documentElement.classList.toggle('dark', tema === 'escuro');
    localStorage.setItem('tema', tema);
  }, [tema]);

  useEffect(() => {
    Promise.all([bindings.GetColunas(), bindings.GetTickets()])
      .then(([cs, tickets]) => {
        setColunas(cs.length > 0 ? cs : fallbackColunas);
        setBoard(boardFromTickets(tickets, cs.length > 0 ? cs : fallbackColunas));
        setCarregando(false);
      })
      .catch((e) => {
        setErro(String(e));
        setCarregando(false);
      });
  }, []);

  // Atalhos globais do Windows: Shift+F2 (ticket na tela → chat do Groq)
  // e Shift+F3 (busca de clientes, emulando a pesquisa do site).
  useEffect(() => {
    const offTela = EventsOn('atalho:tela', (d: TicketDaTela) => {
      const msg = mensagemChatDaTela(d);
      if (msg) {
        setChatKey((k) => k + 1);
        setChatAberto(true);
        setChatMsgInicial(msg);
      } else {
        showToast(d.mensagem || 'Não reconheci um ticket na tela em foco.');
      }
    });
    const offBusca = EventsOn('atalho:busca', () => {
      setBuscar360Coluna(colunas[0] ?? fallbackColunas[0]);
    });
    return () => {
      offTela();
      offBusca();
    };
  }, [colunas]);

  function showToast(msg: string) {
    setToast(msg);
    setTimeout(() => setToast(''), 4000);
  }

  // Resultado da atualização em background: os avisos ficam aqui (nível App)
  // porque a tela de Configurações pode estar fechada quando o download
  // termina ou falha — os toasts morreriam com o unmount dela.
  useEffect(() => {
    const offOk = EventsOn('atualizacao:pronto', (d: { versao: string }) => {
      setToast(`Atualizado para a versão ${d.versao} — o app vai reiniciar.`);
    });
    const offErr = EventsOn('atualizacao:erro', (d: { mensagem: string }) => {
      setToast(d.mensagem || 'Falha na atualização.');
      setTimeout(() => setToast(''), 6000);
    });
    return () => {
      offOk();
      offErr();
    };
  }, []);

  async function recarregarBoard() {
    try {
      const [cs, tickets] = await Promise.all([bindings.GetColunas(), bindings.GetTickets()]);
      setColunas(cs.length > 0 ? cs : fallbackColunas);
      setBoard(boardFromTickets(tickets, cs.length > 0 ? cs : fallbackColunas));
    } catch {
      // silencioso: o board atual continua visível
    }
  }

  async function persistMove(id: number, coluna: string, posicao: number, snapshot: Board) {
    try {
      await bindings.MoveTicket(id, coluna, posicao);
      if (coluna === colunaDisparaCobranca) {
        await prepararCobranca(id, coluna);
      }
    } catch (e) {
      setBoard(snapshot); // reverte a tela se o backend falhar
      showToast(String(e) || 'Não foi possível mover o card.');
    }
  }

  // prepararCobranca roda o fluxo automático da coluna Cobrança e abre o
  // modal preenchido com a proposta gerada.
  async function prepararCobranca(id: number, coluna: string) {
    try {
      const res = await bindings.PrepararCobranca(id);
      setPreCobranca(res);
      setAberto(board[coluna]?.find((t) => t.id === id) ?? res.ticket);
    } catch (e) {
      showToast(String(e) || 'Não foi possível preparar a cobrança.');
    }
  }

  function colunaDe(overId: string | number): string | undefined {
    if (board[String(overId)]) return String(overId); // soltou direto na coluna
    for (const c of Object.keys(board)) {
      if (board[c].some((t) => t.id === Number(overId))) return c; // soltou sobre um card
    }
    return undefined;
  }

  function onDragOver(event: DragOverEvent) {
    const { active, over } = event;
    if (!over) return;
    const id = Number(active.id);
    const destino = colunaDe(over.id);
    if (!destino) return;
    const origem = Object.keys(board).find((c) => board[c].some((t) => t.id === id));
    if (origem && origem !== destino) {
      setBoard(moveCard(board, id, destino, board[destino].length));
    }
  }

  function onDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    if (!over) return;
    const id = Number(active.id);
    const origem = Object.keys(board).find((c) => board[c].some((t) => t.id === id));
    if (!origem) return;
    const destino = colunaDe(over.id) ?? origem;
    const posicao = board[String(over.id)]
      ? board[destino].length
      : Math.max(0, board[destino].findIndex((t) => t.id === Number(over.id)));
    const atual = board[destino].findIndex((t) => t.id === id);
    if (origem === destino && atual === posicao) return;
    const snapshot = board;
    const novo = moveCard(board, id, destino, posicao);
    setBoard(novo);
    persistMove(id, destino, novo[destino].findIndex((t) => t.id === id), snapshot);
  }

  async function novoCard(coluna: string, nome: string, tipo: string) {
    try {
      const fim = (board[coluna] ?? []).length;
      const t = await bindings.SaveTicket({
        id: 0,
        nomeCliente: nome,
        tipoTicket: tipo,
        numeroTicket: '',
        codService: '',
        endereco: '',
        telefone: '',
        colunaAtual: coluna,
        posicao: fim,
        createdAt: '',
        updatedAt: '',
      });
      setBoard((b) => moveCard({ ...b, [coluna]: [...(b[coluna] ?? [])] }, t.id, coluna, fim));
    } catch (e) {
      showToast(String(e));
    }
  }

  async function excluir(ticket: Ticket) {
    if (!window.confirm(`Excluir este card apaga também seus acordos. Isso não pode ser desfeito.`)) return;
    try {
      await bindings.DeleteTicket(ticket.id);
      setBoard((b) => {
        const coluna = ticket.colunaAtual;
        return { ...b, [coluna]: (b[coluna] ?? []).filter((t) => t.id !== ticket.id) };
      });
    } catch (e) {
      showToast(String(e));
    }
  }

  async function criarColuna() {
    const nome = window.prompt('Nome da nova coluna (até 40 caracteres):');
    if (!nome || !nome.trim()) return;
    try {
      await bindings.CriarColuna(nome.trim());
      await recarregarBoard();
    } catch (e) {
      showToast(String(e));
    }
  }

  async function renomearColuna(antigo: string, novo: string) {
    try {
      await bindings.RenomearColuna(antigo, novo);
      await recarregarBoard();
    } catch (e) {
      showToast(String(e));
    }
  }

  async function removerColuna(nomeColuna: string) {
    try {
      await bindings.RemoverColuna(nomeColuna);
      await recarregarBoard();
    } catch (e) {
      showToast(String(e));
    }
  }

  async function moverColuna(nomeColuna: string, direcao: -1 | 1) {
    const i = colunas.indexOf(nomeColuna);
    const j = i + direcao;
    if (i < 0 || j < 0 || j >= colunas.length) return;
    await gravarOrdemColunas([...colunas].map((c, k) => (k === i ? colunas[j] : k === j ? colunas[i] : c)));
  }

  // gravarOrdemColunas aplica a ordem nova na tela e persiste; se o banco
  // recusar, volta ao estado anterior recarregando.
  async function gravarOrdemColunas(nova: string[]) {
    const antiga = colunas;
    setColunas(nova); // feedback imediato
    try {
      await bindings.ReordenarColunas(nova);
    } catch (e) {
      showToast(String(e));
      setColunas(antiga);
    }
  }

  // onColunaDragEnd reordena as colunas arrastadas pela alça do cabeçalho.
  function onColunaDragEnd(e: DragEndEvent) {
    const { active, over } = e;
    if (!over || active.id === over.id) return;
    const de = colunas.indexOf(String(active.id));
    const para = colunas.indexOf(String(over.id));
    if (de < 0 || para < 0) return;
    void gravarOrdemColunas(arrayMove(colunas, de, para));
  }

  if (carregando) return <div className="p-8 text-slate-500">Carregando…</div>;
  if (erro) return <div className="p-8 text-red-600">Erro ao carregar: {erro}</div>;

  return (
    <div className="h-screen flex flex-col bg-gradient-to-br from-sky-100 via-indigo-50 to-rose-100 dark:from-slate-950 dark:via-slate-900 dark:to-indigo-950">
      <header className="px-4 py-3 flex items-center justify-between glass-soft rounded-b-2xl">
        <h1 className="font-semibold text-slate-800 dark:text-slate-100 flex items-center gap-2">
          <span className="w-2.5 h-2.5 rounded-full bg-gradient-to-br from-sky-400 to-blue-600 inline-block" aria-hidden />
          Kanban de Cobrança
        </h1>
        <div className="flex gap-2">
          <div className="flex items-center gap-0.5 rounded-lg px-1.5 bg-white/40 dark:bg-white/10 border border-white/50 dark:border-white/10" title="Tamanho dos cards: individual pode ser mudado no menu ⋯ do card">
            <button
              aria-label="Minimizar todos os cards"
              title="Minimizar todos os cards"
              className={`px-1.5 py-1 rounded text-[13px] leading-none ${modos.global === 'min' ? 'bg-sky-500/20 text-sky-600 dark:text-sky-300' : 'text-slate-500 hover:text-slate-700 dark:hover:text-white'}`}
              onClick={() => setModoGlobal('min')}
            >
              ▁
            </button>
            <button
              aria-label="Deixar todos os cards resumidos"
              title="Deixar todos os cards resumidos"
              className={`px-1.5 py-1 rounded text-[13px] leading-none ${modos.global === 'resumo' ? 'bg-sky-500/20 text-sky-600 dark:text-sky-300' : 'text-slate-500 hover:text-slate-700 dark:hover:text-white'}`}
              onClick={() => setModoGlobal('resumo')}
            >
              ▤
            </button>
          </div>
          <button
            aria-label={tema === 'escuro' ? 'Mudar para tema claro' : 'Mudar para tema escuro'}
            title={tema === 'escuro' ? 'Tema claro' : 'Tema escuro'}
            className="text-sm rounded-lg px-3 py-1.5 bg-white/40 dark:bg-white/10 hover:bg-white/70 dark:hover:bg-white/20 border border-white/50 dark:border-white/10 transition font-medium"
            onClick={() => setTema(tema === 'escuro' ? 'claro' : 'escuro')}
          >
            {tema === 'escuro' ? '☀️' : '🌙'}
          </button>
          <button
            className="text-sm rounded-lg px-3 py-1.5 bg-white/40 dark:bg-white/10 hover:bg-white/70 dark:hover:bg-white/20 border border-white/50 dark:border-white/10 transition font-medium"
            title="Buscar cliente no 360 (Shift+F3)"
            onClick={() => setBuscar360Coluna(colunas[0] ?? fallbackColunas[0])}
          >
            🔎 Buscar
          </button>
          <button
            className="text-sm rounded-lg px-3 py-1.5 bg-white/40 dark:bg-white/10 hover:bg-white/70 dark:hover:bg-white/20 border border-white/50 dark:border-white/10 transition font-medium"
            onClick={() => setChatAberto(true)}
          >
            💬 Assistente
          </button>
          <button
            className="text-sm border rounded px-3 py-1 hover:bg-slate-50 dark:hover:bg-slate-700 dark:border-slate-600 disabled:opacity-50"
            title="Sincronizar dados com o repositório (outro PC acessa os mesmos dados)"
            disabled={sincronizando}
            onClick={sincronizar}
          >
            {sincronizando ? '⟳ Sincronizando…' : '⟳ Sincronizar'}
          </button>
          <button
            className="text-sm rounded-lg px-3 py-1.5 bg-white/40 hover:bg-white/70 border border-white/50 transition font-medium"
            onClick={() => setConfigAberta(true)}
          >
            ⚙ Configurações
          </button>
        </div>
      </header>

      <DndContext
        sensors={sensors}
        collisionDetection={closestCorners}
        onDragOver={onDragOver}
        onDragEnd={onDragEnd}
      >
        <div className="flex gap-3 p-3 overflow-x-auto overscroll-x-contain flex-1">
          {/* Contexto próprio para o arraste de colunas (o de cima é dos cards). */}
          <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onColunaDragEnd}>
            <SortableContext items={colunas} strategy={horizontalListSortingStrategy}>
              {colunas.map((c) => (
                <ColunaOrdenavel
                  key={c}
                  coluna={c}
                  tickets={board[c] ?? []}
                  acordosPorTicket={{}}
                  modoDoCard={modoDoCard}
                  onModoCard={setModoCard}
                  modoColuna={modos.colunas[c] ?? modos.global}
                  onModoColuna={setModoColuna}
                  onAbrir={(t) => {
                    setPreCobranca(null);
                    setAberto(t);
                  }}
                  onExcluir={excluir}
                  onNovoCard={novoCard}
                  onBuscar360={setBuscar360Coluna}
                  onRenomear={renomearColuna}
                  onRemover={removerColuna}
                  onMover={moverColuna}
                />
              ))}
            </SortableContext>
          </DndContext>
          <button
            className="glass-soft rounded-2xl w-56 shrink-0 self-start p-3 text-sm text-slate-500 hover:text-slate-700 dark:hover:text-white font-medium text-left"
            onClick={criarColuna}
          >
            + Nova coluna
          </button>
        </div>
      </DndContext>

      {aberto && (
        <NegotiationModal
          ticket={aberto}
          preCobranca={preCobranca}
          onFechouComSave={() => {
            setAberto(null);
            setPreCobranca(null);
          }}
          onFechar={() => {
            setAberto(null);
            setPreCobranca(null);
          }}
        />
      )}

      {buscar360Coluna !== null && (
        <BuscarCliente360Dialog
          coluna={buscar360Coluna}
          onImportado={(t) => {
            setBoard((b) => ({ ...b, [t.colunaAtual]: [...(b[t.colunaAtual] ?? []), t] }));
            setBuscar360Coluna(null);
          }}
          onFechar={() => setBuscar360Coluna(null)}
        />
      )}

      {configAberta && <Configuracoes onFechar={() => setConfigAberta(false)} onToast={showToast} />}

      {chatAberto && (
        <ChatDrawer
          key={chatKey}
          onFechar={() => setChatAberto(false)}
          onMutou={recarregarBoard}
          onToast={showToast}
          mensagemInicial={chatMsgInicial}
          onConsumiuMensagem={() => setChatMsgInicial(null)}
        />
      )}

      {toast && (
        <div className="fixed bottom-4 left-1/2 -translate-x-1/2 z-[100] glass text-slate-800 dark:text-slate-100 text-sm px-4 py-2.5 rounded-xl shadow-lg">
          {toast}
        </div>
      )}
    </div>
  );
}
