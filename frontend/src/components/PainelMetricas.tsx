import { useEffect, useState } from 'react';
import { GetMetricas, TempoPorColuna } from '../../wailsjs/go/main/App';
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

export interface TempoColuna {
  coluna: string;
  mediaSegundos: number;
  cards: number;
}

// "3d 4h", "5h 12min" ou "32min" — tempo médio de permanência na coluna.
export function formatarTempo(segundos: number): string {
  if (!segundos || segundos < 60) return '—';
  const d = Math.floor(segundos / 86400);
  const h = Math.floor((segundos % 86400) / 3600);
  const min = Math.floor((segundos % 3600) / 60);
  if (d > 0) return h > 0 ? `${d}d ${h}h` : `${d}d`;
  if (h > 0) return min > 0 ? `${h}h ${min}min` : `${h}h`;
  return `${min}min`;
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
// Clicar num item de "Cards por coluna" fecha o painel e foca a coluna no quadro.
// Com embutido=true, renderiza só o conteúdo (o modal e o título ficam por
// conta do DashboardPainel, que alterna entre este painel e o de cancelamentos).
export default function PainelMetricas(props: {
  onFechar?: () => void;
  onClicarColuna?: (coluna: string) => void;
  onAbrirCancelamentos?: () => void;
  embutido?: boolean;
}) {
  const [m, setM] = useState<Metricas | null>(null);
  const [tempos, setTempos] = useState<TempoColuna[]>([]);
  const [erro, setErro] = useState('');

  useEffect(() => {
    GetMetricas()
      .then((d) => setM(d as unknown as Metricas))
      .catch((e) => setErro(String(e)));
    TempoPorColuna()
      .then((d) => setTempos((d as unknown as TempoColuna[]) || []))
      .catch(() => setTempos([]));
  }, []);

  const conteudo = (
    <>
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
                  <li key={c.coluna}>
                    <button
                      className="w-full flex items-center justify-between text-sm rounded-lg px-1.5 -mx-1.5 py-0.5 hover:bg-slate-500/10 dark:hover:bg-white/10 cursor-pointer transition"
                      title={`Mostrar só a coluna ${c.coluna}`}
                      onClick={() => props.onClicarColuna?.(c.coluna)}
                    >
                      <span className="text-slate-600 dark:text-slate-300 truncate">{c.coluna}</span>
                      <span className="font-semibold text-slate-800 dark:text-white">{c.total}</span>
                    </button>
                  </li>
                ))}
                {m.porColuna.length === 0 && (
                  <li className="text-sm text-slate-500 dark:text-slate-400">quadro vazio</li>
                )}
              </ul>
            </section>

            <section className="mt-3 border rounded-lg p-3">
              <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">⏱️ Tempo médio por coluna</h3>
              <p className="text-xs text-slate-400 dark:text-slate-500 mt-0.5">onde os cards passam mais tempo (gargalos)</p>
              <ul className="mt-2 space-y-1">
                {tempos.map((t) => (
                  <li key={t.coluna} className="flex items-center justify-between text-sm">
                    <span className="text-slate-600 dark:text-slate-300 truncate">{t.coluna}</span>
                    <span className="text-slate-800 dark:text-white">
                      <span className="font-semibold">{formatarTempo(t.mediaSegundos)}</span>
                      <span className="text-xs text-slate-400 dark:text-slate-500 ml-2">{t.cards} cards</span>
                    </span>
                  </li>
                ))}
                {tempos.length === 0 && (
                  <li className="text-sm text-slate-500 dark:text-slate-400">sem dados ainda</li>
                )}
              </ul>
            </section>
          </>
        )}
    </>
  );

  if (props.embutido) return conteudo;

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
        {conteudo}
      </div>
    </div>
  );
}
