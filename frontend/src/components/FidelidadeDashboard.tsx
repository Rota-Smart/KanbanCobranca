import { useMemo, useState } from 'react';
import * as App from '../../wailsjs/go/main/App';
import type { ResumoCliente360 } from '../wails-types';

// FidelidadeDashboard: visão e ação sobre a base de clientes do programa
// (fonte: busca de clientes do 360, mesma usada na pesquisa). Cabeçalho com
// números clicáveis que filtram a lista, distribuição visual por status e
// lista interativa com ordenação, filtro e abertura da visão 360.

type FiltroStatus = 'todos' | 'ativos' | 'inativos' | 'semcpf';
type Ordem = 'nome' | 'situacao' | 'codigo';

function situacaoAtiva(r: ResumoCliente360): boolean {
  return (r.Situacao || '').trim().toLowerCase() === 'ativo';
}

function situacaoCurta(r: ResumoCliente360): string {
  const s = (r.Situacao || '').trim();
  return s || '—';
}

export default function FidelidadeDashboard(props: { onFechar: () => void }) {
  const [termo, setTermo] = useState('');
  const [clientes, setClientes] = useState<ResumoCliente360[] | null>(null);
  const [carregando, setCarregando] = useState(false);
  const [erro, setErro] = useState('');
  const [filtro, setFiltro] = useState<FiltroStatus>('todos');
  const [ordem, setOrdem] = useState<Ordem>('nome');
  const [ordemAsc, setOrdemAsc] = useState(true);
  const [expandido, setExpandido] = useState<number | null>(null);

  async function carregar() {
    const q = termo.trim();
    if (!q) return;
    setCarregando(true);
    setErro('');
    setExpandido(null);
    try {
      setClientes(await App.Rota360BuscarClientes(q));
    } catch (e) {
      setErro(String(e));
      setClientes([]);
    } finally {
      setCarregando(false);
    }
  }

  const resumo = useMemo(() => {
    const lista = clientes ?? [];
    const ativos = lista.filter(situacaoAtiva).length;
    const semCpf = lista.filter((r) => !(r.CGCCPF || '').trim()).length;
    return { total: lista.length, ativos, inativos: lista.length - ativos, semCpf };
  }, [clientes]);

  const lista = useMemo(() => {
    let l = clientes ?? [];
    if (filtro === 'ativos') l = l.filter(situacaoAtiva);
    else if (filtro === 'inativos') l = l.filter((r) => !situacaoAtiva(r));
    else if (filtro === 'semcpf') l = l.filter((r) => !(r.CGCCPF || '').trim());
    const sinal = ordemAsc ? 1 : -1;
    return [...l].sort((a, b) => {
      if (ordem === 'codigo') return (a.CodCliente - b.CodCliente) * sinal;
      if (ordem === 'situacao') return situacaoCurta(a).localeCompare(situacaoCurta(b), 'pt-BR') * sinal || a.Nome.localeCompare(b.Nome, 'pt-BR');
      return a.Nome.localeCompare(b.Nome, 'pt-BR') * sinal;
    });
  }, [clientes, filtro, ordem, ordemAsc]);

  function clicarCard(f: FiltroStatus) {
    setFiltro((atual) => (atual === f ? 'todos' : f));
  }

  const pctAtivos = resumo.total > 0 ? Math.round((resumo.ativos / resumo.total) * 100) : 0;

  const cards: { chave: FiltroStatus; rotulo: string; valor: number; cor: string }[] = [
    { chave: 'todos', rotulo: 'Clientes na base', valor: resumo.total, cor: 'text-blue-600 dark:text-blue-300' },
    { chave: 'ativos', rotulo: 'Ativos', valor: resumo.ativos, cor: 'text-emerald-600 dark:text-emerald-300' },
    { chave: 'inativos', rotulo: 'Inativos', valor: resumo.inativos, cor: 'text-rose-600 dark:text-rose-300' },
    { chave: 'semcpf', rotulo: 'Sem CPF/CNPJ', valor: resumo.semCpf, cor: 'text-amber-600 dark:text-amber-300' },
  ];

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 backdrop-blur-sm" onClick={props.onFechar}>
      <div
        className="painel-modal rounded-3xl w-[46rem] max-w-full max-h-[85vh] overflow-y-auto p-5 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <h2 className="font-semibold dark:text-slate-100">🎁 Fidelidade — dashboard de clientes</h2>
          <button aria-label="Fechar" className="text-slate-400 hover:text-slate-600" onClick={props.onFechar}>
            ✕
          </button>
        </div>

        {/* Busca: carrega a base do 360 (mesma fonte da pesquisa de clientes). */}
        <form
          className="mt-3 flex gap-1"
          onSubmit={(e) => {
            e.preventDefault();
            carregar();
          }}
        >
          <input
            className="flex-1 border rounded px-2 py-1 text-sm"
            placeholder="Buscar base no 360: nome, cidade ou código do cliente"
            value={termo}
            onChange={(e) => setTermo(e.target.value)}
            autoFocus
          />
          <button type="submit" className="bg-blue-600 text-white rounded px-3 py-1 text-sm disabled:opacity-50" disabled={carregando || !termo.trim()}>
            {carregando ? '…' : 'Carregar'}
          </button>
        </form>

        {erro && <p className="mt-2 text-xs text-red-600">{erro}</p>}

        {clientes !== null && (
          <>
            {/* Cards de resumo: clicáveis, aplicam o filtro na lista. */}
            <div className="mt-4 grid grid-cols-4 gap-2">
              {cards.map((c) => (
                <button
                  key={c.chave}
                  onClick={() => clicarCard(c.chave)}
                  className={`rounded-2xl border p-3 text-left transition-colors ${
                    filtro === c.chave
                      ? 'border-blue-400/70 bg-blue-500/10'
                      : 'border-white/60 dark:border-slate-600/60 hover:bg-slate-500/10'
                  }`}
                >
                  <p className="text-[10px] font-semibold uppercase tracking-wider text-slate-400">{c.rotulo}</p>
                  <p className={`text-2xl font-bold ${c.cor}`}>{c.valor}</p>
                </button>
              ))}
            </div>

            {/* Distribuição por status: barra empilhada em CSS puro. */}
            {resumo.total > 0 && (
              <div className="mt-4">
                <div className="flex justify-between text-[10px] font-semibold uppercase tracking-wider text-slate-400">
                  <span>Distribuição por status</span>
                  <span>
                    {pctAtivos}% ativos · {100 - pctAtivos}% inativos
                  </span>
                </div>
                <div className="mt-1 flex h-3 w-full overflow-hidden rounded-full bg-slate-300/40 dark:bg-slate-700/40">
                  <div className="bg-emerald-500" style={{ width: `${pctAtivos}%` }} />
                  <div className="bg-rose-500" style={{ width: `${100 - pctAtivos}%` }} />
                </div>
                <div className="mt-1 flex gap-4 text-xs text-slate-500 dark:text-slate-400">
                  <span>
                    <span className="inline-block h-2 w-2 rounded-full bg-emerald-500 mr-1" />
                    Ativos ({resumo.ativos})
                  </span>
                  <span>
                    <span className="inline-block h-2 w-2 rounded-full bg-rose-500 mr-1" />
                    Inativos ({resumo.inativos})
                  </span>
                </div>
              </div>
            )}

            {/* Ordenação da lista. */}
            <div className="mt-4 flex items-center gap-2 text-xs">
              <span className="text-slate-400">Ordenar por:</span>
              {(
                [
                  ['nome', 'Nome'],
                  ['situacao', 'Status'],
                  ['codigo', 'Código'],
                ] as [Ordem, string][]
              ).map(([k, rotulo]) => (
                <button
                  key={k}
                  className={`rounded px-2 py-0.5 ${ordem === k ? 'bg-blue-600 text-white' : 'border border-slate-300/60 dark:border-slate-600/60 hover:bg-slate-500/10'}`}
                  onClick={() => {
                    if (ordem === k) setOrdemAsc((v) => !v);
                    else {
                      setOrdem(k);
                      setOrdemAsc(true);
                    }
                  }}
                >
                  {rotulo}
                  {ordem === k ? (ordemAsc ? ' ↑' : ' ↓') : ''}
                </button>
              ))}
              {filtro !== 'todos' && (
                <button className="ml-auto text-blue-600 dark:text-blue-300 underline" onClick={() => setFiltro('todos')}>
                  limpar filtro: {filtro}
                </button>
              )}
            </div>

            {/* Lista interativa. */}
            {lista.length === 0 ? (
              <p className="mt-4 text-sm text-slate-500">
                {resumo.total === 0 ? 'Nenhum cliente carregado — use a busca acima.' : 'Nenhum cliente com esse filtro.'}
              </p>
            ) : (
              <ul className="mt-2 flex flex-col gap-1">
                {lista.map((r) => {
                  const ativo = situacaoAtiva(r);
                  const aberto = expandido === r.CodCliente;
                  return (
                    <li key={r.CodCliente} className="border rounded-lg">
                      <button
                        className="w-full text-left p-2 flex items-center justify-between gap-2 hover:bg-slate-500/10"
                        onClick={() => setExpandido(aberto ? null : r.CodCliente)}
                      >
                        <div className="min-w-0">
                          <p className="text-sm font-medium truncate dark:text-slate-100">{r.Nome}</p>
                          <p className="text-xs text-slate-500">
                            #{r.CodCliente} · {r.Cidade || '—'}
                            {r.CGCCPF ? ` · ${r.CGCCPF}` : ' · sem CPF/CNPJ'}
                          </p>
                        </div>
                        <div className="shrink-0 flex items-center gap-2">
                          <span
                            className={`rounded-full px-2 py-0.5 text-[10px] font-semibold ${
                              ativo ? 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-300' : 'bg-rose-500/15 text-rose-600 dark:text-rose-300'
                            }`}
                          >
                            {situacaoCurta(r)}
                          </span>
                          <span className="text-slate-400">{aberto ? '▾' : '▸'}</span>
                        </div>
                      </button>
                      {aberto && (
                        <div className="border-t px-3 py-2 text-xs text-slate-500 dark:text-slate-400 flex items-center justify-between gap-2">
                          <div className="min-w-0">
                            <p>CPF/CNPJ: {r.CGCCPF || '—'}</p>
                            <p>
                              Fone: {r.Fone1 || '—'} · Celular: {r.Celular || '—'}
                            </p>
                            <p className="truncate">
                              Endereço: {r.Endereco || '—'}
                              {r.NumEndereco ? `, ${r.NumEndereco}` : ''} · {r.Cidade || '—'}
                              {r.Estado ? `/${r.Estado}` : ''}
                            </p>
                          </div>
                          <button
                            className="shrink-0 border border-slate-300/60 dark:border-slate-600/60 rounded px-2 py-1 hover:bg-slate-500/10"
                            title="Abrir a visão 360 completa deste cliente"
                            onClick={() => App.CrmAbrirJanela(`/clientes?cod=${r.CodCliente}`).catch((e) => setErro(String(e)))}
                          >
                            👁 Visão 360
                          </button>
                        </div>
                      )}
                    </li>
                  );
                })}
              </ul>
            )}

            <p className="mt-4 text-[10px] text-slate-400">
              Dados vindos do 360 (mesma sessão do app). {resumo.total > 0 && `Mostrando ${lista.length} de ${resumo.total} clientes.`}
            </p>
          </>
        )}
      </div>
    </div>
  );
}
