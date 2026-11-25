import { useEffect, useMemo, useState } from 'react';
import {
  PieChart, Pie, Cell, Tooltip, Legend, ResponsiveContainer,
  LineChart, Line, XAxis, YAxis, CartesianGrid,
  BarChart, Bar,
} from 'recharts';
import {
  CancelamentosDoMotivo,
  GetColunas,
  GetTickets,
  ImportarCancelamentos,
  MetricasCancelamentos,
  PreviaImportarCancelamentos,
} from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import type { Ticket } from '../wails-types';

interface Metricas {
  total: number;
  reversao: number;
  perda: number;
  tratamento: number;
  porMotivo: { motivo: string; total: number; reversao: number; perda: number; tratamento: number }[];
  serie: { dia: string; reversao: number; perda: number }[];
}

interface LinhaImport {
  linha: number;
  nome: string;
  motivo: string;
  resultado: string;
  acao: string;
  ticketId: number;
  codService: string;
  coluna: string;
  ticket360: string;
  candidatos: string[];
}

interface PreviaImport {
  cancelados: LinhaImport[];
  revertidos: LinhaImport[];
  tratamento: LinhaImport[];
  ambiguos: LinhaImport[];
  semCard: LinhaImport[];
  erros: string[];
  totalLinhas: number;
}

interface ResumoImport {
  registrados: number;
  movidos: number;
  importados: number;
  jaFechados: number;
  falhas: string[];
}

interface Cancelamento {
  id: number;
  ticketId: number;
  motivo: string;
  resultado: string;
  criadoEm: string;
}

const CORES = ['#6366f1', '#0ea5e9', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899', '#14b8a6'];

// Nomes longos de motivo comprimem os gráficos (a legenda/eixo come o espaço
// da pizza e das barras): encurta nos rótulos; o nome completo fica no tooltip.
function encurtaNome(nome: string, max = 18): string {
  return nome.length > max ? `${nome.slice(0, max - 1)}…` : nome;
}

const PERIODOS: { rotulo: string; dias: number }[] = [
  { rotulo: '7 Dias', dias: 7 },
  { rotulo: '30 Dias', dias: 30 },
  { rotulo: '90 Dias', dias: 90 },
  { rotulo: 'Todo Histórico', dias: 0 },
];

interface KpiCardProps {
  titulo: string;
  valor: string;
  subtitulo?: string;
  icone: string;
  destaque?: string;
}

function KpiCard({ titulo, valor, subtitulo, icone, destaque }: KpiCardProps) {
  return (
    <div className="relative overflow-hidden rounded-2xl p-4 transition-all duration-200 hover:-translate-y-0.5 hover:shadow-lg border border-white/60 dark:border-white/10 bg-white/70 dark:bg-slate-800/60">
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

export default function CancelamentosDashboard(props: {
  onFechar?: () => void;
  onAbrirCard?: (t: Ticket) => void;
  embutido?: boolean;
}) {
  const [dias, setDias] = useState(30);
  // Desligado = só finalizados (reversão/perda); ligado inclui em tratamento.
  const [todos, setTodos] = useState(false);
  const [m, setM] = useState<Metricas | null>(null);
  const [erro, setErro] = useState('');
  const [motivoAberto, setMotivoAberto] = useState<string | null>(null);
  const [detalhe, setDetalhe] = useState<Cancelamento[]>([]);
  const [tickets, setTickets] = useState<Record<number, Ticket>>({});
  const [importador, setImportador] = useState(false);
  const [texto, setTexto] = useState('');
  const [previa, setPrevia] = useState<PreviaImport | null>(null);
  const [colunaTratamento, setColunaTratamento] = useState('');
  const [colunas, setColunas] = useState<string[]>([]);
  const [importando, setImportando] = useState(false);
  const [resumo, setResumo] = useState<ResumoImport | null>(null);
  const [erroImport, setErroImport] = useState('');
  // Desfecho do reenvio da lista ao site (evento do backend em background).
  const [avisoSite, setAvisoSite] = useState<{ ok: boolean; mensagem: string } | null>(null);

  function recarregar() {
    MetricasCancelamentos(dias, todos)
      .then((d) => setM(d as unknown as Metricas))
      .catch((e) => setErro(String(e)));
  }

  useEffect(() => {
    recarregar();
    GetTickets()
      .then((ts) => {
        const idx: Record<number, Ticket> = {};
        for (const t of ts as unknown as Ticket[]) idx[t.id] = t;
        setTickets(idx);
      })
      .catch(() => setTickets({}));

    const off = EventsOn('cancelamentos:mudou', () => {
      recarregar();
    });
    const offSite = EventsOn('cancelamentos:sync-site', (d: { ok?: boolean; mensagem?: string }) => {
      if (!d || !d.mensagem) return;
      setAvisoSite({ ok: !!d.ok, mensagem: d.mensagem });
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
    return () => {
      if (typeof off === 'function') off();
      if (typeof offSite === 'function') offSite();
    };
  }, [dias, todos]);

  async function abrirImportador() {
    setImportador(true);
    setPrevia(null);
    setResumo(null);
    setErroImport('');
    try {
      setColunas((await GetColunas()) as unknown as string[]);
    } catch {
      setColunas([]);
    }
  }

  async function analisar() {
    setErroImport('');
    setResumo(null);
    setPrevia(null);
    try {
      setPrevia((await PreviaImportarCancelamentos(texto)) as unknown as PreviaImport);
    } catch (e) {
      setErroImport(String(e));
    }
  }

  function aplicarCodService(linhaNum: number, cod: string) {
    if (!cod.trim()) return;
    const linhas = texto.split('\n');
    const idx = linhaNum - 1;
    if (idx >= 0 && idx < linhas.length) {
      const l = linhas[idx];
      const partes = l.split('-');
      if (partes.length >= 2) {
        const nomeOriginal = partes[0].trim();
        const resto = partes.slice(1).map((p) => p.trim()).join(' - ');
        linhas[idx] = `${nomeOriginal} - ${cod.trim()} - ${resto}`;
        setTexto(linhas.join('\n'));
      }
    }
  }

  async function importar() {
    setImportando(true);
    setErroImport('');
    try {
      const r = (await ImportarCancelamentos(texto, colunaTratamento)) as unknown as ResumoImport;
      setResumo(r);
      setPrevia(null);
      recarregar();
    } catch (e) {
      setErroImport(String(e));
    } finally {
      setImportando(false);
    }
  }

  useEffect(() => {
    if (!motivoAberto) return setDetalhe([]);
    CancelamentosDoMotivo(motivoAberto, todos)
      .then((cs) => setDetalhe((cs as unknown as Cancelamento[]) || []))
      .catch(() => setDetalhe([]));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [motivoAberto, todos]);

  async function clicarMotivo(nome: string) {
    const nomeAberto = motivoAberto === nome ? null : nome;
    setMotivoAberto(nomeAberto);
    if (!nomeAberto) return setDetalhe([]);
  }

  const barraEncurtada = useMemo(
    () => (m?.porMotivo ?? []).map((x) => ({ ...x, motivo: encurtaNome(x.motivo, 20) })),
    [m],
  );

  const fechados = useMemo(() => {
    if (!m) return 0;
    return m.reversao + m.perda;
  }, [m]);

  const taxaReversao = useMemo(() => {
    if (!m || fechados === 0) return '—';
    return `${Math.round((m.reversao / fechados) * 100)}%`;
  }, [m, fechados]);

  const conteudo = (
    <div className="space-y-5">
      {avisoSite && (
        <div
          className={`flex items-start justify-between gap-2 rounded-xl border px-3 py-2 text-xs ${
            avisoSite.ok
              ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
              : 'border-red-500/40 bg-red-500/10 text-red-700 dark:text-red-300'
          }`}
        >
          <span>{avisoSite.ok ? '✅ ' : '⚠️ '}{avisoSite.mensagem}</span>
          <button
            className="shrink-0 opacity-60 hover:opacity-100"
            title="Fechar aviso"
            onClick={() => setAvisoSite(null)}
          >
            ✕
          </button>
        </div>
      )}
      {/* Barra de Filtro de Período e Ação de Importar */}
      <div className="flex flex-wrap items-center justify-between gap-3 pb-2 border-b border-slate-200/50 dark:border-slate-700/50">
        <div className="flex items-center gap-1.5 p-1 rounded-xl bg-slate-500/10 dark:bg-white/5">
          {PERIODOS.map((p) => (
            <button
              key={p.dias}
              className={`text-xs px-3 py-1.5 rounded-lg font-medium transition ${
                dias === p.dias
                  ? 'bg-white dark:bg-slate-700 text-indigo-600 dark:text-indigo-300 shadow-sm'
                  : 'text-slate-600 dark:text-slate-400 hover:text-slate-900 dark:hover:text-white'
              }`}
              onClick={() => setDias(p.dias)}
            >
              {p.rotulo}
            </button>
          ))}
        </div>
        <div className="flex items-center gap-2">
          <button
            className={`flex items-center gap-2 text-xs font-medium rounded-xl px-3 py-2 border transition ${
              todos
                ? 'bg-amber-500/15 border-amber-500/40 text-amber-700 dark:text-amber-300'
                : 'bg-slate-500/10 border-transparent text-slate-600 dark:text-slate-300 hover:bg-slate-500/20'
            }`}
            onClick={() => setTodos((t) => !t)}
            title={
              todos
                ? 'Mostrando todos, inclusive quem ainda está em tratativa'
                : 'Mostrando só os finalizados (reversão/perda)'
            }
          >
            <span
              className="relative inline-block rounded-full transition"
              style={{ height: 18, width: 32, background: todos ? '#f59e0b' : 'rgba(148,163,184,.6)' }}
            >
              <span
                className="absolute rounded-full bg-white shadow transition-all"
                style={{ height: 14, width: 14, top: 2, left: 2, transform: todos ? 'translateX(14px)' : 'translateX(0)' }}
              />
            </span>
            Incluir em tratamento
          </button>
          <button
            className="text-xs font-semibold rounded-xl px-3.5 py-2 bg-gradient-to-r from-blue-600 to-indigo-600 hover:from-blue-500 hover:to-indigo-500 text-white shadow hover:shadow-md transition flex items-center gap-1.5"
            onClick={abrirImportador}
            title="Colar uma lista pronta e importar para o sistema e nuvem"
          >
            <span>📋</span> Colar Lista de Cancelamentos
          </button>
        </div>
      </div>

      {/* Painel do Importador / Análise */}
      {importador && (
        <div className="p-4 rounded-2xl border border-indigo-500/20 bg-indigo-500/5 space-y-3">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-bold text-slate-800 dark:text-slate-100 flex items-center gap-2">
              <span>📋</span> Importador Inteligente de Cancelamentos
            </h3>
            <button
              className="text-slate-400 hover:text-slate-600 text-sm p-1"
              onClick={() => {
                setImportador(false);
                setPrevia(null);
                setResumo(null);
              }}
            >
              ✕
            </button>
          </div>
          <p className="text-xs text-slate-500 dark:text-slate-400">
            Formato: <code>NOME - MOTIVO - CANCELADO</code> (perda), <code>... - REVERTIDO</code> (reversão) ou só{' '}
            <code>NOME - MOTIVO</code> (em tratamento com ticket em aberto).
          </p>
          <textarea
            className="w-full h-36 rounded-xl border border-slate-300 dark:border-slate-700 bg-white/80 dark:bg-slate-850 p-2.5 text-xs font-mono focus:ring-2 focus:ring-indigo-400 outline-none"
            placeholder={'CLIENTE EXEMPLO - TROCA DE ENDERECO - CANCELADO\nOUTRO CLIENTE - PREÇO'}
            value={texto}
            onChange={(e) => setTexto(e.target.value)}
          />

          {previa && (
            <div className="space-y-3 pt-2">
              <div className="grid grid-cols-4 gap-2 text-center text-xs">
                <div className="rounded-xl bg-red-500/10 p-2 border border-red-500/20">
                  <div className="text-lg font-black text-red-600 dark:text-red-400">{previa.cancelados?.length ?? 0}</div>
                  <div className="text-[10px] text-slate-500">📉 Cancelados</div>
                </div>
                <div className="rounded-xl bg-emerald-500/10 p-2 border border-emerald-500/20">
                  <div className="text-lg font-black text-emerald-600 dark:text-emerald-400">{previa.revertidos?.length ?? 0}</div>
                  <div className="text-[10px] text-slate-500">↩️ Revertidos</div>
                </div>
                <div className="rounded-xl bg-amber-500/10 p-2 border border-amber-500/20">
                  <div className="text-lg font-black text-amber-600 dark:text-amber-400">{previa.tratamento?.length ?? 0}</div>
                  <div className="text-[10px] text-slate-500">⏳ Em tratamento</div>
                </div>
                <div className="rounded-xl bg-slate-500/10 p-2 border border-slate-500/20">
                  <div className="text-lg font-black text-slate-700 dark:text-slate-300">{previa.totalLinhas ?? 0}</div>
                  <div className="text-[10px] text-slate-500">Total Linhas</div>
                </div>
              </div>

              {(previa.tratamento?.length ?? 0) > 0 && (
                <div className="flex items-center gap-2 text-xs">
                  <span className="text-slate-600 dark:text-slate-300 font-medium">
                    Coluna de destino dos "Em tratamento":
                  </span>
                  <select
                    className="rounded-xl border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 px-3 py-1.5 text-xs outline-none"
                    value={colunaTratamento}
                    onChange={(e) => setColunaTratamento(e.target.value)}
                  >
                    <option value="">Escolha a coluna…</option>
                    {colunas.map((c) => (
                      <option key={c} value={c}>{c}</option>
                    ))}
                  </select>
                </div>
              )}

              {(previa.semCard?.length ?? 0) > 0 && (
                <div className="p-3 rounded-xl border border-amber-500/30 bg-amber-500/10 space-y-2">
                  <p className="text-xs font-bold text-amber-800 dark:text-amber-300">
                    ⚠️ {previa.semCard.length} cliente(s) não foram localizados pelo nome no 360. Forneça o código service:
                  </p>
                  <div className="space-y-1.5 max-h-36 overflow-y-auto pr-1">
                    {previa.semCard.map((x) => (
                      <div key={x.linha} className="flex items-center justify-between gap-2 text-xs bg-white/60 dark:bg-slate-800/60 p-1.5 rounded-lg">
                        <span className="font-semibold text-slate-700 dark:text-slate-200 truncate">
                          {x.nome}
                        </span>
                        <div className="flex items-center gap-1.5 shrink-0">
                          <input
                            type="text"
                            placeholder="Cód. Service"
                            className="w-24 px-2 py-0.5 rounded border text-xs bg-white dark:bg-slate-900"
                            id={`cod-semcard-${x.linha}`}
                            onKeyDown={(e) => {
                              if (e.key === 'Enter') {
                                aplicarCodService(x.linha, (e.target as HTMLInputElement).value);
                                (e.target as HTMLInputElement).value = '';
                              }
                            }}
                          />
                          <button
                            type="button"
                            className="px-2.5 py-0.5 rounded bg-indigo-600 text-white hover:bg-indigo-500 text-[11px] font-medium"
                            onClick={() => {
                              const input = document.getElementById(`cod-semcard-${x.linha}`) as HTMLInputElement;
                              if (input && input.value.trim()) {
                                aplicarCodService(x.linha, input.value.trim());
                                input.value = '';
                              }
                            }}
                          >
                            OK
                          </button>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}

          {resumo && (
            <div className="p-3 rounded-xl bg-emerald-500/10 border border-emerald-500/20 text-xs text-emerald-800 dark:text-emerald-300">
              ✅ {resumo.registrados ?? 0} registrados · {resumo.movidos ?? 0} movidos · {resumo.importados ?? 0} criados do 360 · {resumo.jaFechados ?? 0} já na coluna.
            </div>
          )}

          {erroImport && (
            <div className="p-2.5 rounded-xl bg-red-500/10 border border-red-500/20 text-xs text-red-600 dark:text-red-400">
              {erroImport}
            </div>
          )}

          <div className="flex gap-2 pt-1">
            <button
              className="text-xs font-semibold rounded-xl px-3 py-1.5 border border-slate-300 dark:border-slate-700 hover:bg-slate-500/10 text-slate-700 dark:text-slate-200 disabled:opacity-50"
              onClick={analisar}
              disabled={!texto.trim()}
            >
              🔍 Analisar Prévia
            </button>
            <button
              className="text-xs font-semibold rounded-xl px-4 py-1.5 bg-emerald-600 hover:bg-emerald-500 text-white shadow disabled:opacity-50"
              onClick={importar}
              disabled={!previa || importando}
            >
              {importando ? 'Sincronizando…' : '✅ Gravar e Sincronizar'}
            </button>
          </div>
        </div>
      )}

      {erro && (
        <div className="p-3 rounded-xl bg-red-500/10 border border-red-500/20 text-red-600 dark:text-red-400 text-sm">
          {erro}
        </div>
      )}
      {!m && !erro && (
        <div className="flex items-center justify-center py-12 text-slate-400">
          <span className="animate-pulse">Carregando indicadores de cancelamento…</span>
        </div>
      )}

      {m && (
        <>
          {/* KPIs Executivos */}
          <div className="grid grid-cols-2 md:grid-cols-5 gap-3">
            <KpiCard
              titulo="Total Analisado"
              valor={String(m.total)}
              subtitulo="Demandas de cancelamento"
              icone="🚪"
            />
            <KpiCard
              titulo="Reversões"
              valor={String(m.reversao)}
              subtitulo="Clientes mantidos"
              icone="↩️"
              destaque="text-emerald-600 dark:text-emerald-400"
            />
            <KpiCard
              titulo="Perdas"
              valor={String(m.perda)}
              subtitulo="Contratos cancelados"
              icone="📉"
              destaque="text-red-600 dark:text-red-400"
            />
            <KpiCard
              titulo="Em Tratamento"
              valor={String(m.tratamento)}
              subtitulo="Negociações abertas"
              icone="⏳"
              destaque="text-amber-600 dark:text-amber-400"
            />
            <KpiCard
              titulo="Taxa de Reversão"
              valor={taxaReversao}
              subtitulo="Eficácia de retenção"
              icone="🎯"
              destaque="text-indigo-600 dark:text-indigo-400"
            />
          </div>

          {m.total === 0 ? (
            <div className="py-12 text-center text-slate-400">
              Nenhum cancelamento registrado no período. Importe uma lista ou encerre um card de cancelamento.
            </div>
          ) : (
            <>
              {/* Gráficos Interativos Power BI */}
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                {/* Donut Chart com Drill-down */}
                <div className="rounded-2xl p-4 bg-white/70 dark:bg-slate-800/60 border border-white/60 dark:border-white/10 shadow-sm flex flex-col">
                  <div className="flex items-center justify-between pb-2 border-b border-slate-200/50 dark:border-slate-700/50">
                    <h3 className="text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-200 flex items-center gap-2">
                      <span>🍩</span> Composição por Motivo
                    </h3>
                    <span className="text-[10px] text-slate-400">Clique na fatia para detalhar</span>
                  </div>
                  <div className="mt-2 h-72">
                    <ResponsiveContainer width="100%" height="100%">
                      <PieChart>
                        <Pie
                          data={m.porMotivo}
                          dataKey="total"
                          nameKey="motivo"
                          innerRadius={55}
                          outerRadius={85}
                          paddingAngle={3}
                          onClick={(data: { name?: unknown }) => clicarMotivo(String(data?.name ?? ''))}
                          className="cursor-pointer focus:outline-none"
                        >
                          {m.porMotivo.map((_, i) => (
                            <Cell key={i} fill={CORES[i % CORES.length]} />
                          ))}
                        </Pie>
                        <Tooltip />
                        <Legend
                          wrapperStyle={{ fontSize: 11, maxWidth: '45%' }}
                          formatter={(valor) => {
                            const nome = String(valor ?? '');
                            return <span title={nome}>{encurtaNome(nome)}</span>;
                          }}
                        />
                      </PieChart>
                    </ResponsiveContainer>
                  </div>
                  {motivoAberto && (
                    <div className="mt-3 p-2.5 rounded-xl bg-slate-500/10 dark:bg-white/5 border border-slate-200/50 dark:border-slate-700/50 max-h-40 overflow-y-auto">
                      <div className="flex items-center justify-between mb-1 text-[11px] font-bold text-slate-700 dark:text-slate-200">
                        <span>Clientes: {motivoAberto}</span>
                        <span className="text-[10px] text-slate-400">{detalhe.length} ocorrência(s)</span>
                      </div>
                      {detalhe.map((c) => {
                        const t = tickets[c.ticketId];
                        return (
                          <button
                            key={c.id}
                            className="w-full flex items-center justify-between text-xs rounded-lg px-2 py-1 hover:bg-slate-500/10 transition"
                            onClick={() => t && props.onAbrirCard?.(t)}
                            title={t ? 'Abrir card no quadro' : ''}
                          >
                            <span className="truncate font-medium text-slate-700 dark:text-slate-300">
                              {t?.nomeCliente ?? `Card #${c.ticketId}`}
                            </span>
                            <span className={`text-[11px] font-semibold ${c.resultado === 'reversao' ? 'text-emerald-600' : 'text-red-500'}`}>
                              {c.resultado === 'reversao' ? '↩️ Reversão' : '📉 Perda'}
                            </span>
                          </button>
                        );
                      })}
                    </div>
                  )}
                </div>

                {/* Gráfico de Barras Segmentadas */}
                <div className="rounded-2xl p-4 bg-white/70 dark:bg-slate-800/60 border border-white/60 dark:border-white/10 shadow-sm flex flex-col">
                  <div className="flex items-center justify-between pb-2 border-b border-slate-200/50 dark:border-slate-700/50">
                    <h3 className="text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-200 flex items-center gap-2">
                      <span>📊</span> Reversão × Perda por Motivo
                    </h3>
                    <span className="text-[10px] text-slate-400">Segmentação direta</span>
                  </div>
                  <div className="mt-2 h-72">
                    <ResponsiveContainer width="100%" height="100%">
                      <BarChart data={barraEncurtada} layout="vertical" margin={{ left: 10, right: 10 }}>
                        <CartesianGrid strokeDasharray="3 3" opacity={0.2} />
                        <XAxis type="number" allowDecimals={false} fontSize={10} />
                        <YAxis
                          type="category"
                          dataKey="motivo"
                          width={150}
                          fontSize={10}
                          tick={{ fill: '#888' }}
                          tickFormatter={(v: unknown) => encurtaNome(String(v ?? ''))}
                        />
                        <Tooltip />
                        <Legend wrapperStyle={{ fontSize: 11 }} />
                        <Bar dataKey="reversao" name="Reversão" stackId="a" fill="#10b981" radius={[0, 0, 0, 0]} />
                        <Bar dataKey="perda" name="Perda" stackId="a" fill="#ef4444" radius={[0, 0, 0, 0]} />
                        <Bar dataKey="tratamento" name="Em tratamento" stackId="a" fill="#f59e0b" radius={[0, 4, 4, 0]} />
                      </BarChart>
                    </ResponsiveContainer>
                  </div>
                </div>
              </div>

              {/* Tendência Diária (Série Temporal) */}
              {m.serie.length > 1 && (
                <div className="rounded-2xl p-4 bg-white/70 dark:bg-slate-800/60 border border-white/60 dark:border-white/10 shadow-sm">
                  <div className="flex items-center justify-between pb-2 border-b border-slate-200/50 dark:border-slate-700/50">
                    <h3 className="text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-200 flex items-center gap-2">
                      <span>📈</span> Evolução Temporal dos Cancelamentos
                    </h3>
                    <span className="text-[10px] text-slate-400">Tendência diária</span>
                  </div>
                  <div className="mt-2 h-56">
                    <ResponsiveContainer width="100%" height="100%">
                      <LineChart data={m.serie} margin={{ left: -10, right: 10 }}>
                        <CartesianGrid strokeDasharray="3 3" opacity={0.2} />
                        <XAxis dataKey="dia" fontSize={10} tick={{ fill: '#888' }} />
                        <YAxis allowDecimals={false} fontSize={10} tick={{ fill: '#888' }} />
                        <Tooltip />
                        <Legend wrapperStyle={{ fontSize: 11 }} />
                        <Line type="monotone" dataKey="reversao" name="Reversão" stroke="#10b981" strokeWidth={2.5} dot={{ r: 3 }} />
                        <Line type="monotone" dataKey="perda" name="Perda" stroke="#ef4444" strokeWidth={2.5} dot={{ r: 3 }} />
                      </LineChart>
                    </ResponsiveContainer>
                  </div>
                </div>
              )}
            </>
          )}
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
            <span>🚪</span> Dashboard Executivo de Cancelamentos
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
