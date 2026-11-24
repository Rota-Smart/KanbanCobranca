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

export function formatarTempo(segundos: number): string {
  if (!segundos || segundos < 60) return '—';
  const d = Math.floor(segundos / 86400);
  const h = Math.floor((segundos % 86400) / 3600);
  const min = Math.floor((segundos % 3600) / 60);
  if (d > 0) return h > 0 ? `${d}d ${h}h` : `${d}d`;
  if (h > 0) return min > 0 ? `${h}h ${min}min` : `${h}h`;
  return `${min}min`;
}

interface KpiCardProps {
  titulo: string;
  valor: string;
  subtitulo?: string;
  icone: string;
  destaque?: string;
  gradiente?: string;
}

function KpiCard({ titulo, valor, subtitulo, icone, destaque, gradiente }: KpiCardProps) {
  return (
    <div
      className={`relative overflow-hidden rounded-2xl p-4 transition-all duration-200 hover:-translate-y-0.5 hover:shadow-lg border border-white/60 dark:border-white/10 ${
        gradiente || 'bg-white/70 dark:bg-slate-800/60'
      }`}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="text-[11px] font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400 truncate">
            {titulo}
          </p>
          <p className={`text-2xl font-black tracking-tight mt-1 truncate ${destaque || 'text-slate-800 dark:text-white'}`}>
            {valor}
          </p>
          {subtitulo && (
            <p className="text-[10px] text-slate-400 dark:text-slate-500 mt-0.5 truncate">
              {subtitulo}
            </p>
          )}
        </div>
        <div className="w-10 h-10 rounded-xl bg-white/80 dark:bg-white/10 shadow-sm flex items-center justify-center text-lg shrink-0">
          {icone}
        </div>
      </div>
    </div>
  );
}

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

  const totalCards = m?.totalCards || 1;
  const maxTempo = Math.max(...tempos.map((t) => t.mediaSegundos), 1);

  const conteudo = (
    <div className="space-y-5">
      {erro && (
        <div className="p-3 rounded-xl bg-red-500/10 border border-red-500/20 text-red-600 dark:text-red-400 text-sm">
          {erro}
        </div>
      )}
      {!m && !erro && (
        <div className="flex items-center justify-center py-12 text-slate-400">
          <span className="animate-pulse">Carregando métricas executivas…</span>
        </div>
      )}

      {m && (
        <>
          {/* Grade de KPIs Estilo Power BI */}
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
            <KpiCard
              titulo="Total no Quadro"
              valor={String(m.totalCards)}
              subtitulo="Cards em fluxo"
              icone="📋"
            />
            <KpiCard
              titulo="Entradas 7d"
              valor={String(m.entradasSemana)}
              subtitulo="Novas demandas"
              icone="📥"
              destaque="text-sky-600 dark:text-sky-400"
            />
            <KpiCard
              titulo="Fechados 7d"
              valor={String(m.fechadosSemana)}
              subtitulo="Resolvidos na semana"
              icone="✅"
              destaque="text-emerald-600 dark:text-emerald-400"
            />
            <KpiCard
              titulo="Tempo Médio"
              valor={m.tempoMedioDias > 0 ? `${m.tempoMedioDias.toFixed(1)}d` : '—'}
              subtitulo="Ciclo de resolução (30d)"
              icone="⏱️"
            />
            <KpiCard
              titulo="Visitas Agendadas"
              valor={String(m.visitasAgendadas)}
              subtitulo="Compromissos futuros"
              icone="📅"
            />
            <KpiCard
              titulo="Visitas Hoje"
              valor={String(m.visitasHoje)}
              subtitulo="Atendimentos do dia"
              icone="🚀"
              destaque="text-indigo-600 dark:text-indigo-400"
            />
            <KpiCard
              titulo="Acordos Ativos"
              valor={String(m.cobrancasAtivas)}
              subtitulo="Negociações em vigor"
              icone="🤝"
            />
            <KpiCard
              titulo="Valor Negociado"
              valor={formatCents(m.valorEmAbertoCents)}
              subtitulo="Volume total sob acordo"
              icone="💰"
              destaque="text-amber-600 dark:text-amber-400"
            />
          </div>

          {/* Seção Gráfica e Distribuição */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {/* Cards por Coluna (Barras de Distribuição) */}
            <div className="rounded-2xl p-4 bg-white/70 dark:bg-slate-800/60 border border-white/60 dark:border-white/10 shadow-sm flex flex-col">
              <div className="flex items-center justify-between pb-2 border-b border-slate-200/50 dark:border-slate-700/50">
                <h3 className="text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-200 flex items-center gap-2">
                  <span>📊</span> Distribuição por Coluna
                </h3>
                <span className="text-[10px] text-slate-400">Clique para filtrar</span>
              </div>
              <div className="mt-3 space-y-2 flex-1 overflow-y-auto max-h-60 pr-1">
                {m.porColuna.map((c) => {
                  const pct = Math.round((c.total / totalCards) * 100);
                  return (
                    <button
                      key={c.coluna}
                      className="w-full text-left group p-1.5 rounded-xl hover:bg-slate-500/10 transition"
                      title={`Filtrar somente a coluna "${c.coluna}"`}
                      onClick={() => props.onClicarColuna?.(c.coluna)}
                    >
                      <div className="flex justify-between items-center text-xs mb-1">
                        <span className="font-semibold text-slate-700 dark:text-slate-200 group-hover:text-indigo-600 dark:group-hover:text-indigo-400 transition truncate">
                          {c.coluna}
                        </span>
                        <div className="flex items-center gap-2">
                          <span className="text-[10px] text-slate-400">{pct}%</span>
                          <span className="font-bold text-slate-900 dark:text-white px-2 py-0.5 rounded-full bg-slate-100 dark:bg-slate-700 text-[11px]">
                            {c.total}
                          </span>
                        </div>
                      </div>
                      <div className="w-full h-2 rounded-full bg-slate-200 dark:bg-slate-700/60 overflow-hidden">
                        <div
                          className="h-full rounded-full bg-gradient-to-r from-indigo-500 to-sky-400 transition-all duration-500"
                          style={{ width: `${Math.max(pct, 3)}%` }}
                        />
                      </div>
                    </button>
                  );
                })}
                {m.porColuna.length === 0 && (
                  <p className="text-center py-6 text-xs text-slate-400">Quadro sem cards</p>
                )}
              </div>
            </div>

            {/* Tempo Médio por Coluna (Identificação de Gargalos) */}
            <div className="rounded-2xl p-4 bg-white/70 dark:bg-slate-800/60 border border-white/60 dark:border-white/10 shadow-sm flex flex-col">
              <div className="flex items-center justify-between pb-2 border-b border-slate-200/50 dark:border-slate-700/50">
                <h3 className="text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-200 flex items-center gap-2">
                  <span>⏱️</span> Gargalos e Tempo Médio
                </h3>
                <span className="text-[10px] text-slate-400">Permanência média</span>
              </div>
              <div className="mt-3 space-y-2 flex-1 overflow-y-auto max-h-60 pr-1">
                {tempos.map((t) => {
                  const pct = Math.round((t.mediaSegundos / maxTempo) * 100);
                  const isGargalo = pct > 70 && t.mediaSegundos > 86400;
                  return (
                    <div key={t.coluna} className="p-1.5 rounded-xl">
                      <div className="flex justify-between items-center text-xs mb-1">
                        <span className="font-semibold text-slate-700 dark:text-slate-200 truncate flex items-center gap-1.5">
                          {isGargalo && <span title="Coluna com tempo elevado de permanência">🔥</span>}
                          {t.coluna}
                        </span>
                        <div className="flex items-center gap-2">
                          <span className="text-[10px] text-slate-400">{t.cards} cards</span>
                          <span className={`font-bold text-[11px] ${isGargalo ? 'text-amber-600 dark:text-amber-400' : 'text-slate-800 dark:text-white'}`}>
                            {formatarTempo(t.mediaSegundos)}
                          </span>
                        </div>
                      </div>
                      <div className="w-full h-2 rounded-full bg-slate-200 dark:bg-slate-700/60 overflow-hidden">
                        <div
                          className={`h-full rounded-full transition-all duration-500 ${
                            isGargalo
                              ? 'bg-gradient-to-r from-amber-500 to-red-500'
                              : 'bg-gradient-to-r from-sky-400 to-indigo-500'
                          }`}
                          style={{ width: `${Math.max(pct, 4)}%` }}
                        />
                      </div>
                    </div>
                  );
                })}
                {tempos.length === 0 && (
                  <p className="text-center py-6 text-xs text-slate-400">Sem histórico de tempo ainda</p>
                )}
              </div>
            </div>
          </div>
        </>
      )}
    </div>
  );

  if (props.embutido) return conteudo;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 backdrop-blur-sm" onClick={props.onFechar}>
      <div
        className="painel-modal rounded-3xl w-[58rem] max-w-[96vw] max-h-[90vh] overflow-y-auto p-6 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between pb-3 mb-2 border-b border-slate-200/50 dark:border-slate-700/50">
          <h2 className="font-bold text-lg text-slate-900 dark:text-white flex items-center gap-2">
            <span>📊</span> Painel Executivo do Quadro
          </h2>
          <button aria-label="Fechar" className="text-slate-400 hover:text-slate-600 dark:hover:text-slate-200 text-lg" onClick={props.onFechar}>
            ✕
          </button>
        </div>
        {conteudo}
      </div>
    </div>
  );
}
