import { useEffect, useRef, useState } from 'react';
import * as App from '../../wailsjs/go/main/App';
import type { Avaliacao, Cobranca, Proposta, ReparcelamentoInput, ReparcelamentoResult, ResultadoCobranca, Ticket } from '../wails-types';
import { formatCents, parseCents } from '../money';
import OrcamentoModal from './OrcamentoModal';

const statusValidos = ['proposta', 'aceita', 'quebrada', 'cancelada'] as const;
// espelha ErrTotalInvalido do Go: total em aberto precisa ser positivo
const MIN_TOTAL_CENTS = 1;

interface Props {
  ticket: Ticket;
  /** Resultado do fluxo automático da coluna Cobrança (pré-preenche o modal). */
  preCobranca?: ResultadoCobranca | null;
  onFechouComSave: () => void;
  onFechar: () => void;
}

function CampoMoeda(props: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  onBlur: () => void;
  erro?: string;
}) {
  return (
    <label className="flex flex-col gap-0.5 text-sm">
      <span className="text-slate-600">{props.label}</span>
      <input
        className={`border rounded px-2 py-1 ${props.erro ? 'border-red-400' : ''}`}
        inputMode="decimal"
        value={props.value}
        onChange={(e) => props.onChange(e.target.value)}
        onBlur={props.onBlur}
      />
      {props.erro && <span className="text-xs text-red-600">{props.erro}</span>}
    </label>
  );
}

export default function NegotiationModal({ ticket, preCobranca, onFechouComSave, onFechar }: Props) {
  const [total, setTotal] = useState('');
  const [entrada, setEntrada] = useState('0');
  const [base, setBase] = useState('0');
  const [vezes, setVezes] = useState('12');
  const [status, setStatus] = useState<string>('proposta');
  const [erros, setErros] = useState<Record<string, string>>({});
  const [calc, setCalc] = useState<ReparcelamentoResult | null>(null);
  const [calcErro, setCalcErro] = useState('');
  const [salvando, setSalvando] = useState(false);
  const [saveErro, setSaveErro] = useState('');
  const [proposta, setProposta] = useState<Proposta | null>(null);
  const [temIA, setTemIA] = useState(false);
  const [historico, setHistorico] = useState<Cobranca[]>([]);
  const [avaliacao, setAvaliacao] = useState<Avaliacao | null>(null);
  const [suja, setSuja] = useState(false);
  const [orcAberto, setOrcAberto] = useState(false);
  const reqId = useRef(0);
  const debounce = useRef<ReturnType<typeof setTimeout>>();

  useEffect(() => {
    App.HasAI().then(setTemIA).catch(() => setTemIA(false));
    if (preCobranca) {
      // Fluxo automático da coluna Cobrança: chega com acordo salvo e proposta pronta.
      const c = preCobranca.cobranca;
      setStatus(c.status);
      setTotal(formatCents(c.totalAbertoCents));
      setEntrada(formatCents(c.entradaCents));
      setBase(formatCents(c.parcelaBaseCents));
      setVezes(String(c.numeroVezes));
      setProposta(preCobranca.proposta);
      setAvaliacao(preCobranca.avaliacao ?? null);
      if (preCobranca.aviso) setCalcErro(preCobranca.aviso);
      return;
    }
    App.GetCobrancaAtiva(ticket.id).then((c) => {
      if (!c) return;
      setStatus(c.status);
      setTotal(formatCents(c.totalAbertoCents));
      setEntrada(formatCents(c.entradaCents));
      setBase(formatCents(c.parcelaBaseCents));
      setVezes(String(c.numeroVezes));
    }).catch(() => {});
    App.GetHistoricoCobrancas(ticket.id).then(setHistorico).catch(() => {});
    App.GetAvaliacaoCliente(ticket.id).then(setAvaliacao).catch(() => {});
  }, [ticket.id, preCobranca]);

  useEffect(() => {
    const totalC = parseCents(total);
    const entradaC = parseCents(entrada) ?? 0;
    const baseC = parseCents(base) ?? 0;
    const n = parseInt(vezes, 10);
    const novos: Record<string, string> = {};
    if (totalC === null || totalC < MIN_TOTAL_CENTS) novos.total = 'Informe um valor maior que zero.';
    if (entradaC < 0 || (totalC !== null && entradaC > totalC)) novos.entrada = 'Entrada inválida.';
    if (baseC < 0) novos.base = 'Parcela base inválida.';
    if (!Number.isInteger(n) || n < 1 || n > 120) novos.vezes = 'Entre 1 e 120.';
    setErros(novos);
    if (Object.keys(novos).length > 0 || totalC === null) {
      setCalc(null);
      return;
    }
    const input: ReparcelamentoInput = {
      totalAbertoCents: totalC,
      entradaCents: entradaC,
      numeroVezes: n,
      parcelaBaseCents: baseC,
    };
    clearTimeout(debounce.current);
    debounce.current = setTimeout(() => {
      const id = ++reqId.current;
      App.CalculateDebt(input)
        .then((r) => {
          if (id === reqId.current) {
            setCalc(r);
            setCalcErro('');
          }
        })
        .catch((e) => {
          if (id === reqId.current) {
            setCalc(null);
            setCalcErro(String(e));
          }
        });
    }, 250);
    return () => clearTimeout(debounce.current);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [total, entrada, base, vezes]);

  const blurMoeda = (set: (v: string) => void) => (raw: string) => {
    const c = parseCents(raw);
    if (c !== null) {
      set(formatCents(c));
      setSuja(true);
    }
  };

  async function salvar() {
    const totalC = parseCents(total);
    if (totalC === null || totalC < MIN_TOTAL_CENTS) return;
    setSalvando(true);
    setSaveErro('');
    try {
      await App.SaveCobranca({
        id: 0,
        ticketId: ticket.id,
        dataContrato: new Date().toISOString().slice(0, 10),
        dataVencimento: '',
        totalAbertoCents: totalC,
        entradaCents: parseCents(entrada) ?? 0,
        numeroVezes: parseInt(vezes, 10),
        parcelaBaseCents: parseCents(base) ?? 0,
        parceladoCents: 0,
        ultimaParcelaCents: 0,
        finalCents: 0,
        status,
        ativa: true,
        versaoRegra: 1,
      });
      setSuja(false);
      onFechouComSave();
    } catch (e) {
      setSaveErro(String(e));
    } finally {
      setSalvando(false);
    }
  }

  async function gerar(polir: boolean) {
    const totalC = parseCents(total);
    if (totalC === null) return;
    try {
      const p = await App.GenerateProposal(
        ticket.id,
        {
          totalAbertoCents: totalC,
          entradaCents: parseCents(entrada) ?? 0,
          numeroVezes: parseInt(vezes, 10),
          parcelaBaseCents: parseCents(base) ?? 0,
        },
        polir,
      );
      setProposta(p);
    } catch (e) {
      setCalcErro(String(e));
    }
  }

  function tentarFechar() {
    if (suja && !window.confirm('Há alterações não salvas. Fechar mesmo assim?')) return;
    onFechar();
  }

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-black/30 backdrop-blur-sm" onClick={tentarFechar}>
      <aside
        className="glass w-[26rem] max-w-full h-full overflow-y-auto p-4 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start justify-between">
          <div>
            <h2 className="text-lg font-semibold dark:text-slate-100">{ticket.nomeCliente}</h2>
            <p className="text-xs text-slate-500">
              {ticket.tipoTicket}
              {ticket.numeroTicket ? ` · Ticket ${ticket.numeroTicket}` : ''}
              {ticket.telefone ? ` · ${ticket.telefone}` : ''}
              {ticket.endereco ? ` · ${ticket.endereco}` : ''}
            </p>
          </div>
          <button aria-label="Fechar" className="text-slate-400 hover:text-slate-600" onClick={tentarFechar}>
            ✕
          </button>
        </div>

        <div className="mt-4 flex flex-col gap-2">
          <CampoMoeda
            label="Valor total em aberto"
            value={total}
            onChange={(v) => { setTotal(v); setSuja(true); }}
            onBlur={() => blurMoeda(setTotal)(total)}
            erro={erros.total}
          />
          <CampoMoeda
            label="Entrada"
            value={entrada}
            onChange={(v) => { setEntrada(v); setSuja(true); }}
            onBlur={() => blurMoeda(setEntrada)(entrada)}
            erro={erros.entrada}
          />
          <CampoMoeda
            label="Parcela base (mensalidade atual)"
            value={base}
            onChange={(v) => { setBase(v); setSuja(true); }}
            onBlur={() => blurMoeda(setBase)(base)}
            erro={erros.base}
          />
          <label className="flex flex-col gap-0.5 text-sm">
            <span className="text-slate-600">Nº de vezes</span>
            <input
              className={`border rounded px-2 py-1 ${erros.vezes ? 'border-red-400' : ''}`}
              inputMode="numeric"
              value={vezes}
              onChange={(e) => { setVezes(e.target.value); setSuja(true); }}
            />
            {erros.vezes && <span className="text-xs text-red-600">{erros.vezes}</span>}
          </label>
          <label className="flex flex-col gap-0.5 text-sm">
            <span className="text-slate-600">Status do acordo</span>
            <select className="border rounded px-2 py-1" value={status} onChange={(e) => { setStatus(e.target.value); setSuja(true); }}>
              {statusValidos.map((s) => (
                <option key={s}>{s}</option>
              ))}
            </select>
          </label>
        </div>

        {calc && (
          <div className="mt-4 bg-slate-50 dark:bg-slate-700 rounded-lg p-3 text-sm grid grid-cols-2 gap-y-1">
            <span className="text-slate-500">Saldo devedor</span>
            <span className="text-right">{formatCents(calc.saldoCents)}</span>
            <span className="text-slate-500">Parcelado</span>
            <span className="text-right">{formatCents(calc.parceladoCents)}</span>
            <span className="text-slate-500">Nova parcela mensal</span>
            <span className="text-right font-medium">{formatCents(calc.finalCents)}</span>
            {calc.restoCents > 0 && (
              <>
                <span className="text-slate-500">Última parcela</span>
                <span className="text-right">{formatCents(calc.ultimaFinalCents)}</span>
              </>
            )}
          </div>
        )}
        {calcErro && <p className="mt-2 text-xs text-red-600">{calcErro}</p>}

        {avaliacao && (
          <div className="mt-3 bg-amber-50 dark:bg-amber-950 border border-amber-200 dark:border-amber-800 rounded-lg p-3 text-sm">
            <h3 className="text-sm font-semibold text-amber-800">Avaliação do cliente</h3>
            <ul className="mt-1 grid grid-cols-2 gap-y-1 text-xs">
              <span className="text-slate-500">Renegociação anterior</span>
              <span className="text-right">
                {avaliacao.renegociou ? `Sim (${avaliacao.qtdRenegociacoes})` : 'Não'}
              </span>
              <span className="text-slate-500">Tempo de casa</span>
              <span className="text-right">{avaliacao.mesesDeCasa > 0 ? `${avaliacao.mesesDeCasa} meses` : '—'}</span>
              <span className="text-slate-500">Fidelidade (12+ meses)</span>
              <span className="text-right">{avaliacao.fiel ? 'Sim' : 'Não'}</span>
              <span className="text-slate-500">Elegível a cortesia</span>
              <span className="text-right">{avaliacao.elegivelCortesia ? 'Sim' : 'Não'}</span>
              {avaliacao.mensalidadeCents > 0 && (
                <>
                  <span className="text-slate-500">Mensalidade (produtos)</span>
                  <span className="text-right">{formatCents(avaliacao.mensalidadeCents)}</span>
                </>
              )}
            </ul>
            {avaliacao.observacoes.length > 0 && (
              <ul className="mt-2 list-disc list-inside text-xs text-amber-800">
                {avaliacao.observacoes.map((o, i) => (
                  <li key={i}>{o}</li>
                ))}
              </ul>
            )}
          </div>
        )}

        <div className="mt-4 flex gap-2">
          <button
            className="flex-1 bg-blue-600 text-white rounded px-3 py-2 text-sm disabled:opacity-50"
            disabled={salvando || Object.keys(erros).length > 0 || !calc}
            onClick={salvar}
          >
            {salvando ? 'Salvando…' : 'Salvar acordo'}
          </button>
        </div>
        {saveErro && <p className="mt-1 text-xs text-red-600">{saveErro}</p>}

        <div className="mt-2 flex gap-2">
          <button className="flex-1 border rounded px-3 py-2 text-sm" onClick={() => gerar(false)}>
            Gerar proposta
          </button>
          {temIA && (
            <button className="flex-1 border rounded px-3 py-2 text-sm" onClick={() => gerar(true)}>
              Polir com IA
            </button>
          )}
          <button
            className="flex-1 border rounded px-3 py-2 text-sm"
            title="Monta o orçamento comercial pela planilha-calculadora"
            onClick={() => setOrcAberto(true)}
          >
            🧮 Orçamento
          </button>
        </div>

        {orcAberto && <OrcamentoModal ticket={ticket} onFechar={() => setOrcAberto(false)} />}

        {proposta && (
          <div className="mt-3">
            {proposta.aviso && <p className="mb-1 text-xs text-amber-700">{proposta.aviso}</p>}
            <textarea
              className="w-full border rounded p-2 text-sm h-48 dark:bg-slate-700 dark:border-slate-600 dark:text-slate-100"
              value={proposta.texto}
              onChange={(e) => setProposta({ ...proposta, texto: e.target.value })}
            />
            <button
              className="mt-1 text-sm border rounded px-3 py-1"
              onClick={() => navigator.clipboard.writeText(proposta.texto)}
            >
              Copiar
            </button>
          </div>
        )}

        {historico.length > 0 && (
          <div className="mt-4">
            <h3 className="text-sm font-semibold text-slate-600">Acordos anteriores</h3>
            <ul className="mt-1 text-xs text-slate-600 flex flex-col gap-1">
              {historico.map((c) => (
                <li key={c.id} className="flex justify-between border-b border-slate-100 pb-1">
                  <span>
                    {formatCents(c.totalAbertoCents)} em {c.numeroVezes}x
                  </span>
                  <span className="uppercase">{c.status}</span>
                </li>
              ))}
            </ul>
          </div>
        )}
      </aside>
    </div>
  );
}
