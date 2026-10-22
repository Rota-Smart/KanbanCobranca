import { useState } from 'react';
import * as App from '../../wailsjs/go/main/App';
import type { ResumoCliente360, Ticket } from '../wails-types';

// BuscarCliente360Dialog pesquisa cliente por código service ou nome no 360
// e importa o card já preenchido na coluna escolhida.
export default function BuscarCliente360Dialog(props: {
  coluna: string;
  onImportado: (t: Ticket) => void;
  onFechar: () => void;
}) {
  const [busca, setBusca] = useState('');
  const [resultados, setResultados] = useState<ResumoCliente360[]>([]);
  const [buscando, setBuscando] = useState(false);
  const [importando, setImportando] = useState<number | null>(null);
  const [erro, setErro] = useState('');

  async function buscar() {
    const q = busca.trim();
    if (!q) return;
    setBuscando(true);
    setErro('');
    try {
      setResultados(await App.Rota360BuscarClientes(q));
    } catch (e) {
      setErro(String(e));
    } finally {
      setBuscando(false);
    }
  }

  async function importar(cod: number) {
    setImportando(cod);
    setErro('');
    try {
      const t = await App.ImportarCliente360(String(cod), props.coluna);
      props.onImportado(t);
    } catch (e) {
      setErro(String(e));
    } finally {
      setImportando(null);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 backdrop-blur-sm" onClick={props.onFechar}>
      <div
        className="glass rounded-2xl w-[28rem] max-w-full max-h-[80vh] overflow-y-auto p-4 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <h2 className="font-semibold dark:text-slate-100">Buscar cliente no 360</h2>
          <button aria-label="Fechar" className="text-slate-400 hover:text-slate-600" onClick={props.onFechar}>
            ✕
          </button>
        </div>
        <p className="text-xs text-slate-500 mt-1">
          Coluna de destino: <strong>{props.coluna}</strong>
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
            placeholder="Código service ou nome do cliente"
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

        <ul className="mt-3 flex flex-col gap-1">
          {resultados.map((r) => (
            <li key={r.CodCliente} className="border rounded-lg p-2 flex items-center justify-between gap-2">
              <div className="min-w-0">
                <p className="text-sm font-medium truncate">{r.Nome}</p>
                <p className="text-xs text-slate-500">
                  #{r.CodCliente} · {r.CGCCPF || 'sem CPF/CNPJ'} · {r.Cidade || ''}
                  {r.Situacao && r.Situacao !== 'Ativo' ? ` · ${r.Situacao}` : ''}
                </p>
              </div>
              <button
                className="shrink-0 bg-emerald-600 text-white rounded px-2 py-1 text-xs disabled:opacity-50"
                disabled={importando !== null}
                onClick={() => importar(r.CodCliente)}
              >
                {importando === r.CodCliente ? '…' : 'Adicionar'}
              </button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
