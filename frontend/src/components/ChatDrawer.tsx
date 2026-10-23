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

  useEffect(() => {
    fimRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [msgs, enviando]);

  // Alternar o microfone: enquanto grava, o que é falado aparece como
  // parcial (cinza) e cada frase finalizada entra no campo de mensagem.
  async function alternarVoz() {
    if (pararVoz.current) {
      pararVoz.current();
      pararVoz.current = null;
      setGravando(false);
      setParcial('');
      return;
    }
    try {
      const st = await App.ModeloVozStatus();
      if (!st.configurado) {
        props.onToast(`⚠ ${st.mensagem}`);
        return;
      }
      pararVoz.current = await iniciarVoz({
        parcial: setParcial,
        final: (t) => {
          setParcial('');
          setTexto((atual) => (atual ? atual + ' ' : '') + t);
        },
        erro: (e) => props.onToast(`⚠ Voz: ${String(e)}`),
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
      if (cs.length > 0) abrir(cs[0].id);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function abrir(id: number) {
    setChatID(id);
    try {
      const ms = await App.MensagensDoChat(id);
      setMsgs(ms.map((m) => ({ id: m.id, papel: m.papel as Msg['papel'], conteudo: m.conteudo, acoes: m.acoes })));
    } catch {
      setMsgs([]);
    }
  }

  async function novaConversa(): Promise<number | null> {
    try {
      const c = await App.CriarChat();
      setChats((cs) => [c, ...cs]);
      setChatID(c.id);
      setMsgs([]);
      return c.id;
    } catch (e) {
      props.onToast(`⚠ ${String(e)}`);
      return null;
    }
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
      const novo = await novaConversa();
      if (novo === null) return;
      id = novo;
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
        className="glass w-[34rem] max-w-full h-full shadow-xl flex"
        onClick={(e) => e.stopPropagation()}
      >
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
                  {m.conteudo}
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
              disabled={enviando}
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
