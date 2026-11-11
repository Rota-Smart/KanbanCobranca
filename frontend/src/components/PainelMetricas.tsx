import { useEffect, useState } from 'react';
import { GetMetricas } from '../../wailsjs/go/main/App';
import { formatCents } from '../money';

export interface Metricas {
  porColuna: { coluna: string; total: number }[];
  totalCards: number;
  entradasSemana: number;
  fechadosSemana: number;
  tempoMedioDias: number;
  cobrancasAtivas: number;
  valorEmAbertoCents: number;
  visitasAgendadas: number;
  visitasHoje: number;
}

// Bloco grande de número do painel.
function Numero(props: { rotulo: string; valor: string; destaque?: string }) {
  return (
    <div className="rounded-2xl bg-white/60 dark:bg-white/10 border border-white/70 dark:border-white/10 p-3">
      <div className="text-xs text-slate-500 dark:text-slate-400">{props.rotulo}</div>
      <div className={`text-2xl font-bold mt-0.5 ${props.destaque || 'text-slate-800 dark:text-white'}`}>
        {props.valor}
      </div>
    </div>
  );
}

// PainelMetricas: resumo do quadro em números — cards por coluna, entradas e
// fechados da semana, tempo médio até o fechamento, acordos ativos e visitas.
export default function PainelMetricas(props: { onFechar: () => void }) {
  const [m, setM] = useState<Metricas | null>(null);
  const [erro, setErro] = useState('');

  useEffect(() => {
    GetMetricas()
      .then((d) => setM(d as unknown as Metricas))
      .catch((e) => setErro(String(e)));
  }, []);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 backdrop-blur-sm" onClick={props.onFechar}>
      <div
        className="painel-modal rounded-3xl w-[32rem] max-w-full max-h-[85vh] overflow-y-auto p-5 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <h2 className="font-semibold text-lg dark:text-slate-100">📊 Painel do quadro</h2>
          <button aria-label="Fechar" className="text-slate-400 hover:text-slate-600" onClick={props.onFechar}>
            ✕
          </button>
        </div>

        {erro && <p className="mt-4 text-sm text-red-600 dark:text-red-300">{erro}</p>}
        {!m && !erro && <p className="mt-4 text-sm text-slate-500 dark:text-slate-400">carregando…</p>}

        {m && (
          <>
            <div className="mt-4 grid grid-cols-2 gap-2">
              <Numero rotulo="Cards no quadro" valor={String(m.totalCards)} />
              <Numero rotulo="Entradas nos últimos 7 dias" valor={String(m.entradasSemana)} />
              <Numero rotulo="Fechados nos últimos 7 dias" valor={String(m.fechadosSemana)} destaque="text-emerald-600 dark:text-emerald-400" />
              <Numero
                rotulo="Tempo médio até fechar (30 dias)"
                valor={m.tempoMedioDias > 0 ? `${m.tempoMedioDias.toFixed(1)} dias` : '—'}
              />
              <Numero rotulo="Visitas agendadas" valor={String(m.visitasAgendadas)} />
              <Numero rotulo="Visitas hoje" valor={String(m.visitasHoje)} destaque="text-indigo-600 dark:text-indigo-400" />
              <Numero rotulo="Acordos ativos" valor={String(m.cobrancasAtivas)} />
              <Numero rotulo="Valor em acordos" valor={formatCents(m.valorEmAbertoCents)} destaque="text-amber-600 dark:text-amber-400" />
            </div>

            <section className="mt-4 border rounded-lg p-3">
              <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">Cards por coluna</h3>
              <ul className="mt-2 space-y-1">
                {m.porColuna.map((c) => (
                  <li key={c.coluna} className="flex items-center justify-between text-sm">
                    <span className="text-slate-600 dark:text-slate-300 truncate">{c.coluna}</span>
                    <span className="font-semibold text-slate-800 dark:text-white">{c.total}</span>
                  </li>
                ))}
                {m.porColuna.length === 0 && (
                  <li className="text-sm text-slate-500 dark:text-slate-400">quadro vazio</li>
                )}
              </ul>
            </section>
          </>
        )}
      </div>
    </div>
  );
}
