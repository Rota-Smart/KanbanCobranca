import { useEffect, useMemo, useState } from 'react';
import * as App from '../../wailsjs/go/main/App';
import type { FaixaMensalidade, Ticket, TabelaOrcamento, TipoSistema } from '../wails-types';

interface Props {
  ticket: Ticket;
  onFechar: () => void;
}

const brl = (v: number) =>
  v.toLocaleString('pt-BR', { style: 'currency', currency: 'BRL' });

// OrcamentoModal monta o orçamento comercial a partir da planilha-calculadora:
// escolhe o tipo de sistema, define quantidades por item e gera a proposta
// com taxa de instalação e mensalidade calculadas no backend.
export default function OrcamentoModal({ ticket, onFechar }: Props) {
  const [tabela, setTabela] = useState<TabelaOrcamento | null>(null);
  const [erro, setErro] = useState('');
  const [tipoIdx, setTipoIdx] = useState(0);
  const [qtds, setQtds] = useState<Record<string, number>>({});
  const [classe, setClasse] = useState<'' | 'service' | 'ampliacao'>('');
  const [gerando, setGerando] = useState(false);
  const [proposta, setProposta] = useState('');
  const [copiado, setCopiado] = useState(false);

  useEffect(() => {
    App.OrcamentoTabela()
      .then((t) => setTabela(t))
      .catch((e) => setErro(String(e)));
  }, []);

  const tipo: TipoSistema | null = tabela?.tipos[tipoIdx] ?? null;

  // totais ao vivo no frontend; o valor oficial é recalculado no Go ao gerar
  const totais = useMemo(() => {
    let taxa = tipo?.taxaInstalacao ?? 0;
    let mensal = 0;
    for (const sec of tipo?.secoes ?? []) {
      for (const it of sec.itens) {
        const q = qtds[it.nome] ?? 0;
        if (q <= 0) continue;
        if (it.taxa) taxa += it.taxa * q;
        if (it.mensal) mensal += it.mensal * q;
      }
    }
    return { taxa, mensal };
  }, [tipo, qtds]);

  function qtd(nome: string, delta: number) {
    setQtds((q) => {
      const novo = Math.max(0, (q[nome] ?? 0) + delta);
      return { ...q, [nome]: novo };
    });
    setProposta('');
  }

  async function gerar() {
    if (!tipo) return;
    const itens: Record<string, number> = {};
    for (const [nome, q] of Object.entries(qtds)) {
      if (q > 0) itens[nome] = q;
    }
    if (Object.keys(itens).length === 0) {
      setErro('Escolha pelo menos um item.');
      return;
    }
    setGerando(true);
    setErro('');
    try {
      setProposta(await App.GerarOrcamento(ticket.id, { tipoSistema: tipo.nome, itens, classificacao: classe }));
      setCopiado(false);
    } catch (e) {
      setErro(String(e));
    } finally {
      setGerando(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm" onClick={onFechar}>
      <div
        className="glass rounded-2xl w-[40rem] max-w-full max-h-[90vh] overflow-y-auto p-5 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start justify-between">
          <div>
            <h2 className="font-semibold text-lg dark:text-slate-100">Gerar orçamento</h2>
            <p className="text-xs text-slate-500">{ticket.nomeCliente} · planilha-calculadora comercial</p>
          </div>
          <button aria-label="Fechar" className="text-slate-400 hover:text-slate-600" onClick={onFechar}>
            ✕
          </button>
        </div>

        {erro && <p className="mt-3 text-sm text-red-600">{erro}</p>}

        {!tabela && !erro && <p className="mt-4 text-sm text-slate-500">Carregando planilha…</p>}

        {tabela && (
          <>
            <div className="mt-3 flex flex-wrap gap-1">
              {tabela.tipos.map((t, i) => (
                <button
                  key={t.nome}
                  className={`text-xs rounded-lg px-3 py-1.5 border transition font-medium ${
                    i === tipoIdx
                      ? 'bg-blue-600 text-white border-blue-600'
                      : 'bg-white/40 dark:bg-white/10 border-white/50 dark:border-white/10'
                  }`}
                  onClick={() => {
                    setTipoIdx(i);
                    setQtds({});
                    setProposta('');
                    setErro('');
                  }}
                >
                  {t.nome}
                </button>
              ))}
            </div>

            {tipo?.secoes.map((sec) => (
              <div key={sec.titulo} className="mt-3 border rounded-lg p-3">
                <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">{sec.titulo}</h3>
                <ul className="mt-1 flex flex-col gap-1">
                  {sec.itens.map((it) => (
                    <li key={it.nome} className="flex items-center justify-between gap-2 text-sm">
                      <span className="min-w-0 flex-1 truncate" title={it.nome}>
                        {it.nome}
                      </span>
                      <span className="text-xs text-slate-500 whitespace-nowrap">
                        {[it.taxa ? `inst. ${brl(it.taxa)}` : '', it.mensal ? `${brl(it.mensal)}/mês` : '']
                          .filter(Boolean)
                          .join(' · ')}
                      </span>
                      <span className="flex items-center gap-1">
                        <button
                          aria-label={`Menos ${it.nome}`}
                          className="w-6 h-6 rounded border text-sm leading-none"
                          onClick={() => qtd(it.nome, -1)}
                        >
                          −
                        </button>
                        <span className="w-6 text-center tabular-nums">{qtds[it.nome] ?? 0}</span>
                        <button
                          aria-label={`Mais ${it.nome}`}
                          className="w-6 h-6 rounded border text-sm leading-none"
                          onClick={() => qtd(it.nome, +1)}
                        >
                          +
                        </button>
                      </span>
                    </li>
                  ))}
                </ul>
              </div>
            ))}

            {tipo && (
              <div className="mt-3 bg-slate-50 dark:bg-slate-700 rounded-lg p-3 text-sm grid grid-cols-2 gap-y-1">
                <span className="text-slate-500">Taxa de instalação</span>
                <span className="text-right">{brl(totais.taxa)}</span>
                <span className="text-slate-500">Mensalidade</span>
                <span className="text-right font-medium">{brl(totais.mensal)}/mês</span>
                {tipo.faixasMensal.map((f: FaixaMensalidade) => (
                  <span key={f.rotulo} className="col-span-2 text-xs text-slate-400">
                    {f.rotulo}: {brl(f.valor)}/mês
                  </span>
                ))}
              </div>
            )}

            <div className="mt-3 flex items-center gap-2 text-sm">
              <span className="text-xs text-slate-500">Marcar como:</span>
              {(
                [
                  ['', 'Automático'],
                  ['service', 'Orçamento'],
                  ['ampliacao', 'Ampliação (RD)'],
                ] as const
              ).map(([v, rotulo]) => (
                <button
                  key={v}
                  className={`text-xs rounded-lg px-2.5 py-1 border transition font-medium ${
                    classe === v
                      ? 'bg-blue-600 text-white border-blue-600'
                      : 'bg-white/40 dark:bg-white/10 border-white/50 dark:border-white/10'
                  }`}
                  onClick={() => {
                    setClasse(v);
                    setProposta('');
                  }}
                >
                  {rotulo}
                </button>
              ))}
            </div>

            <div className="mt-3 flex gap-2">
              <button
                className="flex-1 bg-blue-600 text-white rounded px-3 py-2 text-sm disabled:opacity-50"
                disabled={gerando}
                onClick={gerar}
              >
                {gerando ? 'Calculando…' : 'Gerar orçamento'}
              </button>
            </div>

            {proposta && (
              <div className="mt-3">
                <textarea
                  className="w-full border rounded p-2 text-sm h-52 dark:bg-slate-700 dark:border-slate-600 dark:text-slate-100"
                  value={proposta}
                  onChange={(e) => setProposta(e.target.value)}
                />
                <button
                  className="mt-1 text-sm border rounded px-3 py-1"
                  onClick={() => {
                    navigator.clipboard.writeText(proposta);
                    setCopiado(true);
                  }}
                >
                  {copiado ? 'Copiado ✓' : 'Copiar'}
                </button>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
