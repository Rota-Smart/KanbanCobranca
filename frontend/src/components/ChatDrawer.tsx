import { useEffect, useRef, useState } from 'react';
import * as App from '../../wailsjs/go/main/App';
import type { AcaoDoAgente, MensagemDeChat } from '../wails-types';

interface Msg {
  papel: 'user' | 'assistant';
  conteudo: string;
  acoes?: AcaoDoAgente[];
}

// ChatDrawer: conversa com o agente Groq, que age no app — busca clientes
// no 360, adiciona cards, cria colunas, move cards e puxa financeiro.
// mensagemInicial (ex.: vinda do Shift+F2) é enviada automaticamente na
// abertura, uma única vez.
export default function ChatDrawer(props: {
  onFechar: () => void;
  onMutou: () => void;
  onToast: (m: string) => void;
  mensagemInicial?: string | null;
  onConsumiuMensagem?: () => void;
}) {
  const [msgs, setMsgs] = useState<Msg[]>([]);
  const [texto, setTexto] = useState('');
  const [enviando, setEnviando] = useState(false);
  const fimRef = useRef<HTMLDivElement>(null);
  const inicialEnviada = useRef(false);

  useEffect(() => {
    fimRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [msgs, enviando]);

  async function enviar(textoForcado?: string) {
    const m = (textoForcado ?? texto).trim();
    if (!m || enviando) return;
    setTexto('');
    const historico: MensagemDeChat[] = msgs.map(({ papel, conteudo }) => ({ papel, conteudo }));
    setMsgs((ms) => [...ms, { papel: 'user', conteudo: m }, { papel: 'assistant', conteudo: '…' }]);
    setEnviando(true);
    try {
      const res = await App.EnviarChat(historico, m);
      setMsgs((ms) => [...ms.slice(0, -1), { papel: 'assistant', conteudo: res.resposta, acoes: res.acoes }]);
      if (res.acoes.length > 0) props.onMutou();
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
    <div className="fixed inset-0 z-40 flex justify-end bg-black/30" onClick={props.onFechar}>
      <aside
        className="bg-white dark:bg-slate-800 w-[26rem] max-w-full h-full overflow-y-auto p-4 shadow-xl flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
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
            <div key={i} className={m.papel === 'user' ? 'self-end' : 'self-start w-full'}>
              <div
                className={`rounded-lg px-3 py-2 text-sm max-w-[20rem] whitespace-pre-wrap ${
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
          className="mt-2 flex gap-1"
          onSubmit={(e) => {
            e.preventDefault();
            enviar();
          }}
        >
          <input
            className="flex-1 border rounded px-2 py-1.5 text-sm"
            placeholder="Peça uma ação ao assistente…"
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
      </aside>
    </div>
  );
}
