import { useCallback, useEffect, useMemo, useState } from 'react';
import { CrmAbrirJanela, CrmFunis, CrmMover, CrmNegociacoes } from '../../wailsjs/go/main/App';

export interface FunilCrm {
  id: string;
  nome: string;
  chave?: string;
  area?: string;
  situacao?: string;
  ordem?: number;
}
export interface EtapaCrm {
  id: string;
  funil_id: string;
  nome: string;
  ordem: number;
  tipo?: string; // "ganho", "perda" ou ""
  prazo_dias?: number;
}
export interface NegociacaoCrm {
  id: string;
  funil_id: string;
  etapa_id: string;
  etapa_nome: string;
  etapa_ordem: number;
  titulo: string;
  cliente_nome: string;
  unidade_nome?: string;
  responsavel_nome?: string;
  status: string;
  saude?: string;
  valor_mensal?: number | null;
  valor_pontual?: number | null;
  dias_na_etapa?: number;
  ticket_numero?: string | null;
  exige_acao?: { motivos: string[] } | null;
  esfriando?: boolean;
}

const STATUS = ['aberta', 'pausada', 'ganha', 'perdida', 'todas'] as const;
const rotuloStatus: Record<string, string> = {
  aberta: 'Em andamento',
  pausada: 'Pausadas',
  ganha: 'Ganhas',
  perdida: 'Perdidas',
  todas: 'Todas',
};

function dinheiro(v?: number | null): string {
  if (v == null) return '';
  return v.toLocaleString('pt-BR', { style: 'currency', currency: 'BRL' });
}

// FunilEspelho: espelho do funil de relacionamento do CRM 360, nativo no app.
// Arrastar um card para outra coluna muda a etapa no CRM (com confirmação —
// movimento manual, nunca automático). Clicar no card abre a negociação na
// janela nativa do 360.
export default function FunilEspelho(props: { onFechar: () => void }) {
  const [funis, setFunis] = useState<FunilCrm[]>([]);
  const [etapas, setEtapas] = useState<EtapaCrm[]>([]);
  const [funilID, setFunilID] = useState('');
  const [status, setStatus] = useState<(typeof STATUS)[number]>('aberta');
  const [negociacoes, setNegociacoes] = useState<NegociacaoCrm[]>([]);
  const [carregando, setCarregando] = useState(true);
  const [erro, setErro] = useState('');
  const [avisos, setAvisos] = useState('');
  const [movendo, setMovendo] = useState('');

  const carregar = useCallback(() => {
    setCarregando(true);
    setErro('');
    CrmFunis()
      .then((cfg) => {
        const fs = cfg?.funis || [];
        const ativos = fs.filter((f) => f.situacao !== 'arquivado');
        setFunis(ativos);
        setEtapas(cfg?.etapas || []);
        setFunilID((atual) => {
          if (atual && ativos.some((f) => f.id === atual)) return atual;
          const rel = ativos.find((f) => /relacionamento/i.test(f.nome) || f.chave === 'relacionamento');
          return rel?.id ?? ativos[0]?.id ?? '';
        });
      })
      .catch((e) => {
        setErro(String(e));
        setCarregando(false);
      });
  }, []);

  useEffect(carregar, [carregar]);

  useEffect(() => {
    if (!funilID) return;
    setCarregando(true);
    setNegociacoes([]);
    CrmNegociacoes(funilID, status)
      .then((p) => setNegociacoes(p?.itens || []))
      .catch((e) => setErro(String(e)))
      .finally(() => setCarregando(false));
  }, [funilID, status]);

  const etapasFunil = useMemo(
    () => etapas.filter((e) => e.funil_id === funilID).sort((a, b) => a.ordem - b.ordem),
    [etapas, funilID],
  );

  const porEtapa = useMemo(() => {
    const mapa = new Map<string, NegociacaoCrm[]>();
    for (const n of negociacoes) {
      const lista = mapa.get(n.etapa_id) || [];
      lista.push(n);
      mapa.set(n.etapa_id, lista);
    }
    return mapa;
  }, [negociacoes]);

  async function mover(id: string, etapaID: string, etapaNome: string, titulo: string) {
    if (!confirm(`Mover "${titulo}" para "${etapaNome}" no CRM?\n\nA mudança vale no painel do 360 para todo mundo.`)) {
      return;
    }
    setMovendo(id);
    setAvisos('');
    try {
      await CrmMover(id, etapaID);
      setNegociacoes((lista) => lista.map((n) => (n.id === id ? { ...n, etapa_id: etapaID, etapa_nome: etapaNome } : n)));
    } catch (e) {
      setAvisos(`Falha ao mover: ${String(e)}`);
    } finally {
      setMovendo('');
    }
  }

  return (
    <div className="fixed inset-0 z-50 bg-black/30 backdrop-blur-sm flex items-stretch justify-center p-3" onClick={props.onFechar}>
      <div
        className="painel-modal rounded-3xl flex-1 max-w-full flex flex-col overflow-hidden shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        {/* cabeçalho: título, seletores, resumo */}
        <div className="flex flex-wrap items-center gap-2 p-4 pb-2 border-b border-white/20 dark:border-white/10">
          <h2 className="font-semibold text-lg dark:text-slate-100 mr-2">🧲 Funil do CRM</h2>
          <select
            className="rounded-lg border border-slate-300/60 dark:border-slate-600/60 bg-white/80 dark:bg-slate-800/80 px-2 py-1 text-sm"
            value={funilID}
            onChange={(e) => setFunilID(e.target.value)}
            aria-label="Funil"
          >
            {funis.map((f) => (
              <option key={f.id} value={f.id}>
                {f.nome}
              </option>
            ))}
          </select>
          <select
            className="rounded-lg border border-slate-300/60 dark:border-slate-600/60 bg-white/80 dark:bg-slate-800/80 px-2 py-1 text-sm"
            value={status}
            onChange={(e) => setStatus(e.target.value as (typeof STATUS)[number])}
            aria-label="Status"
          >
            {STATUS.map((s) => (
              <option key={s} value={s}>
                {rotuloStatus[s]}
              </option>
            ))}
          </select>
          <span className="text-xs text-slate-500 dark:text-slate-400">
            {negociacoes.length} {negociacoes.length === 1 ? 'negociação' : 'negociações'}
            {' · '}
            {dinheiro(negociacoes.reduce((s, n) => s + (n.valor_mensal ?? 0), 0))}
          </span>
          <div className="flex-1" />
          <button
            className="rounded-lg px-2 py-1 text-sm hover:bg-slate-500/10"
            title="Abrir o funil completo na janela do 360"
            onClick={() => CrmAbrirJanela('/crm/funil').catch((e) => setAvisos(String(e)))}
          >
            🗔 Abrir no 360
          </button>
          <button className="rounded-lg px-2 py-1 text-sm hover:bg-slate-500/10" title="Recarregar" onClick={carregar}>
            ⟳
          </button>
          <button aria-label="Fechar" className="text-slate-400 hover:text-slate-600 px-2" onClick={props.onFechar}>
            ✕
          </button>
        </div>
        {erro && <p className="px-4 py-1 text-xs text-red-600 dark:text-red-400 break-words">{erro}</p>}
        {avisos && <p className="px-4 py-1 text-xs text-amber-600 dark:text-amber-400 break-words">{avisos}</p>}

        {/* kanban do funil */}
        <div className="flex-1 overflow-x-auto overflow-y-hidden p-3 flex gap-2 items-stretch">
          {carregando && negociacoes.length === 0 && <p className="p-4 text-sm text-slate-500">Carregando funil…</p>}
          {!carregando && funilID && etapasFunil.length === 0 && (
            <p className="p-4 text-sm text-slate-500">Nenhuma etapa neste funil.</p>
          )}
          {etapasFunil.map((et) => {
            const lista = porEtapa.get(et.id) || [];
            return (
              <div
                key={et.id}
                className="rounded-2xl bg-white/40 dark:bg-white/5 border border-white/30 dark:border-white/10 w-64 shrink-0 flex flex-col"
                onDragOver={(e) => e.preventDefault()}
                onDrop={(e) => {
                  e.preventDefault();
                  const id = e.dataTransfer.getData('text/negociacao');
                  const titulo = e.dataTransfer.getData('text/titulo');
                  if (id && id !== '') mover(id, et.id, et.nome, titulo);
                }}
              >
                <div className="px-3 py-2 flex items-baseline gap-2">
                  <h3 className="font-semibold text-sm">{et.nome}</h3>
                  <span className="text-[10px] text-slate-500">{lista.length}</span>
                </div>
                <div className="flex-1 overflow-y-auto px-2 pb-2 space-y-2">
                  {lista.length === 0 && <p className="text-[11px] text-slate-400 px-1">—</p>}
                  {lista.map((n) => (
                    <div
                      key={n.id}
                      draggable
                      onDragStart={(e) => {
                        e.dataTransfer.setData('text/negociacao', n.id);
                        e.dataTransfer.setData('text/titulo', n.titulo || n.cliente_nome);
                        e.dataTransfer.effectAllowed = 'move';
                      }}
                      className={`rounded-xl bg-white dark:bg-slate-800/90 border p-2 text-xs cursor-grab active:cursor-grabbing shadow-sm ${
                        n.saude === 'vermelho'
                          ? 'border-red-300 dark:border-red-500/50'
                          : n.saude === 'amarelo' || n.esfriando
                            ? 'border-amber-300 dark:border-amber-500/50'
                            : 'border-slate-200/80 dark:border-slate-600/60'
                      } ${movendo === n.id ? 'opacity-50' : ''}`}
                      title="Arraste para outra etapa · clique para abrir no 360"
                      onClick={() => CrmAbrirJanela(`/crm/negociacoes/${n.id}`).catch((e) => setAvisos(String(e)))}
                    >
                      <div className="flex items-start justify-between gap-1">
                        <p className="font-medium leading-tight">{n.titulo || n.cliente_nome}</p>
                        {n.exige_acao?.motivos?.length ? (
                          <span
                            className="shrink-0 rounded-full bg-amber-400/80 text-white text-[9px] font-bold px-1.5 py-0.5"
                            title={`Exige ação: ${n.exige_acao.motivos.join(', ')}`}
                          >
                            ação
                          </span>
                        ) : null}
                      </div>
                      <p className="text-slate-500 dark:text-slate-400 mt-0.5 truncate">{n.cliente_nome}</p>
                      <div className="flex items-center justify-between mt-1 text-[10px] text-slate-400">
                        <span>
                          {n.unidade_nome}
                          {n.dias_na_etapa != null && n.dias_na_etapa > 0 ? ` · ${n.dias_na_etapa}d na etapa` : ''}
                        </span>
                        <span>{dinheiro(n.valor_mensal)}</span>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
