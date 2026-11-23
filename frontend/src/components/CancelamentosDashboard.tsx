import { useEffect, useMemo, useState } from 'react';
import {
  PieChart, Pie, Cell, Tooltip, Legend, ResponsiveContainer,
  LineChart, Line, XAxis, YAxis, CartesianGrid,
  BarChart, Bar,
} from 'recharts';
import { CancelamentosDoMotivo, GetTickets, MetricasCancelamentos } from '../../wailsjs/go/main/App';
import type { Ticket } from '../wails-types';

// CancelamentosDashboard: abre pelo botão 🚪 no painel 📊. Mostra o total de
// cancelamentos (últimos 7/30/90 dias ou todos), quantos viraram reversão ou
// perda e a quebra por motivo — pizza clicável, linha por dia e barras por
// motivo. Dados do banco local (o que esta máquina fechou).

interface Metricas {
  total: number;
  reversao: number;
  perda: number;
  porMotivo: { motivo: string; total: number; reversao: number; perda: number }[];
  serie: { dia: string; reversao: number; perda: number }[];
}

interface Cancelamento {
  id: number;
  ticketId: number;
  motivo: string;
  resultado: string;
  criadoEm: string;
}

const CORES = ['#6366f1', '#f59e0b', '#10b981', '#ef4444', '#0ea5e9', '#a855f7', '#f97316', '#14b8a6'];

const PERIODOS: { rotulo: string; dias: number }[] = [
  { rotulo: '7 dias', dias: 7 },
  { rotulo: '30 dias', dias: 30 },
  { rotulo: '90 dias', dias: 90 },
  { rotulo: 'Tudo', dias: 0 },
];

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

export default function CancelamentosDashboard(props: {
  onFechar: () => void;
  onAbrirCard?: (t: Ticket) => void;
}) {
  const [dias, setDias] = useState(30);
  const [m, setM] = useState<Metricas | null>(null);
  const [erro, setErro] = useState('');
  const [motivoAberto, setMotivoAberto] = useState<string | null>(null);
  const [detalhe, setDetalhe] = useState<Cancelamento[]>([]);
  const [tickets, setTickets] = useState<Record<number, Ticket>>({});

  useEffect(() => {
    MetricasCancelamentos(dias)
      .then((d) => setM(d as unknown as Metricas))
      .catch((e) => setErro(String(e)));
    GetTickets()
      .then((ts) => {
        const idx: Record<number, Ticket> = {};
        for (const t of ts as unknown as Ticket[]) idx[t.id] = t;
        setTickets(idx);
      })
      .catch(() => setTickets({}));
  }, [dias]);

  async function clicarMotivo(nome: string) {
    const nomeAberto = motivoAberto === nome ? null : nome;
    setMotivoAberto(nomeAberto);
    if (!nomeAberto) return setDetalhe([]);
    try {
      const cs = await CancelamentosDoMotivo(nomeAberto);
      setDetalhe((cs as unknown as Cancelamento[]) || []);
    } catch {
      setDetalhe([]);
    }
  }

  const taxaReversao = useMemo(() => {
    if (!m || m.total === 0) return '—';
    return `${Math.round((m.reversao / m.total) * 100)}%`;
  }, [m]);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 backdrop-blur-sm" onClick={props.onFechar}>
      <div
        className="painel-modal rounded-3xl w-[46rem] max-w-[95vw] max-h-[90vh] overflow-y-auto p-5 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <h2 className="font-semibold text-lg dark:text-slate-100">🚪 Dashboard de cancelamentos</h2>
          <button aria-label="Fechar" className="text-slate-400 hover:text-slate-600" onClick={props.onFechar}>
            ✕
          </button>
        </div>

        <div className="mt-3 flex gap-1.5">
          {PERIODOS.map((p) => (
            <button
              key={p.dias}
              className={`text-xs rounded-lg px-2.5 py-1 border transition ${
                dias === p.dias
                  ? 'bg-blue-600 text-white border-blue-600'
                  : 'text-slate-600 dark:text-slate-300 hover:bg-slate-500/10'
              }`}
              onClick={() => setDias(p.dias)}
            >
              {p.rotulo}
            </button>
          ))}
        </div>

        {erro && <p className="mt-4 text-sm text-red-600 dark:text-red-300">{erro}</p>}
        {!m && !erro && <p className="mt-4 text-sm text-slate-500 dark:text-slate-400">carregando…</p>}

        {m && (
          <>
            <div className="mt-4 grid grid-cols-4 gap-2">
              <Numero rotulo="Cancelamentos" valor={String(m.total)} />
              <Numero rotulo="↩️ Reversões" valor={String(m.reversao)} destaque="text-emerald-600 dark:text-emerald-400" />
              <Numero rotulo="📉 Perdas" valor={String(m.perda)} destaque="text-red-600 dark:text-red-400" />
              <Numero rotulo="Taxa de reversão" valor={taxaReversao} destaque="text-indigo-600 dark:text-indigo-400" />
            </div>

            {m.total === 0 ? (
              <p className="mt-6 text-sm text-slate-500 dark:text-slate-400 text-center">
                Nenhum cancelamento registrado no período — feche um card de cancelamento para começar a contar.
              </p>
            ) : (
              <>
                <section className="mt-4 border rounded-lg p-3">
                  <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">Motivos (clique para ver os cards)</h3>
                  <div className="mt-2 h-56">
                    <ResponsiveContainer width="100%" height="100%">
                      <PieChart>
                        <Pie
                          data={m.porMotivo}
                          dataKey="total"
                          nameKey="motivo"
                          innerRadius={50}
                          outerRadius={80}
                          paddingAngle={2}
                          onClick={(data: { name?: unknown }) => clicarMotivo(String(data?.name ?? ''))}
                          className="cursor-pointer"
                        >
                          {m.porMotivo.map((_, i) => (
                            <Cell key={i} fill={CORES[i % CORES.length]} />
                          ))}
                        </Pie>
                        <Tooltip />
                        <Legend wrapperStyle={{ fontSize: 12 }} />
                      </PieChart>
                    </ResponsiveContainer>
                  </div>
                  {motivoAberto && (
                    <div className="mt-2 max-h-40 overflow-y-auto border-t pt-2">
                      {detalhe.map((c) => {
                        const t = tickets[c.ticketId];
                        return (
                          <button
                            key={c.id}
                            className="w-full flex items-center justify-between text-sm rounded px-1.5 py-0.5 hover:bg-slate-500/10 dark:hover:bg-white/10"
                            onClick={() => t && props.onAbrirCard?.(t)}
                            title={t ? 'Abrir o card' : ''}
                          >
                            <span className="truncate text-slate-600 dark:text-slate-300">{t?.nomeCliente ?? `card ${c.ticketId}`}</span>
                            <span className={c.resultado === 'reversao' ? 'text-emerald-600' : 'text-red-500'}>
                              {c.resultado === 'reversao' ? '↩️ reversão' : '📉 perda'} ·{' '}
                              {new Date(c.criadoEm).toLocaleDateString('pt-BR')}
                            </span>
                          </button>
                        );
                      })}
                      {detalhe.length === 0 && (
                        <p className="text-xs text-slate-400">carregando…</p>
                      )}
                    </div>
                  )}
                </section>

                <section className="mt-3 border rounded-lg p-3">
                  <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">Reversão × perda por motivo</h3>
                  <div className="mt-2 h-56">
                    <ResponsiveContainer width="100%" height="100%">
                      <BarChart data={m.porMotivo} layout="vertical" margin={{ left: 8 }}>
                        <CartesianGrid strokeDasharray="3 3" opacity={0.3} />
                        <XAxis type="number" allowDecimals={false} fontSize={11} />
                        <YAxis type="category" dataKey="motivo" width={120} fontSize={11} />
                        <Tooltip />
                        <Legend wrapperStyle={{ fontSize: 12 }} />
                        <Bar dataKey="reversao" name="Reversão" stackId="a" fill="#10b981" />
                        <Bar dataKey="perda" name="Perda" stackId="a" fill="#ef4444" />
                      </BarChart>
                    </ResponsiveContainer>
                  </div>
                </section>

                {m.serie.length > 1 && (
                  <section className="mt-3 border rounded-lg p-3">
                    <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">Cancelamentos por dia</h3>
                    <div className="mt-2 h-56">
                      <ResponsiveContainer width="100%" height="100%">
                        <LineChart data={m.serie} margin={{ left: -10 }}>
                          <CartesianGrid strokeDasharray="3 3" opacity={0.3} />
                          <XAxis dataKey="dia" fontSize={11} />
                          <YAxis allowDecimals={false} fontSize={11} />
                          <Tooltip />
                          <Legend wrapperStyle={{ fontSize: 12 }} />
                          <Line type="monotone" dataKey="reversao" name="Reversão" stroke="#10b981" strokeWidth={2} />
                          <Line type="monotone" dataKey="perda" name="Perda" stroke="#ef4444" strokeWidth={2} />
                        </LineChart>
                      </ResponsiveContainer>
                    </div>
                  </section>
                )}
              </>
            )}
          </>
        )}
      </div>
    </div>
  );
}
