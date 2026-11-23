import { useEffect, useMemo, useState } from 'react';
import {
  PieChart, Pie, Cell, Tooltip, Legend, ResponsiveContainer,
  LineChart, Line, XAxis, YAxis, CartesianGrid,
  BarChart, Bar,
} from 'recharts';
import { CancelamentosDoMotivo, GetColunas, GetTickets, ImportarCancelamentos, MetricasCancelamentos, PreviaImportarCancelamentos } from '../../wailsjs/go/main/App';
import type { Ticket } from '../wails-types';

// CancelamentosDashboard: abre pelo botão 🚪 no painel 📊. Mostra o total de
// cancelamentos (últimos 7/30/90 dias ou todos), quantos viraram reversão,
// perda ou seguem em tratamento e a quebra por motivo — pizza clicável, linha
// por dia e barras por motivo. O botão 📋 cola uma lista pronta
// ("NOME - MOTIVO - CANCELADO") e importa tudo de uma vez.

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
  onFechar?: () => void;
  onAbrirCard?: (t: Ticket) => void;
  embutido?: boolean;
}) {
  const [dias, setDias] = useState(30);
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

  function recarregar() {
    MetricasCancelamentos(dias)
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
  }, [dias]);

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

  const fechados = useMemo(() => {
    if (!m) return 0;
    return m.reversao + m.perda;
  }, [m]);

  const taxaReversao = useMemo(() => {
    if (!m || fechados === 0) return '—';
    return `${Math.round((m.reversao / fechados) * 100)}%`;
  }, [m, fechados]);

  // Com embutido=true, o modal e o título ficam por conta do DashboardPainel
  // (que alterna entre o painel do quadro e este); aqui sobra só o conteúdo,
  // com o botão do importador em toolbar própria.
  const cabecalho = props.embutido ? (
    <div className="flex items-center justify-end">
      <button
        className="text-xs rounded-lg px-2.5 py-1.5 border border-blue-500/60 text-blue-600 dark:text-blue-300 hover:bg-blue-500/10"
        onClick={abrirImportador}
        title="Colar uma lista pronta e importar tudo"
      >
        📋 Colar lista
      </button>
    </div>
  ) : (
    <div className="flex items-center justify-between">
      <h2 className="font-semibold text-lg dark:text-slate-100">🚪 Dashboard de cancelamentos</h2>
      <div className="flex items-center gap-2">
        <button
          className="text-xs rounded-lg px-2.5 py-1.5 border border-blue-500/60 text-blue-600 dark:text-blue-300 hover:bg-blue-500/10"
          onClick={abrirImportador}
          title="Colar uma lista pronta e importar tudo"
        >
          📋 Colar lista
        </button>
        <button aria-label="Fechar" className="text-slate-400 hover:text-slate-600" onClick={props.onFechar}>
          ✕
        </button>
      </div>
    </div>
  );

  const conteudo = (
    <>
        {cabecalho}

        {importador && (
          <div className="mt-3 border rounded-xl p-3 space-y-2">
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">
                Colar lista de cancelamentos
              </h3>
              <button
                className="text-slate-400 hover:text-slate-600 text-sm"
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
              Uma linha por cliente: <code>NOME - MOTIVO - CANCELADO</code>, <code>... - REVERTIDO</code> ou só{' '}
              <code>NOME - MOTIVO</code> (em tratamento, ticket em aberto).
            </p>
            <textarea
              className="w-full h-40 rounded-lg border bg-white/70 dark:bg-slate-800/70 p-2 text-sm font-mono"
              placeholder={'SELVINO ALOISIO CLOSSMANN - TROCA DE ENDEREÇO - CANCELADO\nROSANE ELIA PAUSE - NÃO SABEMOS O MOTIVO'}
              value={texto}
              onChange={(e) => setTexto(e.target.value)}
            />
            {previa && (
              <>
                <div className="grid grid-cols-4 gap-2 text-center text-sm">
                  <div className="rounded-lg bg-red-500/10 p-1.5">
                    <div className="font-bold text-red-600 dark:text-red-400">{previa.cancelados?.length ?? 0}</div>
                    <div className="text-xs text-slate-500">📉 cancelados</div>
                  </div>
                  <div className="rounded-lg bg-emerald-500/10 p-1.5">
                    <div className="font-bold text-emerald-600 dark:text-emerald-400">{previa.revertidos?.length ?? 0}</div>
                    <div className="text-xs text-slate-500">↩️ revertidos</div>
                  </div>
                  <div className="rounded-lg bg-amber-500/10 p-1.5">
                    <div className="font-bold text-amber-600 dark:text-amber-400">{previa.tratamento?.length ?? 0}</div>
                    <div className="text-xs text-slate-500">⏳ em tratamento</div>
                  </div>
                  <div className="rounded-lg bg-slate-500/10 p-1.5">
                    <div className="font-bold text-slate-600 dark:text-slate-300">{previa.totalLinhas ?? 0}</div>
                    <div className="text-xs text-slate-500">linhas</div>
                  </div>
                </div>
                {(previa.tratamento?.length ?? 0) > 0 && (
                  <div className="flex items-center gap-2 text-sm">
                    <span className="text-slate-600 dark:text-slate-300 whitespace-nowrap">
                      Mover os em tratamento para:
                    </span>
                    <select
                      className="rounded-lg border bg-white/70 dark:bg-slate-800/70 px-2 py-1 text-sm"
                      value={colunaTratamento}
                      onChange={(e) => setColunaTratamento(e.target.value)}
                    >
                      <option value="">escolha a coluna…</option>
                      {colunas.map((c) => (
                        <option key={c} value={c}>
                          {c}
                        </option>
                      ))}
                    </select>
                  </div>
                )}
                {(previa.ambiguos?.length ?? 0) > 0 && (
                  <ul className="text-xs text-orange-600 dark:text-orange-300 list-disc list-inside">
                    {previa.ambiguos.map((x) => (
                      <li key={x.linha}>
                        {x.nome}: casa com vários cards ({(x.candidatos ?? []).join(', ')}) — resolva no quadro
                      </li>
                    ))}
                  </ul>
                )}
                {(() => {
                  const todas = [...(previa.cancelados ?? []), ...(previa.revertidos ?? []), ...(previa.tratamento ?? [])];
                  const criar = todas.filter((x) => x.acao === 'criar').length;
                  return criar > 0 ? (
                    <p className="text-xs text-slate-500 dark:text-slate-400">
                      {criar} vão ganhar um segundo card criado do 360 (o card atual não tem ticket de cancelamento vinculado).
                    </p>
                  ) : null;
                })()}
                {(previa.semCard?.length ?? 0) > 0 && (
                  <p className="text-xs text-orange-600 dark:text-orange-300">
                    {previa.semCard.length} não achei no 360 — conecte a sessão e importe de novo.
                  </p>
                )}
                {(previa.erros ?? []).map((e, i) => (
                  <p key={i} className="text-xs text-red-600 dark:text-red-300">
                    {e}
                  </p>
                ))}
              </>
            )}
            {resumo && (
              <div className="rounded-lg border p-2 text-sm space-y-1">
                <p className="text-emerald-600 dark:text-emerald-400">
                  ✅ {resumo.registrados ?? 0} registrados · {resumo.movidos ?? 0} movidos · {resumo.importados ?? 0} criados do 360 ·{' '}
                  {resumo.jaFechados ?? 0} já na coluna
                </p>
                {(resumo.falhas ?? []).map((f, i) => (
                  <p key={i} className="text-xs text-red-600 dark:text-red-300">
                    {f}
                  </p>
                ))}
              </div>
            )}
            {erroImport && <p className="text-sm text-red-600 dark:text-red-300">{erroImport}</p>}
            <div className="flex gap-2">
              <button
                className="text-xs rounded-lg px-2.5 py-1.5 border text-slate-600 dark:text-slate-300 hover:bg-slate-500/10 disabled:opacity-50"
                onClick={analisar}
                disabled={!texto.trim()}
              >
                🔍 Analisar
              </button>
              <button
                className="text-xs rounded-lg px-2.5 py-1.5 bg-blue-600 text-white hover:bg-blue-700 disabled:opacity-50"
                onClick={importar}
                disabled={!previa || importando}
              >
                {importando ? 'importando…' : '✅ Importar'}
              </button>
            </div>
          </div>
        )}

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
            <div className="mt-4 grid grid-cols-5 gap-2">
              <Numero rotulo="Registros" valor={String(m.total)} />
              <Numero rotulo="↩️ Reversões" valor={String(m.reversao)} destaque="text-emerald-600 dark:text-emerald-400" />
              <Numero rotulo="📉 Perdas" valor={String(m.perda)} destaque="text-red-600 dark:text-red-400" />
              <Numero rotulo="⏳ Em tratamento" valor={String(m.tratamento)} destaque="text-amber-600 dark:text-amber-400" />
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
                        <Bar dataKey="tratamento" name="Em tratamento" stackId="a" fill="#f59e0b" />
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
    </>
  );

  if (props.embutido) return conteudo;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 backdrop-blur-sm" onClick={props.onFechar}>
      <div
        className="painel-modal rounded-3xl w-[46rem] max-w-[95vw] max-h-[90vh] overflow-y-auto p-5 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        {conteudo}
      </div>
    </div>
  );
}
