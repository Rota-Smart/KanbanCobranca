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
import type { Comissao, ResultadoCobranca, Ticket, Vinculo360 } from './wails-types';
import BuscarCliente360Dialog from './components/BuscarCliente360Dialog';
import BuscarTickets360Dialog from './components/BuscarTickets360Dialog';
import ChatDrawer from './components/ChatDrawer';
import Configuracoes from './components/Configuracoes';
import KanbanColumn from './components/KanbanColumn';
import MovimentarTicketDialog from './components/MovimentarTicketDialog';
import NegotiationModal from './components/NegotiationModal';
import type { ModoCard } from './components/TicketCard';
import VisitaModal from './components/VisitaModal';
import { formatCents } from './money';
import ComissaoModal from './components/ComissaoModal';
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

// Assuntos distintos dos vínculos card↔ticket, ordenados — alimenta o
// <select> do filtro. Um card pode ter vários tickets, logo vários assuntos.
function assuntosDistintos(vinculos: Vinculo360[]): string[] {
  const set = new Set<string>();
  for (const v of vinculos) {
    const a = (v.assunto ?? '').trim();
    if (a) set.add(a);
  }
  return [...set].sort((a, b) => a.localeCompare(b, 'pt-BR'));
}

// assuntosPorCard agrupa os vínculos por card para o filtro sem varrer a
// lista de vínculos a cada card renderizado.
function assuntosPorCard(vinculos: Vinculo360[]): Map<number, string[]> {
  const mapa = new Map<number, string[]>();
  for (const v of vinculos) {
    const a = (v.assunto ?? '').trim();
    if (!a) continue;
    const lista = mapa.get(v.ticketId) ?? [];
    lista.push(a);
    mapa.set(v.ticketId, lista);
  }
  return mapa;
}

// cardPassaNoFiltro: o texto casa com nome do cliente, número do ticket ou
// assunto; o assunto selecionado tem que estar entre os vínculos do card.
function cardPassaNoFiltro(t: Ticket, assuntos: string[], texto: string, assunto: string): boolean {
  if (assunto && !assuntos.some((a) => a.toLowerCase() === assunto.toLowerCase())) return false;
  const q = texto.trim().toLowerCase();
  if (!q) return true;
  const alvo = [t.nomeCliente, t.ticket360Numero, ...assuntos].join('\n').toLowerCase();
  return alvo.includes(q);
}

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

// LeituraAcesso: payload do evento "atalho:acesso" (Shift+F5 leu a tela).
interface LeituraAcesso {
  achou: boolean;
  processo: string;
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

// colunaVisitaAgendada: soltar o card nela abre o diálogo de agendamento de
// visita (data, turno, horário, endereço + link do Google Maps).
const colunaVisitaAgendada = 'Visita Agendada';

// colunaTicketFechado: destino dos cards com tickets resolvidos no 360. A
// coluna exibe só os últimos cards movidos para ela; o resto fica no histórico.
const colunaTicketFechado = 'Ticket Fechado';
const maxCardsFechadosVisiveis = 5;

// colunaUpgrade: comissionamento — soltar o card nela abre o diálogo que
// grava serviço (5% de comissão) + parcela do upgrade (75%); o botão ✅
// manda para o histórico de comissões recebidas.
const colunaUpgrade = 'Upgrade';

// Linha do histórico da coluna Ticket Fechado (binding HistoricoTicketsFechados).
interface FechadoHistorico {
  cardId: number;
  cliente: string;
  ticketNumero: string;
  movidoEm: string;
}

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
  const [visitaTicket, setVisitaTicket] = useState<Ticket | null>(null);
  const [comissaoTicket, setComissaoTicket] = useState<Ticket | null>(null);
  const [comissoes, setComissoes] = useState<Comissao[]>([]);
  const [comissoesAberto, setComissoesAberto] = useState(false);
  const [buscar360Coluna, setBuscar360Coluna] = useState<string | null>(null);
  const [buscaTicketsAberta, setBuscaTicketsAberta] = useState(false);
  const [movimentaAberta, setMovimentaAberta] = useState(false);
  const [configAberta, setConfigAberta] = useState(false);
  const [chatAberto, setChatAberto] = useState(false);
  const [chatMsgInicial, setChatMsgInicial] = useState<string | null>(null);
  const [chatKey, setChatKey] = useState(0); // cada Shift+F2 remonta o drawer p/ reenviar
  const [tema, setTema] = useState<'claro' | 'escuro'>(temaInicial);
  const [sincronizando, setSincronizando] = useState(false);
  const [modos, setModos] = useState<ModosCards>(modosDoStorage);
  const [filtroTexto, setFiltroTexto] = useState('');
  const [filtroAssunto, setFiltroAssunto] = useState('');
  const [vinculos, setVinculos] = useState<Vinculo360[]>([]);
  const [historicoFechados, setHistoricoFechados] = useState<FechadoHistorico[]>([]);
  const [historicoAberto, setHistoricoAberto] = useState(false);
  const [selecao, setSelecao] = useState<Set<number>>(new Set());
  const [destinoMassa, setDestinoMassa] = useState('');

  async function carregarHistoricoFechados() {
    try {
      setHistoricoFechados((await bindings.HistoricoTicketsFechados()) ?? []);
    } catch {
      // sem histórico (coluna nem criada) a coluna mostra o que tem
    }
  }

  async function carregarVinculos() {
    try {
      // ?? []: versão velha da bridge pode devolver null quando não há
      // vínculos — iterar null derrubava o app inteiro (tela azul).
      setVinculos((await bindings.VinculosTodos()) ?? []);
    } catch {
      // sem vínculos o filtro por assunto só fica vazio
    }
  }

  const assuntosTodos = assuntosDistintos(vinculos);
  const mapaAssuntos = assuntosPorCard(vinculos);
  const filtroAtivo = filtroTexto.trim() !== '' || filtroAssunto !== '';

  function cardVisivel(t: Ticket): boolean {
    return cardPassaNoFiltro(t, mapaAssuntos.get(t.id) ?? [], filtroTexto, filtroAssunto);
  }

  // Contagem de cards visíveis (para o rótulo do filtro).
  const totalCards = Object.values(board).reduce((n, ts) => n + ts.length, 0);
  const visiveis = Object.values(board).reduce((n, ts) => n + ts.filter(cardVisivel).length, 0);

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
    void carregarVinculos();
    void carregarHistoricoFechados();
    void carregarComissoes();
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
    const offBuscaTickets = EventsOn('atalho:busca-tickets', () => {
      setBuscaTicketsAberta(true);
    });
    // Shift+F6: movimentar ticket para coluna sem passar pela IA.
    const offMovimenta = EventsOn('atalho:movimenta', () => {
      setMovimentaAberta(true);
    });
    // Shift+F5: leitura da tela do Access (OS) — abre o chat com o briefing.
    const offAcesso = EventsOn('atalho:acesso', (d: LeituraAcesso) => {
      if (d?.mensagem && d.achou) {
        setChatKey((k) => k + 1);
        setChatAberto(true);
        setChatMsgInicial(d.mensagem);
      } else {
        showToast(d?.mensagem || 'Não consegui ler a tela em foco.');
      }
    });
    return () => {
      offTela();
      offBusca();
      offBuscaTickets();
      offMovimenta();
      offAcesso();
    };
  }, [colunas]);

  function showToast(msg: string, ms = 4000) {
    setToast(msg);
    setTimeout(() => setToast(''), ms);
  }

  // Verificação de tickets fechados: consulta o 360 e move os cards cujos
  // tickets resolveram para a coluna Ticket Fechado — sem passar pela IA.
  const [verificandoFechados, setVerificandoFechados] = useState(false);
  const [menuAcao, setMenuAcao] = useState<{ x: number; y: number } | null>(null);

  // Visitas agendadas, de qualquer data (atrasadas incluídas), mais os cards
  // que estão na coluna "Visita Agendada" sem data gravada — alimentam o botão
  // 📅 Visitas. Ordenadas pela data e depois pelo horário/turno.
  const [menuVisitas, setMenuVisitas] = useState<{ x: number; y: number } | null>(null);
  const chaveDia = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
  const hojeISO = chaveDia(new Date());
  const visitasProximas = (() => {
    const ordemTurno: Record<string, number> = { Manhã: 0, Tarde: 1, Noite: 2 };
    const itens: Ticket[] = [];
    for (const lista of Object.values(board)) {
      for (const t of lista) {
        if (t.visitaData || t.colunaAtual === colunaVisitaAgendada) itens.push(t);
      }
    }
    itens.sort((a, b) => {
      if (!a.visitaData !== !b.visitaData) return a.visitaData ? -1 : 1; // sem data por último
      if (a.visitaData !== b.visitaData) return a.visitaData < b.visitaData ? -1 : 1;
      if (a.visitaHora || b.visitaHora) return (a.visitaHora || '99:99') < (b.visitaHora || '99:99') ? -1 : 1;
      return (ordemTurno[a.visitaTurno] ?? 3) - (ordemTurno[b.visitaTurno] ?? 3);
    });
    return itens;
  })();
  function resumoVisita(t: Ticket): string {
    const dia = t.visitaData
      ? t.visitaData.slice(8, 10) + '/' + t.visitaData.slice(5, 7)
      : 'sem data';
    const quando = t.visitaHora || t.visitaTurno;
    return `${dia}${quando ? ' · ' + quando : ''} — ${t.nomeCliente}`;
  }
  const visitaAtrasada = (t: Ticket) => !!t.visitaData && t.visitaData < hojeISO;

  async function verificarFechados() {
    if (verificandoFechados) return;
    setVerificandoFechados(true);
    showToast('🔍 Conferindo tickets no 360…');
    try {
      const r = await bindings.VerificarTicketsFechados();
      if ((r.movidos?.length ?? 0) === 0) {
        showToast(`✅ ${r.verificados} ticket(s) conferido(s) — nenhum card para mover.${r.erros?.length ? ' ' + r.erros[0] : ''}`, 6000);
      } else {
        const nomes = r.movidos.map((m) => m.cliente || `card ${m.cardId}`).join(', ');
        showToast(`✅ ${r.movidos.length} card(s) movido(s) para ${r.coluna}: ${nomes}`, 8000);
        await recarregarBoard();
      }
    } catch (e) {
      showToast('⚠ Falha ao verificar tickets: ' + String(e), 6000);
    } finally {
      setVerificandoFechados(false);
    }
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

  // Card vinculado/desvinculado do ticket 360 (evento interno do TicketCard):
  // troca o card no board sem recarregar tudo.
  useEffect(() => {
    function aoAtualizar(e: Event) {
      const t = (e as CustomEvent).detail as Ticket;
      if (!t?.id) return;
      // Card aberto no modal: atualiza o snapshot também, senão a aba
      // Cliente continua mostrando o vínculo antigo.
      setAberto((a) => (a && a.id === t.id ? t : a));
      void carregarVinculos(); // vínculo pode ter mudado → atualiza o filtro
      setBoard((b) => {
        const novo: Board = {};
        for (const [c, ts] of Object.entries(b)) {
          novo[c] = ts.map((x) => (x.id === t.id ? t : x));
        }
        return novo;
      });
    }
    window.addEventListener('ticket-atualizado', aoAtualizar);
    return () => window.removeEventListener('ticket-atualizado', aoAtualizar);
  }, []);

  async function recarregarBoard() {
    try {
      const [cs, tickets] = await Promise.all([bindings.GetColunas(), bindings.GetTickets()]);
      setColunas(cs.length > 0 ? cs : fallbackColunas);
      setBoard(boardFromTickets(tickets, cs.length > 0 ? cs : fallbackColunas));
    } catch {
      // silencioso: o board atual continua visível
    }
    void carregarVinculos();
    void carregarHistoricoFechados();
  }

  async function persistMove(id: number, coluna: string, posicao: number, snapshot: Board) {
    try {
      await bindings.MoveTicket(id, coluna, posicao);
      if (coluna === colunaDisparaCobranca) {
        await prepararCobranca(id, coluna);
      }
      if (coluna === colunaVisitaAgendada) {
        abrirVisita(id, snapshot);
      }
      if (coluna === colunaUpgrade) {
        abrirComissao(id, snapshot);
      }
    } catch (e) {
      setBoard(snapshot); // reverte a tela se o backend falhar
      showToast(String(e) || 'Não foi possível mover o card.');
    }
  }

  // abrirVisita acha o card (em qualquer coluna do snapshot) e abre o
  // diálogo de agendamento de visita.
  function abrirVisita(id: number, fonte: Board) {
    for (const ts of Object.values(fonte)) {
      const t = ts.find((x) => x.id === id);
      if (t) {
        setVisitaTicket(t);
        return;
      }
    }
  }

  // abrirComissao acha o card (em qualquer coluna do snapshot) e abre o
  // diálogo de comissionamento da coluna Upgrade.
  function abrirComissao(id: number, fonte: Board) {
    for (const ts of Object.values(fonte)) {
      const t = ts.find((x) => x.id === id);
      if (t) {
        setComissaoTicket(t);
        return;
      }
    }
  }

  // receberComissao marca a comissão como recebida (vai para o histórico).
  async function receberComissao(t: Ticket) {
    try {
      await bindings.MarcarComissaoRecebida(t.id);
      await carregarComissoes();
      showToast(`✅ Comissão de ${t.nomeCliente} recebida — no histórico.`);
    } catch (e) {
      showToast(String(e) || 'Não foi possível marcar como recebida.');
    }
  }

  async function carregarComissoes() {
    try {
      setComissoes(await bindings.ListarComissoes());
    } catch {
      // silencioso: sem comissões exibidas
    }
  }

  const comissoesPorTicket = Object.fromEntries(comissoes.map((c) => [c.ticketId, c]));

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
        ticket360Id: '',
        ticket360Numero: '',
        observacoes: '',
        visitaData: '',
        visitaTurno: '',
        visitaHora: '',
        visitaEndereco: '',
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

  // ---- Seleção múltipla: ações em massa (mover, observação, excluir) ----

  function alternarSelecao(t: Ticket) {
    setSelecao((s) => {
      const n = new Set(s);
      if (n.has(t.id)) n.delete(t.id);
      else n.add(t.id);
      return n;
    });
  }

  async function moverSelecionados() {
    const ids = [...selecao];
    if (!destinoMassa || ids.length === 0) return;
    try {
      let pos = (board[destinoMassa] ?? []).length;
      for (const id of ids) {
        await bindings.MoveTicket(id, destinoMassa, pos++);
        if (destinoMassa === colunaDisparaCobranca) await prepararCobranca(id, destinoMassa);
      }
      showToast(`${ids.length} card${ids.length > 1 ? 's' : ''} movido${ids.length > 1 ? 's' : ''} para ${destinoMassa}.`);
    } catch (e) {
      showToast(String(e));
    }
    setSelecao(new Set());
    setDestinoMassa('');
    await recarregarBoard();
  }

  async function observacaoSelecionados() {
    const txt = window.prompt('Observação a adicionar em TODOS os cards selecionados:');
    if (!txt || !txt.trim()) return;
    const ids = [...selecao];
    let ok = 0;
    try {
      for (const id of ids) {
        // acrescenta à observação existente do card, não substitui
        const atual = Object.values(board).flat().find((t) => t.id === id)?.observacoes?.trim() ?? '';
        const t = await bindings.SalvarObservacoesTicket(id, atual ? `${atual}
${txt.trim()}` : txt.trim());
        window.dispatchEvent(new CustomEvent('ticket-atualizado', { detail: t }));
        ok++;
      }
      showToast(`Observação adicionada em ${ok} card${ok > 1 ? 's' : ''}.`);
    } catch (e) {
      showToast(String(e));
    }
    setSelecao(new Set());
  }

  async function excluirSelecionados() {
    const ids = [...selecao];
    if (ids.length === 0) return;
    if (!window.confirm(`Excluir ${ids.length} card${ids.length > 1 ? 's' : ''} apaga também os acordos deles. Isso não pode ser desfeito.`)) return;
    try {
      for (const id of ids) await bindings.DeleteTicket(id);
      setBoard((b) => {
        const n: Board = {};
        for (const [c, ts] of Object.entries(b)) n[c] = ts.filter((t) => !selecao.has(t.id));
        return n;
      });
      showToast(`${ids.length} card${ids.length > 1 ? 's' : ''} excluído${ids.length > 1 ? 's' : ''}.`);
    } catch (e) {
      showToast(String(e));
      await recarregarBoard();
    }
    setSelecao(new Set());
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

  // IDs de coluna são strings, de cards são números — um único DndContext
  // atende os dois (contextos aninhados não funcionam: o useSortable dos
  // cards resolveria para o contexto interno, das colunas, e o arraste dos
  // cards pararia).
  function ehColuna(id: string | number): boolean {
    return typeof id === 'string' && colunas.includes(id);
  }

  function onDragEndUnificado(event: DragEndEvent) {
    if (ehColuna(event.active.id)) onColunaDragEnd(event);
    else onDragEnd(event);
  }

  function onDragOverUnificado(event: DragOverEvent) {
    if (ehColuna(event.active.id)) return; // arraste de coluna: nada a fazer
    onDragOver(event);
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
            title="Pesquisar tickets abertos do 360 (Shift+F4)"
            onClick={() => setBuscaTicketsAberta(true)}
          >
            🎟 Tickets
          </button>
          <div className="relative">
            <button
              className={`text-sm rounded-lg px-3 py-1.5 bg-white/40 dark:bg-white/10 hover:bg-white/70 dark:hover:bg-white/20 border border-white/50 dark:border-white/10 transition font-medium ${visitasProximas.length > 0 ? 'text-sky-700 dark:text-sky-300' : ''}`}
              title="Visitas agendadas (qualquer data — atrasadas incluídas)"
              onClick={(e) => {
                const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
                setMenuVisitas((m) => (m ? null : { x: r.right, y: r.bottom + 4 }));
              }}
            >
              📅 Visitas{visitasProximas.length > 0 ? ` (${visitasProximas.length})` : ''}
            </button>
            {menuVisitas && (
              <>
                <div
                  className="fixed inset-0 z-[100]"
                  onClick={() => setMenuVisitas(null)}
                />
                <div
                  className="fixed z-[101] rounded-xl text-xs bg-white dark:bg-slate-800 border border-slate-200/80 dark:border-slate-600/60 shadow-xl overflow-y-auto max-h-[60vh] min-w-[16rem]"
                  style={{ right: window.innerWidth - menuVisitas.x, top: menuVisitas.y }}
                >
                  {visitasProximas.length === 0 ? (
                    <div className="px-3 py-2 text-slate-500 dark:text-slate-400">
                      Nenhuma visita agendada.
                    </div>
                  ) : (
                    visitasProximas.map((t) => (
                      <button
                        key={t.id}
                        className={`block w-full text-left px-3 py-2 hover:bg-slate-500/10 ${visitaAtrasada(t) ? 'text-red-600 dark:text-red-400' : 'text-slate-700 dark:text-slate-200'}`}
                        title={t.visitaEndereco || t.endereco || ''}
                        onClick={() => {
                          setMenuVisitas(null);
                          setPreCobranca(null);
                          setAberto(t);
                        }}
                      >
                        📍 {resumoVisita(t)}
                        {visitaAtrasada(t) ? ' · atrasada' : ''}
                      </button>
                    ))
                  )}
                </div>
              </>
            )}
          </div>
          <div className="relative">
            <button
              className="text-sm rounded-lg px-3 py-1.5 bg-white/40 dark:bg-white/10 hover:bg-white/70 dark:hover:bg-white/20 border border-white/50 dark:border-white/10 transition font-medium disabled:opacity-60"
              title="Ações de ticket: mover para coluna ou fechar os finalizados"
              disabled={verificandoFechados}
              onClick={(e) => {
                const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
                setMenuAcao((m) => (m ? null : { x: r.right, y: r.bottom + 4 }));
              }}
            >
              {verificandoFechados ? '🔍 Conferindo…' : '🎫 Ação'}
            </button>
            {menuAcao && (
              <>
                <div
                  className="fixed inset-0 z-[100]"
                  onClick={() => setMenuAcao(null)}
                />
                <div
                  className="fixed z-[101] rounded-xl text-xs bg-white dark:bg-slate-800 border border-slate-200/80 dark:border-slate-600/60 shadow-xl overflow-y-auto max-h-[60vh] min-w-[12rem]"
                  style={{ right: window.innerWidth - menuAcao.x, top: menuAcao.y }}
                >
                  <button
                    className="block w-full text-left px-3 py-2 hover:bg-slate-500/10 text-slate-700 dark:text-slate-200"
                    onClick={() => { setMenuAcao(null); setMovimentaAberta(true); }}
                  >
                    📥 Mover ticket para coluna…
                  </button>
                  <button
                    className="block w-full text-left px-3 py-2 hover:bg-slate-500/10 text-slate-700 dark:text-slate-200"
                    onClick={() => { setMenuAcao(null); void verificarFechados(); }}
                  >
                    ✅ Fechar finalizados (Ticket Fechado)
                  </button>
                </div>
              </>
            )}
          </div>
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

      <div className="px-4 pt-2 flex items-center gap-2 flex-wrap">
        <input
          type="search"
          value={filtroTexto}
          onChange={(e) => setFiltroTexto(e.target.value)}
          placeholder="Filtrar cards: cliente, nº ou assunto do ticket…"
          className="text-sm rounded-lg px-3 py-1.5 w-72 bg-white/40 dark:bg-white/10 border border-white/50 dark:border-white/10 placeholder:text-slate-400 dark:placeholder:text-slate-500 outline-none focus:ring-2 focus:ring-sky-400/60"
        />
        <select
          value={filtroAssunto}
          onChange={(e) => setFiltroAssunto(e.target.value)}
          className="text-sm rounded-lg px-2 py-1.5 max-w-xs bg-white/40 dark:bg-white/10 border border-white/50 dark:border-white/10 outline-none focus:ring-2 focus:ring-sky-400/60"
          title="Filtrar pelo assunto dos tickets vinculados aos cards"
        >
          <option value="">Assunto: todos</option>
          {assuntosTodos.map((a) => (
            <option key={a} value={a}>{a}</option>
          ))}
        </select>
        {filtroAtivo && (
          <>
            <span className="text-xs text-slate-500 dark:text-slate-400">
              {visiveis} de {totalCards} cards
            </span>
            <button
              className="text-xs rounded-lg px-2 py-1 bg-white/40 dark:bg-white/10 border border-white/50 dark:border-white/10 hover:bg-white/70 dark:hover:bg-white/20"
              onClick={() => {
                setFiltroTexto('');
                setFiltroAssunto('');
              }}
            >
              ✕ Limpar filtro
            </button>
          </>
        )}
      </div>

      <DndContext
        sensors={sensors}
        collisionDetection={closestCorners}
        onDragOver={onDragOverUnificado}
        onDragEnd={onDragEndUnificado}
      >
        <div className="flex gap-3 p-3 overflow-x-auto overscroll-x-contain flex-1">
          <SortableContext items={colunas} strategy={horizontalListSortingStrategy}>
              {colunas.map((c) => {
                const todosDaColuna = (board[c] ?? []).filter(cardVisivel);
                const ehFechada = c === colunaTicketFechado;
                // Coluna de fechados: só os últimos movidos ficam visíveis —
                // a ordem vem do histórico (mais recente primeiro); o que não
                // está nele vai para o fim.
                let ticketsDaColuna = todosDaColuna;
                if (ehFechada) {
                  const ordem = new Map(historicoFechados.map((h, i) => [h.cardId, i]));
                  ticketsDaColuna = [...todosDaColuna]
                    .sort((a, b) => (ordem.get(a.id) ?? Infinity) - (ordem.get(b.id) ?? Infinity))
                    .slice(0, maxCardsFechadosVisiveis);
                }
                return (
                  <ColunaOrdenavel
                    key={c}
                    coluna={c}
                    tickets={ticketsDaColuna}
                    contagem={todosDaColuna.length}
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
                    onAgendarVisita={setVisitaTicket}
                    comissoes={comissoesPorTicket}
                    onReceberComissao={receberComissao}
                    onEditarComissao={(t) => setComissaoTicket(t)}
                    selecionado={(t) => selecao.has(t.id)}
                    emSelecao={selecao.size > 0}
                    onAlternarSelecao={alternarSelecao}
                    onNovoCard={novoCard}
                    onBuscar360={setBuscar360Coluna}
                    onRenomear={renomearColuna}
                    onRemover={removerColuna}
                    onMover={moverColuna}
                    rodape={
                      ehFechada && historicoFechados.length > 0 ? (
                        <button
                          className="mt-1 w-full text-xs text-sky-600 dark:text-sky-300 hover:text-sky-700 dark:hover:text-sky-200 py-1 rounded font-medium"
                          onClick={() => setHistoricoAberto(true)}
                        >
                          🗂 Histórico de fechados ({historicoFechados.length})
                        </button>
                      ) : c === colunaUpgrade && comissoes.some((x) => x.recebido) ? (
                        <button
                          className="mt-1 w-full text-xs text-teal-600 dark:text-teal-300 hover:text-teal-700 dark:hover:text-teal-200 py-1 rounded font-medium"
                          onClick={() => setComissoesAberto(true)}
                        >
                          🧾 Comissões recebidas ({comissoes.filter((x) => x.recebido).length})
                        </button>
                      ) : undefined
                    }
                  />
                );
              })}
            </SortableContext>
          <button
            className="glass-soft rounded-2xl w-56 shrink-0 self-start p-3 text-sm text-slate-500 hover:text-slate-700 dark:hover:text-white font-medium text-left"
            onClick={criarColuna}
          >
            + Nova coluna
          </button>
        </div>
      </DndContext>

      {selecao.size > 0 && (
        <div className="fixed bottom-4 left-1/2 -translate-x-1/2 z-[95] glass rounded-2xl shadow-xl px-3 py-2 flex flex-wrap items-center gap-2 text-sm max-w-[95vw]">
          <span className="font-semibold text-slate-700 dark:text-slate-100 whitespace-nowrap">
            {selecao.size} selecionado{selecao.size > 1 ? 's' : ''}
          </span>
          <select
            value={destinoMassa}
            onChange={(e) => setDestinoMassa(e.target.value)}
            className="rounded-lg border border-slate-300/70 dark:border-slate-500/60 bg-white/80 dark:bg-slate-800/80 px-2 py-1 text-xs text-slate-700 dark:text-slate-100"
          >
            <option value="">Mover para…</option>
            {colunas.map((c) => (
              <option key={c} value={c}>{c}</option>
            ))}
          </select>
          <button
            className="rounded-lg px-2.5 py-1 text-xs font-medium bg-indigo-500/90 text-white hover:bg-indigo-500 disabled:opacity-40"
            disabled={!destinoMassa}
            onClick={moverSelecionados}
          >
            Mover
          </button>
          <button
            className="rounded-lg px-2.5 py-1 text-xs font-medium bg-slate-500/15 text-slate-700 dark:text-slate-200 hover:bg-slate-500/25"
            onClick={observacaoSelecionados}
          >
            🗒️ Observação
          </button>
          <button
            className="rounded-lg px-2.5 py-1 text-xs font-medium bg-red-500/15 text-red-600 dark:text-red-300 hover:bg-red-500/25"
            onClick={excluirSelecionados}
          >
            Excluir
          </button>
          <button
            aria-label="Limpar seleção"
            title="Limpar seleção"
            className="rounded-lg px-2 py-1 text-xs text-slate-400 hover:text-slate-600 dark:hover:text-white"
            onClick={() => { setSelecao(new Set()); setDestinoMassa(''); }}
          >
            ✕
          </button>
        </div>
      )}

      {aberto && (
        <NegotiationModal
          ticket={aberto}
          podeGerar={aberto.colunaAtual === colunaDisparaCobranca}
          preCobranca={preCobranca}
          onFechouComSave={() => {
            setAberto(null);
            setPreCobranca(null);
          }}
          onFechar={() => {
            setAberto(null);
            setPreCobranca(null);
          }}
          onAgendarVisita={(t) => setVisitaTicket(t)}
        />
      )}

      {buscar360Coluna !== null && (
        <BuscarCliente360Dialog
          coluna={buscar360Coluna}
          onImportado={(t) => {
            setBoard((b) => ({ ...b, [t.colunaAtual]: [...(b[t.colunaAtual] ?? []), t] }));
            setBuscar360Coluna(null);
            void carregarVinculos();
          }}
          onFechar={() => setBuscar360Coluna(null)}
        />
      )}

      {buscaTicketsAberta && <BuscarTickets360Dialog onFechar={() => setBuscaTicketsAberta(false)} />}

      {historicoAberto && (
        <div
          className="fixed inset-0 z-[100] bg-black/40 flex items-center justify-center p-4"
          onClick={() => setHistoricoAberto(false)}
        >
          <div
            className="bg-white dark:bg-slate-800 rounded-2xl shadow-2xl w-[26rem] max-w-full max-h-[70vh] flex flex-col"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="px-4 py-3 border-b border-slate-200 dark:border-slate-700 flex items-center justify-between shrink-0">
              <h3 className="font-semibold text-slate-800 dark:text-slate-100 text-sm">
                🗂 Histórico — {colunaTicketFechado}
              </h3>
              <button
                aria-label="Fechar histórico"
                className="text-slate-400 hover:text-slate-600 dark:hover:text-white"
                onClick={() => setHistoricoAberto(false)}
              >
                ✕
              </button>
            </div>
            <div className="overflow-y-auto p-2">
              {historicoFechados.length === 0 ? (
                <p className="text-sm text-slate-500 dark:text-slate-400 p-3">
                  Nenhum card foi movido para lá ainda.
                </p>
              ) : (
                historicoFechados.map((h) => {
                  let card: Ticket | undefined;
                  for (const ts of Object.values(board)) {
                    const achou = ts.find((x) => x.id === h.cardId);
                    if (achou) {
                      card = achou;
                      break;
                    }
                  }
                  return (
                    <button
                      key={h.cardId}
                      className="w-full text-left px-3 py-2 rounded-lg hover:bg-slate-500/10 text-sm text-slate-700 dark:text-slate-200 disabled:opacity-60"
                      disabled={!card}
                      title={card ? 'Abrir o card' : 'Este card não está mais no board'}
                      onClick={() => {
                        setHistoricoAberto(false);
                        if (card) {
                          setPreCobranca(null);
                          setAberto(card);
                        }
                      }}
                    >
                      <span className="font-medium">{h.cliente || `card ${h.cardId}`}</span>
                      {h.ticketNumero ? <span className="text-slate-500"> · #{h.ticketNumero}</span> : null}
                      <span className="block text-xs text-slate-500 dark:text-slate-400">
                        movido em {new Date(h.movidoEm).toLocaleString('pt-BR', { day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' })}
                      </span>
                    </button>
                  );
                })
              )}
            </div>
          </div>
        </div>
      )}

      {movimentaAberta && (
        <MovimentarTicketDialog
          colunas={colunas}
          onPronto={(t) => {
            // Cliente novo: card entra na coluna; cliente existente: atualiza o card.
            setBoard((b) => {
              const semEle: Board = {};
              for (const [c, ts] of Object.entries(b)) {
                semEle[c] = ts.filter((x) => x.id !== t.id);
              }
              return { ...semEle, [t.colunaAtual]: [...(semEle[t.colunaAtual] ?? []), t] };
            });
            setMovimentaAberta(false);
            void carregarVinculos();
            showToast(`#${t.ticket360Numero || 'ticket'} → ${t.nomeCliente} em "${t.colunaAtual}"`);
          }}
          onFechar={() => setMovimentaAberta(false)}
        />
      )}

      {visitaTicket && (
        <VisitaModal
          ticket={visitaTicket}
          onSalvo={(t) => {
            window.dispatchEvent(new CustomEvent('ticket-atualizado', { detail: t }));
            setVisitaTicket(null);
            showToast(t.visitaData ? `📅 Visita de ${t.nomeCliente} agendada` : 'Visita removida do card.');
          }}
          onFechar={() => setVisitaTicket(null)}
        />
      )}

      {comissaoTicket && (
        <ComissaoModal
          ticket={comissaoTicket}
          comissao={comissoesPorTicket[comissaoTicket.id] ?? null}
          onSalvo={(c) => {
            void carregarComissoes();
            // o card pode ter sido movido para Upgrade pelo backend
            void recarregarBoard();
            setComissaoTicket(null);
            showToast(`💰 Comissão de ${formatCents(c.comissaoCents)} registrada na coluna Upgrade.`);
          }}
          onFechar={() => setComissaoTicket(null)}
        />
      )}

      {comissoesAberto && (
        <div
          className="fixed inset-0 z-[100] bg-black/40 flex items-center justify-center p-4"
          onClick={() => setComissoesAberto(false)}
        >
          <div
            className="bg-white dark:bg-slate-800 rounded-2xl shadow-2xl w-[26rem] max-w-full max-h-[70vh] flex flex-col"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="px-4 py-3 border-b border-slate-200 dark:border-slate-700 flex items-center justify-between shrink-0">
              <h3 className="font-semibold text-slate-800 dark:text-slate-100 text-sm">
                🧾 Comissões — Upgrade
              </h3>
              <button
                aria-label="Fechar histórico"
                className="text-slate-400 hover:text-slate-600 dark:hover:text-white"
                onClick={() => setComissoesAberto(false)}
              >
                ✕
              </button>
            </div>
            <div className="overflow-y-auto p-2 text-xs">
              {(() => {
                const recebidas = comissoes.filter((c) => c.recebido);
                const pendentes = comissoes.filter((c) => !c.recebido);
                const nomeDe = (id: number) =>
                  Object.values(board).flat().find((t) => t.id === id)?.nomeCliente ?? `card ${id}`;
                const totalRec = recebidas.reduce((acc, c) => acc + c.comissaoCents, 0);
                const totalPend = pendentes.reduce((acc, c) => acc + c.comissaoCents, 0);
                if (comissoes.length === 0) {
                  return (
                    <p className="text-sm text-slate-500 dark:text-slate-400 p-3">
                      Nenhuma comissão registrada ainda.
                    </p>
                  );
                }
                return (
                  <>
                    {pendentes.length > 0 && (
                      <p className="px-2 pt-1 pb-0.5 font-semibold text-slate-500 dark:text-slate-400">
                        A receber — {formatCents(totalPend)}
                      </p>
                    )}
                    {pendentes.map((c) => (
                      <div key={c.id} className="px-2 py-1.5 rounded hover:bg-slate-500/5 flex items-center justify-between gap-2">
                        <span className="min-w-0 truncate text-slate-700 dark:text-slate-200" title={c.observacoes || nomeDe(c.ticketId)}>
                          ⏳ {c.dataRegistro || '—'} · {nomeDe(c.ticketId)}
                        </span>
                        <span className="font-mono text-teal-700 dark:text-teal-300 shrink-0">
                          {formatCents(c.comissaoCents)}
                        </span>
                      </div>
                    ))}
                    <p className="px-2 pt-2 pb-0.5 font-semibold text-slate-500 dark:text-slate-400">
                      Recebidas — {formatCents(totalRec)}
                    </p>
                    {recebidas.map((c) => (
                      <div key={c.id} className="px-2 py-1.5 rounded hover:bg-slate-500/5 flex items-center justify-between gap-2">
                        <span className="min-w-0 truncate text-slate-500 dark:text-slate-400" title={c.observacoes || nomeDe(c.ticketId)}>
                          ✅ {c.recebidoEm} · {nomeDe(c.ticketId)}
                        </span>
                        <span className="font-mono shrink-0">{formatCents(c.comissaoCents)}</span>
                      </div>
                    ))}
                  </>
                );
              })()}
            </div>
          </div>
        </div>
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
