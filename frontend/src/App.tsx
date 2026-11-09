import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCorners,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragOverEvent,
  type DragStartEvent,
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
import { boardFromTickets, moveCard, ordenarFechados, type Board } from './kanban';

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

// ---- Ordenação dos cards por coluna (data de adição, posição do arraste, A-Z) ----

type OrdenacaoModo = 'manual' | 'data' | 'alfabetica';
interface Ordenacao {
  modo: OrdenacaoModo;
  desc: boolean;
}

const ordenacaoPadrao: Ordenacao = { modo: 'manual', desc: false };

function ordenacoesDoStorage(): Record<string, Ordenacao> {
  try {
    const bruto = JSON.parse(localStorage.getItem('ordenacaoColunas') ?? '');
    if (bruto && typeof bruto === 'object') return bruto;
  } catch {
    // sem valor salvo ou inválido: usa o padrão
  }
  return {};
}

// Ordena uma cópia dos cards; "manual" devolve o array como está (a ordem
// vem do banco, que é onde o arraste grava a posição).
function ordenarTickets(tickets: Ticket[], o: Ordenacao): Ticket[] {
  if (o.modo === 'manual') return tickets;
  const copia = [...tickets];
  copia.sort((a, b) => {
    let r: number;
    if (o.modo === 'data') {
      r = (Date.parse(a.createdAt) || 0) - (Date.parse(b.createdAt) || 0);
    } else {
      r = a.nomeCliente.localeCompare(b.nomeCliente, 'pt-BR', { sensitivity: 'base' });
    }
    return o.desc ? -r : r;
  });
  return copia;
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
            className="cursor-grab active:cursor-grabbing text-white/50 hover:text-white leading-none select-none touch-none px-0.5 text-xs"
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
  // Quantidade de acordos salvos por card: alimenta o selo 🤝 dos cards.
  const [acordosPorTicket, setAcordosPorTicket] = useState<Record<number, number>>({});
  const [erro, setErro] = useState('');
  const [toast, setToast] = useState('');
  // Aviso de versão nova achada pelo monitor: {versao, progresso} com
  // progresso = -1 enquanto não está baixando; null = sem aviso/dispensado.
  const [avisoAtualiza, setAvisoAtualiza] = useState<{ versao: string; progresso: number } | null>(null);
  // Versão baixada e trocada no disco, esperando decisão de reinício.
  const [prontaAtualiza, setProntaAtualiza] = useState<string | null>(null);
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
  const [sincronizando, setSincronizando] = useState(false);
  const [contaEstado, setContaEstado] = useState('');
  const [modos, setModos] = useState<ModosCards>(modosDoStorage);
  const [ordenacoes, setOrdenacoes] = useState<Record<string, Ordenacao>>(ordenacoesDoStorage);
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

  // setOrdenacaoColuna grava a ordenação da coluna (modo + direção) e persiste.
  function setOrdenacaoColuna(coluna: string, modo: OrdenacaoModo, desc: boolean) {
    setOrdenacoes((o) => {
      const n = { ...o, [coluna]: { modo, desc } };
      localStorage.setItem('ordenacaoColunas', JSON.stringify(n));
      return n;
    });
  }

  function ordenacaoDe(coluna: string): Ordenacao {
    return ordenacoes[coluna] ?? ordenacaoPadrao;
  }

  // modoDoCard resolve card > coluna > global.
  function modoDoCard(t: Ticket): ModoCard {
    return modos.cards[t.id] ?? modos.colunas[t.colunaAtual] ?? modos.global;
  }

  // abrirCardPorId acha o card no board atual; se o quadro estiver velho
  // (card criado por outra sessão/importação), recarrega do banco e tenta de
  // novo. Nulo = card não existe mais.
  async function abrirCardPorId(id: number): Promise<Ticket | null> {
    for (const ts of Object.values(board)) {
      const t = ts.find((x) => x.id === id);
      if (t) return t;
    }
    try {
      const bd = boardFromTickets(await bindings.GetTickets(), colunas);
      const t = abrirCardPorIdEm(bd, id);
      if (t) setBoard(bd);
      return t;
    } catch {
      return null;
    }
  }

  function abrirCardPorIdEm(fonte: Board, id: number): Ticket | null {
    for (const ts of Object.values(fonte)) {
      const t = ts.find((x) => x.id === id);
      if (t) return t;
    }
    return null;
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
    // Visual único em azul escuro: o tema escuro fica sempre ligado.
    document.documentElement.classList.add('dark');
  }, []);

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
        if (t.visitaRealizada) continue; // realizadas saem do aviso de visitas
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
        const falhas = r.erros?.length ? ` ⚠ ${r.erros.length} falha(s): ${r.erros[0]}` : '';
        showToast(`✅ ${r.movidos.length} card(s) movido(s) para ${r.coluna}: ${nomes}${falhas}`, 8000);
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
    // Restaura pendência de reinício ("Depois" + fechou/reabriu): só apaga se
    // este app JÁ for a versão pendente (reabrir = exe já trocado = atualizado).
    // Antes apagava sem conferir — o botão de reiniciar em Configurações
    // nunca mais aparecia depois de reabrir o app com pendência ativa.
    bindings.VersaoInstalada().then((versaoAtual: string) => {
      const pendente = localStorage.getItem('atualizacaoPronta');
      if (!pendente) return;
      if (pendente === versaoAtual) localStorage.removeItem('atualizacaoPronta');
      else setProntaAtualiza(pendente);
    }).catch(() => {});
    const offOk = EventsOn('atualizacao:pronto', (d: { versao: string }) => {
      setAvisoAtualiza(null);
      // Instalada no disco; o reinício é decisão do usuário. Se ele fechar
      // o app sem reiniciar, na próxima abertura já é a versão nova.
      localStorage.setItem('atualizacaoPronta', d.versao);
      setProntaAtualiza(d.versao);
    });
    const offErr = EventsOn('atualizacao:erro', (d: { mensagem: string }) => {
      setToast(d.mensagem || 'Falha na atualização.');
      setTimeout(() => setToast(''), 6000);
    });
    const offDisp = EventsOn('atualizacao:disponivel', (d: { versao: string }) => {
      setAvisoAtualiza({ versao: d.versao, progresso: -1 });
      // Baixa sozinho em background — sem clicar em nada.
      bindings.InstalarAtualizacao().catch(() => {});
    });
    const offProg = EventsOn('atualizacao:progresso', (d: { percento: number }) => {
      setAvisoAtualiza((a) => (a ? { ...a, progresso: d.percento } : a));
    });
    // "Abra o card do cliente X" no chat: o backend resolveu o card e pede a
    // abertura do modal completo por evento (o board pode estar desatualizado
    // em relação ao banco, então se não achar aqui, recarrega e tenta de novo).
    const offAbrir = EventsOn('chat:abrir-card', (id: number) => {
      abrirCardPorId(id).then((t) => {
        if (t) {
          // O drawer do chat é renderizado depois do modal (mesmo z-40) e
          // ficaria por cima — fecha para o card aparecer na tela. A conversa
          // fica salva e volta pelo botão do chat.
          setChatAberto(false);
          setAberto(t);
        } else {
          showToast(`Card #${id} não encontrado no quadro visível.`);
        }
      });
    });
    return () => {
      offOk();
      offErr();
      offDisp();
      offProg();
      offAbrir();
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
    // Acordo salvo/alterado atualiza o selo 🤝 junto (onSalvou passa aqui).
    bindings.ContagemAcordos()
      .then((m) => setAcordosPorTicket(m as Record<number, number>))
      .catch(() => {});
  }

  useEffect(() => {
    bindings.ContaStatus().then((st) => setContaEstado(st?.estado || '')).catch(() => {});
    const offSync = EventsOn('conta:sync', (st: { estado?: string }) => {
      setContaEstado(st?.estado || '');
    });
    const offQuadro = EventsOn('conta:quadro', () => {
      void recarregarBoard();
      void carregarComissoes();
    });
    return () => {
      offSync();
      offQuadro();
    };
  }, []);

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
      // Ticket Fechado mostra o card no topo já durante o arrasto.
      const pos = destino === colunaTicketFechado ? 0 : board[destino].length;
      setBoard(moveCard(board, id, destino, pos));
    }
  }

  function onDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    if (!over) return;
    const id = Number(active.id);
    const origem = Object.keys(board).find((c) => board[c].some((t) => t.id === id));
    if (!origem) return;
    const destino = colunaDe(over.id) ?? origem;
    // Ticket Fechado: entra sempre no topo (o mais antigo da lista visível
    // sai pelo botão de histórico, que mostra só os últimos movidos).
    if (destino === colunaTicketFechado) {
      const snapshot = board;
      const novo = moveCard(board, id, destino, 0);
      setBoard(novo);
      persistMove(id, destino, 0, snapshot);
      void carregarHistoricoFechados();
      return;
    }
    // Coluna com ordenação ativa (data/A-Z): a ordem exibida não é a posição
    // salva, então arrastar dentro dela só anexa ao fim — sem reordenar no meio.
    if (ordenacaoDe(destino).modo !== 'manual') {
      if (origem === destino) return;
      const fim = board[destino].length;
      const snapshot = board;
      const novo = moveCard(board, id, destino, fim);
      setBoard(novo);
      persistMove(id, destino, novo[destino].findIndex((t) => t.id === id), snapshot);
      return;
    }
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
        visitaRealizada: '',
                tentativas: 0,
        ultimaTentativa: '',
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

  // onDragStart guarda o board antes do arrasto: o onDragOver teleporta o card
  // para a coluna de destino na tela ANTES de salvar no banco — se o soltar não
  // vira movimento válido (soltou fora das colunas, Esc, arrasto abortado), a
  // tela precisa voltar ao estado original, senão o card parece movido e some
  // do lugar quando o board recarrega (voltando "sozinho" pra coluna anterior).
  const [boardPreArrasto, setBoardPreArrasto] = useState<Board | null>(null);

  function onDragStartUnificado(event: DragStartEvent) {
    if (!ehColuna(event.active.id)) setBoardPreArrasto(board);
  }

  function descartarArrasto() {
    if (boardPreArrasto) setBoard(boardPreArrasto);
    setBoardPreArrasto(null);
  }

  function onDragCancelUnificado() {
    descartarArrasto();
  }

  function onDragEndUnificado(event: DragEndEvent) {
    if (ehColuna(event.active.id)) {
      onColunaDragEnd(event);
      return;
    }
    // Sem destino válido (soltou fora de coluna/card): desfaz o teleport do
    // onDragOver — nada foi salvo no banco.
    if (!event.over) {
      descartarArrasto();
      return;
    }
    setBoardPreArrasto(null);
    onDragEnd(event);
  }

  function onDragOverUnificado(event: DragOverEvent) {
    if (ehColuna(event.active.id)) return; // arraste de coluna: nada a fazer
    onDragOver(event);
  }

  if (carregando) return <div className="p-8 text-white/80">Carregando…</div>;
  if (erro) return <div className="p-8 text-red-600">Erro ao carregar: {erro}</div>;

  return (
    <div className="h-screen flex gap-3 p-3 text-slate-800 dark:text-slate-100">
      {/* Barra lateral de ícones (rail), como na referência: atalhos das
          ferramentas que antes ficavam enfileiradas no header. */}
      <aside className="w-16 shrink-0 rounded-3xl flex flex-col items-center py-4 gap-2 bg-white/10 dark:bg-white/5 border border-white/20 dark:border-white/10 backdrop-blur-xl shadow-[0_12px_40px_rgba(10,16,60,0.3)]">
        <div
          className="w-9 h-9 rounded-2xl bg-gradient-to-br from-white/90 to-indigo-200/80 shadow-inner mb-3 flex items-center justify-center text-[11px] font-black text-indigo-700 select-none"
          title="RotaSuite"
          aria-hidden
        >
          RS
        </div>
        <button className="rail-btn ativo" title="Quadro" aria-label="Quadro">
          ▦
        </button>
        <button className="rail-btn" title="Assistente (chat)" aria-label="Assistente" onClick={() => setChatAberto(true)}>
          💬
        </button>
        <button
          className="rail-btn"
          title="Buscar cliente no 360 (Shift+F3)"
          aria-label="Buscar cliente no 360"
          onClick={() => setBuscar360Coluna(colunas[0] ?? fallbackColunas[0])}
        >
          🔎
        </button>
        <button
          className="rail-btn"
          title="Pesquisar tickets abertos do 360 (Shift+F4)"
          aria-label="Pesquisar tickets do 360"
          onClick={() => setBuscaTicketsAberta(true)}
        >
          🎟
        </button>
        <button
          className="rail-btn"
          title={`Visitas agendadas (qualquer data — atrasadas incluídas)${visitasProximas.length > 0 ? ` — ${visitasProximas.length}` : ''}`}
          aria-label="Visitas agendadas"
          onClick={(e) => {
            const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
            setMenuVisitas((m) => (m ? null : { x: r.right + 8, y: r.top }));
          }}
        >
          📅
          {visitasProximas.length > 0 && (
            <span className="absolute -top-0.5 -right-0.5 min-w-[1.1rem] h-[1.1rem] px-1 rounded-full bg-rose-500 text-white text-[10px] font-bold flex items-center justify-center shadow">
              {visitasProximas.length}
            </span>
          )}
        </button>
        <button
          className="rail-btn disabled:opacity-60"
          title={verificandoFechados ? 'Conferindo tickets…' : 'Ações de ticket: mover para coluna ou fechar os finalizados'}
          aria-label="Ações de ticket"
          disabled={verificandoFechados}
          onClick={(e) => {
            const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
            setMenuAcao((m) => (m ? null : { x: r.right + 8, y: r.top }));
          }}
        >
          {verificandoFechados ? '🔍' : '🎫'}
        </button>
        <div className="flex-1" />
        <button
          className="rail-btn disabled:opacity-50"
          title={sincronizando ? 'Sincronizando…' : 'Sincronizar dados com o repositório (outro PC acessa os mesmos dados)'}
          aria-label="Sincronizar"
          disabled={sincronizando}
          onClick={sincronizar}
        >
          <span className={sincronizando ? 'animate-spin inline-block' : ''}>⟳</span>
        </button>
        <button className="rail-btn" title="Configurações" aria-label="Configurações" onClick={() => setConfigAberta(true)}>
          ⚙
        </button>
      </aside>

      {/* Menus abertos pelos botões do rail: abrem à direita do botão. */}
      {menuVisitas && (
        <>
          <div className="fixed inset-0 z-[100]" onClick={() => setMenuVisitas(null)} />
          <div
            className="fixed z-[101] rounded-2xl text-xs bg-white/95 dark:bg-slate-800/95 backdrop-blur-xl border border-white/60 dark:border-slate-600/60 shadow-2xl overflow-y-auto max-h-[60vh] min-w-[16rem] py-1"
            style={{ left: menuVisitas.x, top: Math.min(menuVisitas.y, window.innerHeight - 200) }}
          >
            <p className="px-3 pt-1.5 pb-1 text-[10px] font-semibold uppercase tracking-wider text-slate-400">Visitas agendadas</p>
            {visitasProximas.length === 0 ? (
              <div className="px-3 py-2 text-slate-500 dark:text-slate-400">Nenhuma visita agendada.</div>
            ) : (
              visitasProximas.map((t) => (
                <div key={t.id} className="flex items-stretch">
                  <button
                    className={`flex-1 text-left px-3 py-2 hover:bg-slate-500/10 ${visitaAtrasada(t) && !t.visitaRealizada ? 'text-red-600 dark:text-red-400' : t.visitaRealizada ? 'text-emerald-700 dark:text-emerald-300' : 'text-slate-700 dark:text-slate-200'}`}
                    title={t.visitaEndereco || t.endereco || ''}
                    onClick={() => {
                      setMenuVisitas(null);
                      setPreCobranca(null);
                      setAberto(t);
                    }}
                  >
                    {t.visitaRealizada ? '✅' : '📍'} {resumoVisita(t)}
                    {visitaAtrasada(t) && !t.visitaRealizada ? ' · atrasada' : ''}
                  </button>
                  {!t.visitaRealizada && (
                    <button
                      className="px-2 text-emerald-600 dark:text-emerald-400 hover:bg-emerald-500/10"
                      title="Marcar visita como realizada"
                      onClick={async () => {
                        try {
                          const u = await bindings.MarcarVisitaRealizada(t.id);
                          const atual = u as Ticket;
                          setBoard((b) => {
                            const n: Board = {};
                            for (const [c, ts] of Object.entries(b)) n[c] = ts.map((x) => (x.id === atual.id ? atual : x));
                            return n;
                          });
                          showToast(`Visita de ${t.nomeCliente} marcada como realizada.`);
                        } catch (e) {
                          showToast(String(e));
                        }
                      }}
                    >
                      ✅
                    </button>
                  )}
                </div>
              ))
            )}
          </div>
        </>
      )}
      {menuAcao && (
        <>
          <div className="fixed inset-0 z-[100]" onClick={() => setMenuAcao(null)} />
          <div
            className="fixed z-[101] rounded-2xl text-xs bg-white/95 dark:bg-slate-800/95 backdrop-blur-xl border border-white/60 dark:border-slate-600/60 shadow-2xl overflow-y-auto max-h-[60vh] min-w-[14rem] py-1"
            style={{ left: menuAcao.x, top: menuAcao.y }}
          >
            <p className="px-3 pt-1.5 pb-1 text-[10px] font-semibold uppercase tracking-wider text-slate-400">Ações de ticket</p>
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

      <main className="flex-1 min-w-0 flex flex-col gap-3">
      <header className="glass rounded-2xl px-5 py-3 flex items-center gap-4 flex-wrap">
        <h1 className="text-xl font-bold tracking-tight text-slate-800 dark:text-white whitespace-nowrap">
          Kanban de Cobrança
        </h1>
        <div className="flex items-center gap-0.5 rounded-xl p-1 bg-slate-500/10 dark:bg-white/10" title="Tamanho dos cards: individual pode ser mudado no menu ⋯ do card">
          <button
            aria-label="Minimizar todos os cards"
            title="Minimizar todos os cards"
            className={`px-3 py-1 rounded-lg text-sm font-medium transition ${modos.global === 'min' ? 'bg-white text-slate-800 shadow dark:bg-white/20 dark:text-white' : 'text-slate-500 hover:text-slate-700 dark:hover:text-white'}`}
            onClick={() => setModoGlobal('min')}
          >
            Compacto
          </button>
          <button
            aria-label="Deixar todos os cards resumidos"
            title="Deixar todos os cards resumidos"
            className={`px-3 py-1 rounded-lg text-sm font-medium transition ${modos.global === 'resumo' ? 'bg-white text-slate-800 shadow dark:bg-white/20 dark:text-white' : 'text-slate-500 hover:text-slate-700 dark:hover:text-white'}`}
            onClick={() => setModoGlobal('resumo')}
          >
            Resumo
          </button>
        </div>

        <div className="flex-1" />

        <div className="flex items-center gap-2 flex-wrap">
          {filtroAtivo && (
            <>
              <span className="text-xs text-slate-500 dark:text-slate-400">
                {visiveis} de {totalCards} cards
              </span>
              <button
                className="text-xs rounded-lg px-2 py-1 bg-slate-500/10 hover:bg-slate-500/20"
                onClick={() => {
                  setFiltroTexto('');
                  setFiltroAssunto('');
                }}
              >
                ✕ Limpar
              </button>
            </>
          )}
          <select
            value={filtroAssunto}
            onChange={(e) => setFiltroAssunto(e.target.value)}
            className="text-sm rounded-xl px-3 py-1.5 max-w-[12rem] bg-white/60 dark:bg-white/10 border border-white/70 dark:border-white/10 outline-none focus:ring-2 focus:ring-indigo-400/60"
            title="Filtrar pelo assunto dos tickets vinculados aos cards"
          >
            <option value="">Assunto: todos</option>
            {assuntosTodos.map((a) => (
              <option key={a} value={a}>{a}</option>
            ))}
          </select>
          <div className="relative">
            <span className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 text-sm pointer-events-none" aria-hidden>
              ⌕
            </span>
            <input
              type="search"
              value={filtroTexto}
              onChange={(e) => setFiltroTexto(e.target.value)}
              placeholder="Buscar: cliente, nº ou assunto…"
              className="text-sm rounded-xl pl-8 pr-3 py-1.5 w-64 bg-white/60 dark:bg-white/10 border border-white/70 dark:border-white/10 placeholder:text-slate-400 dark:placeholder:text-slate-500 outline-none focus:ring-2 focus:ring-indigo-400/60"
            />
          </div>
          <button
            className="text-sm rounded-xl px-4 py-1.5 font-semibold bg-white/80 hover:bg-white text-indigo-700 border border-white shadow-sm dark:bg-indigo-500/80 dark:hover:bg-indigo-500 dark:text-white dark:border-indigo-400/40 transition"
            onClick={() => setChatAberto(true)}
          >
            💬 Assistente
          </button>
          {contaEstado === 'sincronizando' && (
            <span className="text-xs text-slate-500 dark:text-slate-400 self-center">sincronizando</span>
          )}
          {contaEstado === 'aguardando' && (
            <span className="text-xs text-slate-500 dark:text-slate-400 self-center">aguardando internet</span>
          )}
          {contaEstado === 'reconectar' && (
            <span className="text-xs text-amber-800/80 dark:text-amber-200/80 self-center">reconectar a conta</span>
          )}
        </div>
      </header>

      <DndContext
        sensors={sensors}
        collisionDetection={closestCorners}
        onDragStart={onDragStartUnificado}
        onDragCancel={onDragCancelUnificado}
        onDragOver={onDragOverUnificado}
        onDragEnd={onDragEndUnificado}
      >
        <div className="flex gap-4 pb-1 overflow-x-auto overscroll-x-contain flex-1 min-h-0">
          <SortableContext items={colunas} strategy={horizontalListSortingStrategy}>
              {colunas.map((c) => {
                const todosDaColuna = (board[c] ?? []).filter(cardVisivel);
                const ehFechada = c === colunaTicketFechado;
                // Coluna de fechados: só os últimos movidos ficam visíveis —
                // a ordem vem do histórico (mais recente primeiro); card que
                // acabou de chegar (fora do histórico ainda) fica no topo.
                let ticketsDaColuna = todosDaColuna;
                if (ehFechada) {
                  ticketsDaColuna = ordenarFechados(todosDaColuna, historicoFechados)
                    .slice(0, maxCardsFechadosVisiveis);
                } else {
                  ticketsDaColuna = ordenarTickets(todosDaColuna, ordenacaoDe(c));
                }
                return (
                  <ColunaOrdenavel
                    key={c}
                    coluna={c}
                    tickets={ticketsDaColuna}
                    contagem={todosDaColuna.length}
                    ordenacaoModo={ordenacaoDe(c).modo}
                    ordenacaoDesc={ordenacaoDe(c).desc}
                    onOrdenacao={(modo, desc) => setOrdenacaoColuna(c, modo, desc)}
                    acordosPorTicket={acordosPorTicket}
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
                          className="mt-2 w-full text-xs text-white/85 hover:text-white bg-white/10 hover:bg-white/20 py-1.5 rounded-xl font-medium"
                          onClick={() => setHistoricoAberto(true)}
                        >
                          🗂 Histórico de fechados ({historicoFechados.length})
                        </button>
                      ) : c === colunaUpgrade && comissoes.some((x) => x.recebido) ? (
                        <button
                          className="mt-2 w-full text-xs text-white/85 hover:text-white bg-white/10 hover:bg-white/20 py-1.5 rounded-xl font-medium"
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
            className="rounded-3xl w-60 shrink-0 self-start p-4 text-sm font-semibold text-white/80 hover:text-white border-2 border-dashed border-white/30 hover:border-white/60 hover:bg-white/10 transition text-left"
            onClick={criarColuna}
          >
            + Nova coluna
          </button>
        </div>
      </DndContext>
      </main>

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
          onSalvou={() => {
            // Acordo salvo: atualiza o quadro (selos/cobrança), o modal do
            // cliente continua aberto.
            void recarregarBoard();
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
            className="painel-modal rounded-3xl w-[26rem] max-w-full max-h-[70vh] flex flex-col"
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
            className="painel-modal rounded-3xl w-[26rem] max-w-full max-h-[70vh] flex flex-col"
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

      {avisoAtualiza && (
        <div className="fixed bottom-16 left-1/2 -translate-x-1/2 z-[100] glass text-slate-800 dark:text-slate-100 text-sm px-4 py-3 rounded-xl shadow-lg flex items-center gap-3">
          {avisoAtualiza.progresso >= 0 ? (
            <span>Baixando atualização {avisoAtualiza.versao}{avisoAtualiza.progresso > 0 ? ` — ${avisoAtualiza.progresso}%` : '…'}</span>
          ) : (
            <>
              <span>Nova versão {avisoAtualiza.versao} encontrada — baixando em segundo plano…</span>
              <button
                className="text-slate-500 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200"
                title="Dispensar aviso (o download continua)"
                onClick={() => setAvisoAtualiza(null)}
              >
                ✕
              </button>
            </>
          )}
        </div>
      )}
      {prontaAtualiza && (
        <div className="fixed bottom-16 left-1/2 -translate-x-1/2 z-[100] glass text-slate-800 dark:text-slate-100 text-sm px-4 py-3 rounded-xl shadow-lg flex items-center gap-3">
          <span>Versão <b>{prontaAtualiza}</b> baixada. Reiniciar agora para atualizar?</span>
          <button
            className="px-3 py-1 rounded-lg bg-emerald-600 text-white text-xs font-medium hover:bg-emerald-500"
            onClick={() => { bindings.ReiniciarApp().then(() => setProntaAtualiza(null)).catch((e) => {
              setToast('⚠ ' + String(e));
              setTimeout(() => setToast(''), 8000);
            }); }}
          >
            Reiniciar agora
          </button>
          <button
            className="px-3 py-1 rounded-lg text-xs font-medium border border-slate-300/50 text-slate-600 hover:bg-slate-500/10 dark:text-slate-300"
            title="Depois: o botão de reiniciar fica em ⚙ Configurações (e fechar o app já atualiza)"
            onClick={() => setProntaAtualiza(null)}
          >
            Depois
          </button>
        </div>
      )}
      {toast && (
        <div className="fixed bottom-4 left-1/2 -translate-x-1/2 z-[100] glass text-slate-800 dark:text-slate-100 text-sm px-4 py-2.5 rounded-xl shadow-lg">
          {toast}
        </div>
      )}
    </div>
  );
}
