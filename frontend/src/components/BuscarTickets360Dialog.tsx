import { useState } from 'react';
import * as App from '../../wailsjs/go/main/App';
import type { Ticket360 } from '../wails-types';

// BuscarTickets360Dialog pesquisa tickets ABERTOS do 360 por número,
// código service do cliente ou nome do solicitante; 🌐 abre o ticket no
// navegador (mesma navegação do abrir_ticket_360).
export default function BuscarTickets360Dialog(props: { onFechar: () => void }) {
  const [busca, setBusca] = useState('');
  const [resultados, setResultados] = useState<Ticket360[]>([]);
  const [buscou, setBuscou] = useState(false);
  const [buscando, setBuscando] = useState(false);
  const [abrindo, setAbrindo] = useState<string | null>(null);
  const [erro, setErro] = useState('');

  async function buscar() {
    const q = busca.trim();
    if (!q) return;
    setBuscando(true);
    setErro('');
    try {
      setResultados(await App.Rota360PesquisarTickets(q));
      setBuscou(true);
    } catch (e) {
      setErro(String(e));
    } finally {
      setBuscando(false);
    }
  }

  async function abrir(numero: string) {
    setAbrindo(numero);
    setErro('');
    try {
      await App.Rota360AbrirTicket(numero);
    } catch (e) {
      setErro(String(e));
    } finally {
      setAbrindo(null);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 backdrop-blur-sm" onClick={props.onFechar}>
      <div
        className="glass rounded-2xl w-[32rem] max-w-full max-h-[80vh] overflow-y-auto p-4 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <h2 className="font-semibold dark:text-slate-100">Buscar tickets no 360</h2>
          <button aria-label="Fechar" className="text-slate-400 hover:text-slate-600" onClick={props.onFechar}>
            ✕
          </button>
        </div>
        <p className="text-xs text-slate-500 mt-1">
          Tickets em aberto/pendentes — por número do ticket, código service ou nome do cliente.
        </p>

        <form
          className="mt-3 flex gap-1"
          onSubmit={(e) => {
            e.preventDefault();
            buscar();
          }}
        >
          <input
            className="flex-1 border rounded px-2 py-1 text-sm"
            placeholder="Nº do ticket, código service ou nome"
            value={busca}
            onChange={(e) => setBusca(e.target.value)}
            autoFocus
          />
          <button
            type="submit"
            className="bg-blue-600 text-white rounded px-3 py-1 text-sm disabled:opacity-50"
            disabled={buscando || !busca.trim()}
          >
            {buscando ? '…' : 'Buscar'}
          </button>
        </form>

        {erro && <p className="mt-2 text-xs text-red-600">{erro}</p>}

        {buscou && resultados.length === 0 && (
          <p className="mt-3 text-sm text-slate-500">Nenhum ticket aberto encontrado.</p>
        )}

        <ul className="mt-3 flex flex-col gap-1">
          {resultados.map((t) => (
            <li key={t.id} className="border rounded-lg p-2 flex items-center justify-between gap-2">
              <div className="min-w-0">
                <p className="text-sm font-medium truncate">
                  #{t.numero} · {t.titulo || t.assunto_nome}
                </p>
                <p className="text-xs text-slate-500 truncate">
                  {t.solicitante_nome}
                  {t.codigo_service ? ` (cód. ${t.codigo_service})` : ''}
                  {t.status ? ` · ${t.status}` : ''}
                </p>
              </div>
              <button
                className="shrink-0 bg-sky-600 text-white rounded px-2 py-1 text-xs disabled:opacity-50"
                title="Abrir o ticket no navegador"
                disabled={abrindo !== null}
                onClick={() => abrir(t.numero)}
              >
                {abrindo === t.numero ? '…' : '🌐 Abrir'}
              </button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
