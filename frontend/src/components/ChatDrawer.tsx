import { useEffect, useRef, useState } from 'react';
import * as App from '../../wailsjs/go/main/App';
import type { db } from '../../wailsjs/go/models';
import type { AcaoDoAgente } from '../wails-types';
import { iniciarVoz } from '../voz';

interface Msg {
  id?: number;
  papel: 'user' | 'assistant';
  conteudo: string;
  acoes?: AcaoDoAgente[];
}

// textoFormatado converte o markdown simples que o modelo usa (**negrito**,
// *itálico*, `código`) em elementos de verdade — o usuário nunca vê os asteriscos.
function textoFormatado(s: string): React.ReactNode[] {
  return s.split(/(\*\*[^*]+\*\*|\*[^*\n]+\*|`[^`\n]+`)/g).map((p, i) => {
    if (p.startsWith('**') && p.endsWith('**') && p.length > 4) return <strong key={i}>{p.slice(2, -2)}</strong>;
    if (p.startsWith('*') && p.endsWith('*') && p.length > 2) return <em key={i}>{p.slice(1, -1)}</em>;
    if (p.startsWith('`') && p.endsWith('`') && p.length > 2)
      return (
        <code key={i} className="px-1 rounded bg-slate-500/15 text-[0.85em]">
          {p.slice(1, -1)}
        </code>
      );
    return p;
  });
}

// ChatDrawer: conversas com o agente Groq, que age no app — busca clientes
// no 360, adiciona cards, cria colunas, move cards e puxa financeiro.
// Cada conversa tem histórico persistido no banco; a lista de conversas fica
// na coluna da esquerda. mensagemInicial (ex.: vinda do Shift+F2) é enviada
// automaticamente na abertura, uma única vez.
export default function ChatDrawer(props: {
  onFechar: () => void;
  onMutou: () => void;
  onToast: (m: string) => void;
  mensagemInicial?: string | null;
  onConsumiuMensagem?: () => void;
}) {
  const [chats, setChats] = useState<db.Chat[]>([]);
  const [chatID, setChatID] = useState<number | null>(null);
  const [msgs, setMsgs] = useState<Msg[]>([]);
  const [texto, setTexto] = useState('');
  const [enviando, setEnviando] = useState(false);
  const [gravando, setGravando] = useState(false);
  const [parcial, setParcial] = useState('');
  const pararVoz = useRef<(() => void) | null>(null);
  const fimRef = useRef<HTMLDivElement>(null);
  const inicialEnviada = useRef(false);
  const chatIDRef = useRef<number | null>(null);
  // Largura do drawer, ajustável arrastando a borda esquerda (persistida).
  const [largura, setLargura] = useState(() => {
    const n = Number(localStorage.getItem('chatLargura'));
    return n >= 320 && n <= window.innerWidth - 80 ? n : 544;
  });
  const larguraRef = useRef(largura);
  larguraRef.current = largura;

  // iniciarRedimensionar arrasta a borda esquerda: a largura cresce na
  // medida em que o ponteiro vai para a esquerda.
  function iniciarRedimensionar(e: React.PointerEvent) {
    e.preventDefault();
    const x0 = e.clientX;
    const w0 = larguraRef.current;
    const mover = (ev: PointerEvent) => {
      const nova = Math.min(window.innerWidth - 80, Math.max(320, w0 + (x0 - ev.clientX)));
      setLargura(nova);
    };
    const soltar = () => {
      window.removeEventListener('pointermove', mover);
      window.removeEventListener('pointerup', soltar);
      localStorage.setItem('chatLargura', String(larguraRef.current));
    };
    window.addEventListener('pointermove', mover);
    window.addEventListener('pointerup', soltar);
  }

  useEffect(() => {
    fimRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [msgs, enviando]);

  // Alternar o microfone: ao clicar de novo (ou no teto de 60s), a gravação
  // vira WAV, vai ao Whisper local e o texto entra no campo de mensagem.
  async function alternarVoz() {
    if (pararVoz.current) {
      const parar = pararVoz.current;
      pararVoz.current = null;
      setGravando(false);
      parar();
      return;
    }
    try {
      pararVoz.current = await iniciarVoz({
        parcial: setParcial,
        final: (t) => {
          setParcial('');
          setTexto((atual) => (atual ? atual + ' ' : '') + t);
        },
        erro: (e) => props.onToast(`⚠ Voz: ${String(e)}`),
        etapa: (s) => props.onToast(`🎤 ${s}`),
      });
      setGravando(true);
    } catch (e) {
      props.onToast(`⚠ Microfone: ${String(e)}`);
    }
  }

  useEffect(() => () => pararVoz.current?.(), []);

  useEffect(() => {
    App.ListarChats().then((cs) => {
      setChats(cs);
      // Se há mensagem inicial (Shift+F2), ela cria um chat novo — abrir o
      // mais recente aqui disputaria com o envio e sobrescreveria a tela
      // com a conversa antiga quando a resposta chegasse por último.
      if (cs.length > 0 && !props.mensagemInicial?.trim()) abrir(cs[0].id);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function abrir(id: number) {
    chatIDRef.current = id;
    setChatID(id);
    setMsgs([]); // limpa já: evita exibir a conversa anterior enquanto carrega
    try {
      const ms = await App.MensagensDoChat(id);
      if (chatIDRef.current !== id) return; // o usuário já trocou de chat
      setMsgs(ms.map((m) => ({ id: m.id, papel: m.papel as Msg['papel'], conteudo: m.conteudo, acoes: m.acoes })));
    } catch {
      if (chatIDRef.current !== id) return;
      setMsgs([]);
    }
  }

  // "+ Nova conversa" não grava nada no banco: o chat só é criado de verdade
  // quando a primeira mensagem é enviada (em enviar). Assim conversas abertas
  // e abandonadas não viram dezenas de "Nova conversa" vazias na lista.
  function novaConversa() {
    chatIDRef.current = null;
    setChatID(null);
    setMsgs([]);
  }

  async function apagar(id: number) {
    try {
      await App.ApagarChat(id);
      const restantes = chats.filter((c) => c.id !== id);
      setChats(restantes);
      if (chatID === id) {
        if (restantes.length > 0) await abrir(restantes[0].id);
        else {
          setChatID(null);
          setMsgs([]);
        }
      }
    } catch (e) {
      props.onToast(`⚠ ${String(e)}`);
    }
  }

  async function enviar(textoForcado?: string) {
    const m = (textoForcado ?? texto).trim();
    if (!m || enviando) return;
    let id = chatID;
    if (id === null) {
      try {
        const c = await App.CriarChat();
        id = c.id;
        chatIDRef.current = id;
        setChatID(id);
      } catch (e) {
        props.onToast(`⚠ ${String(e)}`);
        return;
      }
    }
    setTexto('');
    setMsgs((ms) => [...ms, { papel: 'user', conteudo: m }, { papel: 'assistant', conteudo: '…' }]);
    setEnviando(true);
    try {
      const res = await App.EnviarChat(id, m);
      setMsgs((ms) => [...ms.slice(0, -1), { papel: 'assistant', conteudo: res.resposta, acoes: res.acoes }]);
      if (res.acoes.length > 0) props.onMutou();
      App.ListarChats().then(setChats); // reordena pela atividade + título auto
    } catch (e) {
      setMsgs((ms) => [...ms.slice(0, -1), { papel: 'assistant', conteudo: `⚠ ${String(e)}` }]);
    } finally {
      setEnviando(false);
    }
  }

  useEffect(() => {
    if (inicialEnviada.current) return;
    const m = props.mensagemInicial?.trim();
    if (!m) return;
    inicialEnviada.current = true;
    props.onConsumiuMensagem?.();
    void enviar(m);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-black/30 backdrop-blur-sm" onClick={props.onFechar}>
      <aside
        className="glass h-full shadow-xl flex relative"
        style={{ width: largura, maxWidth: '100%' }}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Borda de redimensionar: arraste para alargar/encolher o assistente. */}
        <div
          role="separator"
          aria-orientation="vertical"
          aria-label="Arraste para redimensionar o assistente"
          title="Arraste para redimensionar"
          className="absolute left-0 top-0 h-full w-1.5 cursor-ew-resize hover:bg-sky-400/40 z-10"
          onPointerDown={iniciarRedimensionar}
        />
        {/* Coluna de conversas */}
        <div className="w-44 shrink-0 border-r border-white/20 dark:border-white/10 flex flex-col p-2 gap-1">
          <button
            className="w-full bg-blue-600 text-white rounded px-2 py-1.5 text-sm disabled:opacity-50"
            onClick={novaConversa}
          >
            + Nova conversa
          </button>
          <div className="flex-1 overflow-y-auto mt-1 flex flex-col gap-1">
            {chats.map((c) => (
              <div
                key={c.id}
                className={`group flex items-center rounded px-2 py-1.5 text-xs cursor-pointer ${
                  chatID === c.id
                    ? 'bg-blue-600/20 text-blue-700 dark:text-blue-200'
                    : 'hover:bg-white/40 dark:hover:bg-white/10 text-slate-600 dark:text-slate-300'
                }`}
                onClick={() => abrir(c.id)}
              >
                <span className="flex-1 truncate" title={c.titulo}>{c.titulo}</span>
                <button
                  aria-label="Apagar conversa"
                  className="hidden group-hover:block text-slate-400 hover:text-red-500 ml-1"
                  onClick={(e) => {
                    e.stopPropagation();
                    void apagar(c.id);
                  }}
                >
                  ✕
                </button>
              </div>
            ))}
          </div>
        </div>

        {/* Conversa atual */}
        <div className="flex-1 min-w-0 p-4 flex flex-col overflow-hidden">
          <div className="flex items-start justify-between">
            <div>
              <h2 className="text-lg font-semibold dark:text-slate-100">Assistente (Groq)</h2>
              <p className="text-xs text-slate-500">
                Peça: “adicione o cliente 24755 em Cobrança”, “crie a coluna Renegociação”, “qual o
                financeiro do 20690?”…
              </p>
            </div>
            <button aria-label="Fechar chat" className="text-slate-400 hover:text-slate-600" onClick={props.onFechar}>
              ✕
            </button>
          </div>

          <div className="mt-3 flex-1 flex flex-col gap-2 overflow-y-auto">
            {msgs.length === 0 && (
              <p className="text-sm text-slate-400 mt-4">
                Exemplos:
                <br />• “Buscar o cliente RESOLUTE e adicionar em Negociando”
                <br />• “Crie uma coluna chamada Renegociação”
                <br />• “Financeiro do 24755” e depois “adicione em Cobrança e gere a proposta”
              </p>
            )}
            {msgs.map((m, i) => (
              <div key={m.id ?? i} className={m.papel === 'user' ? 'self-end' : 'self-start w-full'}>
                <div
                  className={`rounded-lg px-3 py-2 text-sm max-w-[22rem] whitespace-pre-wrap ${
                    m.papel === 'user' ? 'bg-blue-600 text-white' : 'bg-slate-100 dark:bg-slate-700 text-slate-800 dark:text-slate-100'
                  }`}
                >
                  {m.papel === 'assistant' ? textoFormatado(m.conteudo) : m.conteudo}
                </div>
                {m.acoes && m.acoes.length > 0 && (
                  <ul className="mt-1 text-xs text-emerald-700 flex flex-col gap-0.5">
                    {m.acoes.map((a, j) => (
                      <li key={j}>⚙ {a.resumo}</li>
                    ))}
                  </ul>
                )}
              </div>
            ))}
            {enviando && <p className="text-xs text-slate-400 self-start">pensando…</p>}
            <div ref={fimRef} />
          </div>

          <form
            className="mt-2 flex gap-1 items-center"
            onSubmit={(e) => {
              e.preventDefault();
              enviar();
            }}
          >
            <button
              type="button"
              aria-label={gravando ? 'Parar gravação' : 'Falar (fala para texto)'}
              title={gravando ? 'Parar gravação' : 'Fala para texto (Vosk local)'}
              className={`rounded px-2 py-1.5 text-sm border ${
                gravando
                  ? 'bg-red-600 text-white border-red-600 animate-pulse'
                  : 'border-slate-300 text-slate-600 hover:bg-white/40 dark:border-slate-500 dark:text-slate-300'
              }`}
              onClick={alternarVoz}
            >
              🎤
            </button>
            <input
              className="flex-1 border rounded px-2 py-1.5 text-sm"
              placeholder={gravando ? 'Ouvindo… fale sua mensagem' : 'Peça uma ação ao assistente…'}
              value={texto}
              onChange={(e) => setTexto(e.target.value)}
            />
            <button
              type="submit"
              className="bg-blue-600 text-white rounded px-3 py-1.5 text-sm disabled:opacity-50"
              disabled={enviando || !texto.trim()}
            >
              Enviar
            </button>
          </form>
          {parcial && (
            <p className="mt-1 text-xs text-slate-400 italic truncate" title={parcial}>
              {parcial}…
            </p>
          )}
        </div>
      </aside>
    </div>
  );
}
