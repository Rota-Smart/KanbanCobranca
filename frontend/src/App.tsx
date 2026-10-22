import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCorners,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragOverEvent,
} from '@dnd-kit/core';
import { sortableKeyboardCoordinates } from '@dnd-kit/sortable';
import { useEffect, useState } from 'react';
import * as bindings from '../wailsjs/go/main/App';
import type { ResultadoCobranca, Ticket } from './wails-types';
import BuscarCliente360Dialog from './components/BuscarCliente360Dialog';
import ChatDrawer from './components/ChatDrawer';
import Configuracoes from './components/Configuracoes';
import KanbanColumn from './components/KanbanColumn';
import JanelaControles from './components/JanelaControles';
import NegotiationModal from './components/NegotiationModal';
import { boardFromTickets, moveCard, type Board } from './kanban';

const fallbackColunas = ['Fazer contato'];

// Tema claro/escuro persistido; aplicado na <html> via classe 'dark'.
function temaInicial(): 'claro' | 'escuro' {
  const salvo = localStorage.getItem('tema');
  if (salvo === 'claro' || salvo === 'escuro') return salvo;
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'escuro' : 'claro';
}

// colunaDisparaCobranca: mover para cá puxa o financeiro do 360 e gera a proposta.
const colunaDisparaCobranca = 'Cobrança';

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
  const [tema, setTema] = useState<'claro' | 'escuro'>(temaInicial);

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

  function showToast(msg: string) {
    setToast(msg);
    setTimeout(() => setToast(''), 4000);
  }

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

  if (carregando) return <div className="p-8 text-slate-500">Carregando…</div>;
  if (erro) return <div className="p-8 text-red-600">Erro ao carregar: {erro}</div>;

  return (
    <div className="h-screen flex flex-col bg-slate-50 dark:bg-slate-900">
      <header className="px-4 py-3 border-b bg-white dark:bg-slate-800 dark:border-slate-700 flex items-center justify-between">
        <h1 className="font-semibold dark:text-slate-100">Kanban de Cobrança</h1>
        <div className="flex gap-2">
          <button
            aria-label={tema === 'escuro' ? 'Mudar para tema claro' : 'Mudar para tema escuro'}
            title={tema === 'escuro' ? 'Tema claro' : 'Tema escuro'}
            className="text-sm border rounded px-3 py-1 hover:bg-slate-50 dark:hover:bg-slate-700 dark:border-slate-600"
            onClick={() => setTema(tema === 'escuro' ? 'claro' : 'escuro')}
          >
            {tema === 'escuro' ? '☀️' : '🌙'}
          </button>
          <button
            className="text-sm border rounded px-3 py-1 hover:bg-slate-50 dark:hover:bg-slate-700 dark:border-slate-600"
            onClick={() => setChatAberto(true)}
          >
            💬 Assistente
          </button>
          <button
            className="text-sm border rounded px-3 py-1 hover:bg-slate-50"
            onClick={() => setConfigAberta(true)}
          >
            ⚙ Configurações
          </button>
          <JanelaControles />
        </div>
      </header>

      <DndContext
        sensors={sensors}
        collisionDetection={closestCorners}
        onDragOver={onDragOver}
        onDragEnd={onDragEnd}
      >
        <div className="flex gap-3 p-3 overflow-x-auto flex-1">
          {colunas.map((c) => (
            <KanbanColumn
              key={c}
              coluna={c}
              tickets={board[c] ?? []}
              acordosPorTicket={{}}
              onAbrir={(t) => {
                setPreCobranca(null);
                setAberto(t);
              }}
              onExcluir={excluir}
              onNovoCard={novoCard}
              onBuscar360={setBuscar360Coluna}
            />
          ))}
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
          onFechar={() => setChatAberto(false)}
          onMutou={recarregarBoard}
          onToast={showToast}
        />
      )}

      {toast && (
        <div className="fixed bottom-4 left-1/2 -translate-x-1/2 bg-slate-800 text-white text-sm px-4 py-2 rounded shadow-lg">
          {toast}
        </div>
      )}
    </div>
  );
}
