import { useState } from 'react';
import * as App from '../../wailsjs/go/main/App';
import type { Ticket } from '../wails-types';

// MovimentarTicketDialog é a ação manual (Shift+F6): pega um ticket do 360
// (número, uuid ou URL) e coloca o card do cliente numa coluna — tudo direto
// nas funções do app, sem passar pelo assistente de IA.
export default function MovimentarTicketDialog(props: {
  colunas: string[];
  onPronto: (t: Ticket) => void;
  onFechar: () => void;
}) {
  const [entrada, setEntrada] = useState('');
  const [coluna, setColuna] = useState(props.colunas[0] ?? '');
  const [fazendo, setFazendo] = useState(false);
  const [erro, setErro] = useState('');

  async function movimentar() {
    if (!entrada.trim() || !coluna) return;
    setFazendo(true);
    setErro('');
    try {
      const t = await App.MovimentarTicket360(entrada.trim(), coluna);
      props.onPronto(t);
    } catch (e) {
      setErro(String(e));
    } finally {
      setFazendo(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 backdrop-blur-sm" onClick={props.onFechar}>
      <div
        className="glass rounded-2xl w-[28rem] max-w-full p-4 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <h2 className="font-semibold dark:text-slate-100">Mover ticket para coluna</h2>
          <button aria-label="Fechar" className="text-slate-400 hover:text-slate-600" onClick={props.onFechar}>
            ✕
          </button>
        </div>
        <p className="text-xs text-slate-500 mt-1">
          Ação direta no app (sem o assistente): busca o ticket e o cliente no 360 e coloca o card na coluna.
          Cliente sem card → cria novo; com card → move o existente e vincula o ticket.
        </p>

        <form
          className="mt-3 flex flex-col gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            movimentar();
          }}
        >
          <input
            className="border rounded px-2 py-1.5 text-sm"
            placeholder="Nº do ticket (ex.: 12345), uuid ou URL do ticket"
            value={entrada}
            onChange={(e) => setEntrada(e.target.value)}
            autoFocus
          />
          <select
            className="border rounded px-2 py-1.5 text-sm bg-white/70 dark:bg-slate-800"
            value={coluna}
            onChange={(e) => setColuna(e.target.value)}
          >
            {props.colunas.map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </select>
          <button
            type="submit"
            className="bg-blue-600 text-white rounded px-3 py-1.5 text-sm disabled:opacity-50"
            disabled={fazendo || !entrada.trim() || !coluna}
          >
            {fazendo ? 'Movendo…' : 'Movimentar'}
          </button>
        </form>

        {erro && <p className="mt-2 text-xs text-red-600">{erro}</p>}
      </div>
    </div>
  );
}
