import { useState } from 'react';
import PainelMetricas from './PainelMetricas';
import CancelamentosDashboard from './CancelamentosDashboard';
import type { Ticket } from '../wails-types';

// DashboardPainel: o menu 📊 abre ESTE modal — um submenu com o título do
// dashboard e as duas opções: o painel do quadro (números do kanban) e o
// dashboard de cancelamentos (motivos, reversão/perda/em tratamento e o
// importador de lista colada). A aba escolhida fica guardada: abrir de novo
// volta onde você estava.
export type AbaPainel = 'quadro' | 'cancelamentos';

const CHAVE_ABA = 'painel-aba';

export default function DashboardPainel(props: {
  onFechar: () => void;
  onClicarColuna?: (coluna: string) => void;
  onAbrirCard?: (t: Ticket) => void;
}) {
  const [aba, setAba] = useState<AbaPainel>(() => {
    const salva = sessionStorage.getItem(CHAVE_ABA);
    return salva === 'cancelamentos' ? 'cancelamentos' : 'quadro';
  });

  function escolher(a: AbaPainel) {
    setAba(a);
    sessionStorage.setItem(CHAVE_ABA, a);
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 backdrop-blur-sm" onClick={props.onFechar}>
      <div
        className="painel-modal rounded-3xl w-[58rem] max-w-[96vw] max-h-[92vh] overflow-y-auto p-6 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between gap-3">
          <h2 className="font-semibold text-lg dark:text-slate-100 shrink-0">📊 Dashboard</h2>
          <div className="flex rounded-xl bg-slate-500/10 p-0.5 text-xs" role="tablist" aria-label="Seções do dashboard">
            <button
              role="tab"
              aria-selected={aba === 'quadro'}
              className={`px-3 py-1.5 rounded-lg transition ${
                aba === 'quadro'
                  ? 'bg-white/80 dark:bg-white/15 text-indigo-700 dark:text-indigo-200 font-semibold shadow-sm'
                  : 'text-slate-600 dark:text-slate-300 hover:bg-slate-500/10'
              }`}
              onClick={() => escolher('quadro')}
            >
              Painel do quadro
            </button>
            <button
              role="tab"
              aria-selected={aba === 'cancelamentos'}
              className={`px-3 py-1.5 rounded-lg transition ${
                aba === 'cancelamentos'
                  ? 'bg-white/80 dark:bg-white/15 text-indigo-700 dark:text-indigo-200 font-semibold shadow-sm'
                  : 'text-slate-600 dark:text-slate-300 hover:bg-slate-500/10'
              }`}
              onClick={() => escolher('cancelamentos')}
            >
              🚪 Cancelamentos
            </button>
          </div>
          <button aria-label="Fechar" className="text-slate-400 hover:text-slate-600 shrink-0" onClick={props.onFechar}>
            ✕
          </button>
        </div>

        {aba === 'quadro' ? (
          <PainelMetricas embutido onClicarColuna={props.onClicarColuna} />
        ) : (
          <CancelamentosDashboard embutido onAbrirCard={props.onAbrirCard} />
        )}
      </div>
    </div>
  );
}
