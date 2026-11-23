import { useEffect, useState } from 'react';
import { ListarMotivosCancelamento, RegistrarCancelamento } from '../../wailsjs/go/main/App';

// MotivoCancelamentoDialog: aparece quando um card com ticket de assunto
// "Cancelamento" tenta entrar em "Ticket Fechado" sem motivo. Exige escolher
// o resultado (reversão = cliente ficou / perda = cliente saiu) e digitar o
// motivo — que pode ser um já usado (datalist) ou um novo, criado na hora.
export default function MotivoCancelamentoDialog(props: {
  ticketId: number;
  nomeCliente: string;
  onConfirmado: () => void;
  onFechar: () => void;
}) {
  const [motivos, setMotivos] = useState<string[]>([]);
  const [motivo, setMotivo] = useState('');
  const [resultado, setResultado] = useState<'reversao' | 'perda' | ''>('');
  const [salvando, setSalvando] = useState(false);
  const [erro, setErro] = useState('');

  useEffect(() => {
    ListarMotivosCancelamento()
      .then((ms) => setMotivos((ms as unknown as { nome: string }[]).map((m) => m.nome)))
      .catch(() => setMotivos([]));
  }, []);

  async function confirmar() {
    if (!motivo.trim() || !resultado) return;
    setSalvando(true);
    setErro('');
    try {
      await RegistrarCancelamento(props.ticketId, motivo.trim(), resultado);
      props.onConfirmado();
    } catch (e) {
      setErro(String(e));
      setSalvando(false);
    }
  }

  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/30 backdrop-blur-sm" onClick={props.onFechar}>
      <div
        className="painel-modal rounded-3xl w-[26rem] max-w-full p-4 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <h2 className="font-semibold dark:text-slate-100">Motivo do cancelamento</h2>
          <button aria-label="Fechar" className="text-slate-400 hover:text-slate-600" onClick={props.onFechar}>
            ✕
          </button>
        </div>
        <p className="text-xs text-slate-500 mt-1">
          {props.nomeCliente ? `${props.nomeCliente}: ` : ''}card de cancelamento — diga o que aconteceu antes de fechar.
        </p>

        <div className="mt-3 flex gap-2">
          <button
            className={`flex-1 rounded-lg px-3 py-2 text-sm border transition ${
              resultado === 'reversao'
                ? 'bg-emerald-600 text-white border-emerald-600'
                : 'hover:bg-emerald-500/10 text-slate-600 dark:text-slate-300'
            }`}
            onClick={() => setResultado('reversao')}
          >
            ↩️ Reversão (ficou)
          </button>
          <button
            className={`flex-1 rounded-lg px-3 py-2 text-sm border transition ${
              resultado === 'perda'
                ? 'bg-red-600 text-white border-red-600'
                : 'hover:bg-red-500/10 text-slate-600 dark:text-slate-300'
            }`}
            onClick={() => setResultado('perda')}
          >
            📉 Perda (saiu)
          </button>
        </div>

        <form
          className="mt-3 flex flex-col gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            void confirmar();
          }}
        >
          <input
            className="border rounded px-2 py-1.5 text-sm"
            placeholder="Motivo (ex.: preço, internet lenta, mudou de cidade…)"
            value={motivo}
            onChange={(e) => setMotivo(e.target.value)}
            list="motivos-cancelamento"
            autoFocus
          />
          <datalist id="motivos-cancelamento">
            {motivos.map((m) => (
              <option key={m} value={m} />
            ))}
          </datalist>
          <p className="text-[11px] text-slate-400">Digite um motivo novo para criá-lo ou escolha um já usado na lista.</p>
          <button
            type="submit"
            className="bg-blue-600 text-white rounded px-3 py-1.5 text-sm disabled:opacity-50"
            disabled={salvando || !motivo.trim() || !resultado}
          >
            {salvando ? 'Salvando…' : 'Confirmar e fechar card'}
          </button>
        </form>

        {erro && <p className="mt-2 text-xs text-red-600">{erro}</p>}
      </div>
    </div>
  );
}
