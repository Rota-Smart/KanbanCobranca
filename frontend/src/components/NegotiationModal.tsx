import { useEffect, useRef, useState } from 'react';
import * as App from '../../wailsjs/go/main/App';
import type {
  Avaliacao,
  Cobranca,
  Financeiro360,
  Fatura360,
  Proposta,
  ReparcelamentoInput,
  ReparcelamentoResult,
  RenegociacaoPronta,
  ResultadoCobranca,
  Ticket,
} from '../wails-types';
import { formatCents, parseCents } from '../money';
import OrcamentoModal from './OrcamentoModal';

const statusValidos = ['proposta', 'aceita', 'quebrada', 'cancelada'] as const;
// espelha ErrTotalInvalido do Go: total em aberto precisa ser positivo
const MIN_TOTAL_CENTS = 1;

interface Props {
  ticket: Ticket;
  /** Card está na coluna Cobrança: libera o botão "Gerar renegociação". */
  podeGerar?: boolean;
  /** Resultado do fluxo automático da coluna Cobrança (pré-preenche o modal). */
  preCobranca?: ResultadoCobranca | null;
  onFechouComSave: () => void;
  onFechar: () => void;
}

// Seções do card completo: o nav troca o conteúdo, estilo coluna do Kanban.
type Aba = 'acordo' | 'proposta' | 'cliente' | 'financeiro' | 'historico';

const abas: { id: Aba; icone: string; rotulo: string }[] = [
  { id: 'acordo', icone: '📝', rotulo: 'Acordo' },
  { id: 'proposta', icone: '✉️', rotulo: 'Proposta' },
  { id: 'cliente', icone: '👤', rotulo: 'Cliente' },
  { id: 'financeiro', icone: '💰', rotulo: 'Financeiro' },
  { id: 'historico', icone: '🕘', rotulo: 'Histórico' },
];

function CampoMoeda(props: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  onBlur: () => void;
  erro?: string;
}) {
  return (
    <label className="flex flex-col gap-0.5 text-sm">
      <span className="text-slate-600 dark:text-slate-300">{props.label}</span>
      <input
        className={`border rounded px-2 py-1 bg-white/70 dark:bg-slate-800/70 text-slate-800 dark:text-slate-100 ${props.erro ? 'border-red-400' : ''}`}
        inputMode="decimal"
        value={props.value}
        onChange={(e) => props.onChange(e.target.value)}
        onBlur={props.onBlur}
      />
      {props.erro && <span className="text-xs text-red-600">{props.erro}</span>}
    </label>
  );
}

function LinhaDado(props: { rotulo: string; valor: React.ReactNode }) {
  if (props.valor === undefined || props.valor === null || props.valor === '') return null;
  return (
    <div className="flex justify-between gap-3 text-sm">
      <span className="text-slate-500 dark:text-slate-400 shrink-0">{props.rotulo}</span>
      <span className="text-right text-slate-800 dark:text-slate-100 break-words min-w-0">{props.valor}</span>
    </div>
  );
}

const brlReais = (v: number) =>
  v.toLocaleString('pt-BR', { style: 'currency', currency: 'BRL' });

const dataBR = (iso: string) => {
  if (!iso) return '—';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleDateString('pt-BR');
};

export default function NegotiationModal({ ticket, podeGerar, preCobranca, onFechouComSave, onFechar }: Props) {
  const [aba, setAba] = useState<Aba>('acordo');
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
  const [fin, setFin] = useState<Financeiro360 | null>(null);
  const [faturas, setFaturas] = useState<Fatura360[]>([]);
  const [condicoes, setCondicoes] = useState('');
  const [gerando, setGerando] = useState(false);
  const [gerarErro, setGerarErro] = useState('');
  const [reneg, setReneg] = useState<RenegociacaoPronta | null>(null);
  const [cortesiaQtd, setCortesiaQtd] = useState(1);
  const [cortesiaMeses, setCortesiaMeses] = useState(3);
  const reqId = useRef(0);
  const debounce = useRef<ReturnType<typeof setTimeout>>();

  useEffect(() => {
    App.HasAI().then(setTemIA).catch(() => setTemIA(false));
    App.ListarParametros()
      .then((ps) => setCondicoes(ps.map((p) => p.conteudo).join('\n\n').trim()))
      .catch(() => setCondicoes(''));
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
      setFin(preCobranca.financeiro ?? null);
      setFaturas(preCobranca.faturas ?? []);
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

  // gerarRenegociacao puxa os valores em aberto do 360 (Política v7 + padrão
  // da proposta ficam no backend) e preenche o acordo; o texto sai na aba Proposta.
  async function gerarRenegociacao() {
    setGerando(true);
    setGerarErro('');
    try {
      const r = await App.GerarRenegociacao(ticket.id);
      setTotal(formatCents(r.totalAbertoCents));
      setEntrada(formatCents(r.entradaCents));
      setBase(formatCents(r.parcelaBaseCents));
      setVezes(String(r.numeroVezes));
      setProposta(r.proposta);
      setAvaliacao(r.avaliacao ?? null);
      setFin(r.financeiro ?? null);
      setFaturas(r.faturas ?? []);
      setReneg(r);
      if (r.cortesiaRestantes > 0) setCortesiaQtd(1);
      setSuja(true);
      setAba('proposta');
    } catch (e) {
      setGerarErro(String(e));
    } finally {
      setGerando(false);
    }
  }

  // pagamentoAVista preenche o acordo como entrada única só dos valores já
  // vencidos (o mês corrente segue na mensalidade normal).
  async function pagamentoAVista() {
    if (!reneg || reneg.totalVencidoCents <= 0) return;
    const input: ReparcelamentoInput = {
      totalAbertoCents: reneg.totalVencidoCents,
      entradaCents: reneg.totalVencidoCents,
      numeroVezes: 1,
      parcelaBaseCents: parseCents(base) ?? reneg.parcelaBaseCents,
    };
    setTotal(formatCents(input.totalAbertoCents));
    setEntrada(formatCents(input.entradaCents));
    setVezes('1');
    setSuja(true);
    try {
      const p = await App.GenerateProposal(ticket.id, input, false);
      setProposta(p);
      setAba('proposta');
    } catch (e) {
      setGerarErro(String(e));
    }
  }

  async function gerarComVezes(n: number) {
    const totalC = parseCents(total);
    if (totalC === null) return;
    setVezes(String(n));
    setSuja(true);
    const input: ReparcelamentoInput = {
      totalAbertoCents: totalC,
      entradaCents: parseCents(entrada) ?? 0,
      numeroVezes: n,
      parcelaBaseCents: parseCents(base) ?? 0,
    };
    try {
      const p = await App.GenerateProposal(ticket.id, input, false);
      setProposta(p);
    } catch (e) {
      setCalcErro(String(e));
    }
  }

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
        className="glass w-[30rem] max-w-full h-full overflow-y-auto p-4 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start justify-between">
          <div className="min-w-0">
            <h2 className="text-lg font-semibold dark:text-slate-100 break-words">{ticket.nomeCliente}</h2>
            <p className="text-xs text-slate-500">
              {ticket.tipoTicket}
              {ticket.numeroTicket ? ` · Ticket ${ticket.numeroTicket}` : ''}
              {ticket.telefone ? ` · ${ticket.telefone}` : ''}
            </p>
          </div>
          <button aria-label="Fechar" className="text-slate-400 hover:text-slate-600" onClick={tentarFechar}>
            ✕
          </button>
        </div>

        {/* Nav de seções, estilo coluna do Kanban */}
        <nav className="mt-3 flex gap-1 overflow-x-auto -mx-1 px-1 pb-1" aria-label="Seções do cliente">
          {abas.map((a) => (
            <button
              key={a.id}
              aria-current={aba === a.id}
              className={`shrink-0 px-2.5 py-1.5 rounded-lg text-[13px] font-medium transition ${
                aba === a.id
                  ? 'bg-sky-500/20 text-sky-700 dark:text-sky-200'
                  : 'text-slate-500 hover:bg-slate-500/10 hover:text-slate-700 dark:hover:text-white'
              }`}
              onClick={() => setAba(a.id)}
            >
              <span aria-hidden className="mr-1">{a.icone}</span>
              {a.rotulo}
            </button>
          ))}
        </nav>

        {aba === 'acordo' && (
          <div className="mt-3 flex flex-col gap-2">
            {podeGerar && (
              <>
                <button
                  className="w-full rounded-lg px-3 py-2.5 text-sm font-semibold text-white bg-gradient-to-r from-emerald-500 to-teal-600 hover:from-emerald-400 hover:to-teal-500 disabled:opacity-60 shadow"
                  disabled={gerando}
                  title="Puxa o total em aberto do 360 e monta a proposta no padrão (condições da Política v7)"
                  onClick={gerarRenegociacao}
                >
                  {gerando ? '🔄 Puxando valores do 360…' : '🔄 Gerar renegociação'}
                </button>
                {gerarErro && <p className="text-xs text-red-600 break-words">{gerarErro}</p>}
                {reneg && reneg.valorMensalidadeCents > 0 && (
                  <p className="text-xs text-slate-600 dark:text-slate-300 bg-white/50 dark:bg-white/5 rounded-lg px-3 py-2">
                    📄 Contrato: <b>{formatCents(reneg.valorMensalidadeCents)}</b>/mês
                    {reneg.diaVencimento > 0 && <> · vence dia <b>{reneg.diaVencimento}</b></>}
                    {reneg.contratoAssinatura && <> · assinado em {reneg.contratoAssinatura}</>}
                    {reneg.contratoPrazoMeses > 0 && <> · prazo de {reneg.contratoPrazoMeses} meses</>}
                    {reneg.contratoTermino && <> · termina em {reneg.contratoTermino}</>}
                  </p>
                )}
              </>
            )}

            {reneg && reneg.totalVencidoCents > 0 && (
              <div className="flex flex-wrap gap-1.5 items-center">
                <button
                  className="px-2.5 py-1.5 rounded-lg text-[13px] font-medium text-white bg-gradient-to-r from-rose-500 to-red-600 hover:from-rose-400 hover:to-red-500 disabled:opacity-60"
                  title={`Entrada única quitando só as faturas vencidas (${formatCents(reneg.totalVencidoCents)}); a mensalidade do mês segue normal`}
                  onClick={pagamentoAVista}
                >
                  💵 À vista (só vencidas: {formatCents(reneg.totalVencidoCents)})
                </button>
                {[3, 6, 12].map((n) => (
                  <button
                    key={n}
                    className={`px-2.5 py-1.5 rounded-lg text-[13px] font-medium border ${
                      vezes === String(n)
                        ? 'bg-sky-500/20 text-sky-700 dark:text-sky-200 border-sky-400/50'
                        : 'text-slate-600 hover:bg-slate-500/10 dark:text-slate-300'
                    }`}
                    title={`Recalcula o parcelamento em ${n}x e regenera a proposta`}
                    onClick={() => gerarComVezes(n)}
                  >
                    {n}x
                  </button>
                ))}
              </div>
            )}

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
              <span className="text-slate-600 dark:text-slate-300">Nº de vezes</span>
              <input
                className={`border rounded px-2 py-1 bg-white/70 dark:bg-slate-800/70 text-slate-800 dark:text-slate-100 ${erros.vezes ? 'border-red-400' : ''}`}
                inputMode="numeric"
                value={vezes}
                onChange={(e) => { setVezes(e.target.value); setSuja(true); }}
              />
              {erros.vezes && <span className="text-xs text-red-600">{erros.vezes}</span>}
            </label>
            <label className="flex flex-col gap-0.5 text-sm">
              <span className="text-slate-600 dark:text-slate-300">Status do acordo</span>
              <select className="border rounded px-2 py-1 bg-white/70 dark:bg-slate-800/70 text-slate-800 dark:text-slate-100" value={status} onChange={(e) => { setStatus(e.target.value); setSuja(true); }}>
                {statusValidos.map((s) => (
                  <option key={s}>{s}</option>
                ))}
              </select>
            </label>

            {calc && (
              <div className="bg-slate-50 dark:bg-slate-700 rounded-lg p-3 text-sm grid grid-cols-2 gap-y-1">
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
            {calcErro && <p className="text-xs text-red-600">{calcErro}</p>}

            {reneg && (
              <div className={`rounded-lg p-3 text-sm border ${reneg.direitoCortesia ? 'bg-emerald-50 dark:bg-emerald-950/40 border-emerald-200 dark:border-emerald-800' : 'bg-slate-50 dark:bg-slate-700/50 border-slate-200 dark:border-slate-600'}`}>
                <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">
                  🎁 Cortesia {reneg.direitoCortesia ? `— ${reneg.cortesiaRestantes} disponíve${reneg.cortesiaRestantes > 1 ? 'is' : 'l'}` : '— sem direito'}
                </h3>
                <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">{reneg.cortesiaMotivo}</p>
                {reneg.direitoCortesia && reneg.valorMensalidadeCents > 0 && (
                  <>
                    <div className="mt-2 flex flex-wrap gap-2 items-end">
                      <label className="flex flex-col gap-0.5 text-xs">
                        <span className="text-slate-600 dark:text-slate-300">Mensalidades de cortesia</span>
                        <select
                          className="border rounded px-2 py-1 bg-white/70 dark:bg-slate-800/70 text-slate-800 dark:text-slate-100"
                          value={cortesiaQtd}
                          onChange={(e) => setCortesiaQtd(Number(e.target.value))}
                        >
                          {Array.from({ length: reneg.cortesiaRestantes }, (_, i) => i + 1).map((n) => (
                            <option key={n} value={n}>{n} mensalidade{n > 1 ? 's' : ''}</option>
                          ))}
                        </select>
                      </label>
                      <label className="flex flex-col gap-0.5 text-xs">
                        <span className="text-slate-600 dark:text-slate-300">Diluir em (meses)</span>
                        <select
                          className="border rounded px-2 py-1 bg-white/70 dark:bg-slate-800/70 text-slate-800 dark:text-slate-100"
                          value={cortesiaMeses}
                          onChange={(e) => setCortesiaMeses(Number(e.target.value))}
                        >
                          {Array.from({ length: 12 }, (_, i) => i + 1).map((n) => (
                            <option key={n} value={n}>{n}x</option>
                          ))}
                        </select>
                      </label>
                    </div>
                    <p className="mt-2 text-xs text-slate-700 dark:text-slate-200">
                      Cortesia total de <b>{formatCents(reneg.valorMensalidadeCents * cortesiaQtd)}</b> —
                      {' '}desconto de <b>{formatCents(Math.round((reneg.valorMensalidadeCents * cortesiaQtd) / cortesiaMeses))}/mês</b> durante {cortesiaMeses} {cortesiaMeses === 1 ? 'mês' : 'meses'}
                      {calc && calc.finalCents > 0 && (
                        <> · parcela durante: <b>{formatCents(Math.max(0, calc.finalCents - Math.round((reneg.valorMensalidadeCents * cortesiaQtd) / cortesiaMeses)))}</b></>
                      )}
                    </p>
                    <p className="mt-1 text-[11px] text-slate-500 dark:text-slate-400">
                      Política v7: cortesia exige renovação da vigência por no mínimo 12 meses. Informe o desconto ao cliente e registre no ticket.
                    </p>
                  </>
                )}
              </div>
            )}

            <button
              className="bg-blue-600 text-white rounded px-3 py-2 text-sm disabled:opacity-50"
              disabled={salvando || Object.keys(erros).length > 0 || !calc}
              onClick={salvar}
            >
              {salvando ? 'Salvando…' : 'Salvar acordo'}
            </button>
            {saveErro && <p className="text-xs text-red-600">{saveErro}</p>}

            <button
              className="border rounded px-3 py-2 text-sm text-slate-700 dark:text-slate-200"
              title="Monta o orçamento comercial pela planilha-calculadora"
              onClick={() => setOrcAberto(true)}
            >
              🧮 Orçamento comercial
            </button>
          </div>
        )}

        {aba === 'proposta' && (
          <div className="mt-3 flex flex-col gap-2">
            <div className="flex gap-2">
              <button className="flex-1 border rounded px-3 py-2 text-sm" onClick={() => gerar(false)}>
                Gerar proposta
              </button>
              {temIA && (
                <button className="flex-1 border rounded px-3 py-2 text-sm" onClick={() => gerar(true)}>
                  Polir com IA
                </button>
              )}
            </div>
            {proposta ? (
              <>
                {proposta.aviso && <p className="text-xs text-amber-700">{proposta.aviso}</p>}
                <textarea
                  className="w-full border rounded p-2 text-sm h-64 bg-white/70 dark:bg-slate-800/70 dark:border-slate-600 dark:text-slate-100"
                  value={proposta.texto}
                  onChange={(e) => setProposta({ ...proposta, texto: e.target.value })}
                />
                <button
                  className="self-start text-sm border rounded px-3 py-1"
                  onClick={() => navigator.clipboard.writeText(proposta.texto)}
                >
                  📋 Copiar proposta
                </button>
              </>
            ) : (
              <p className="text-sm text-slate-500 dark:text-slate-400">
                Gere a proposta pelos valores do acordo — ou use “Gerar renegociação” na aba Acordo para puxar os valores do 360.
              </p>
            )}
            {condicoes && (
              <details className="border rounded-lg px-3 py-2 text-sm bg-white/40 dark:bg-white/5">
                <summary className="cursor-pointer text-slate-600 dark:text-slate-300 font-medium">
                  📄 Condições da Política (PDF nas Configurações)
                </summary>
                <pre className="mt-2 whitespace-pre-wrap text-xs text-slate-600 dark:text-slate-300 max-h-56 overflow-y-auto">{condicoes}</pre>
              </details>
            )}
          </div>
        )}

        {aba === 'cliente' && (
          <div className="mt-3 flex flex-col gap-3">
            <div className="flex flex-col gap-1.5 bg-white/40 dark:bg-white/5 rounded-lg p-3">
              <LinhaDado rotulo="Código service" valor={ticket.codService && `#${ticket.codService}`} />
              <LinhaDado rotulo="Tipo" valor={ticket.tipoTicket} />
              <LinhaDado rotulo="Ticket" valor={ticket.numeroTicket} />
              <LinhaDado rotulo="Telefone" valor={ticket.telefone} />
              <LinhaDado rotulo="Endereço" valor={ticket.endereco} />
              <LinhaDado
                rotulo="Ticket 360"
                valor={ticket.ticket360Numero && `#${ticket.ticket360Numero}`}
              />
            </div>

            {reneg && (reneg.valorMensalidadeCents > 0 || reneg.contratoAssinatura) && (
              <div className="flex flex-col gap-1.5 bg-white/40 dark:bg-white/5 rounded-lg p-3">
                <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">📄 Contrato</h3>
                <LinhaDado rotulo="Mensalidade" valor={reneg.valorMensalidadeCents > 0 && formatCents(reneg.valorMensalidadeCents)} />
                <LinhaDado rotulo="Dia de vencimento" valor={reneg.diaVencimento > 0 && `dia ${reneg.diaVencimento}`} />
                <LinhaDado rotulo="Assinatura" valor={reneg.contratoAssinatura} />
                <LinhaDado rotulo="Prazo (vigência)" valor={reneg.contratoPrazoMeses > 0 && `${reneg.contratoPrazoMeses} meses`} />
                <LinhaDado rotulo="Término" valor={reneg.contratoTermino} />
                <LinhaDado rotulo="Cortesia" valor={reneg.cortesiaMotivo} />
              </div>
            )}

            {avaliacao && (
              <div className="bg-amber-50 dark:bg-amber-950 border border-amber-200 dark:border-amber-800 rounded-lg p-3 text-sm">
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
          </div>
        )}

        {aba === 'financeiro' && (
          <div className="mt-3 flex flex-col gap-3">
            {fin && (fin.TotalDevendo > 0 || fin.TotalDevendoComEncargos > 0 || fin.TotalAtrasadas > 0) ? (
              <>
                <div className="bg-slate-50 dark:bg-slate-700 rounded-lg p-3 text-sm grid grid-cols-2 gap-y-1">
                  <span className="text-slate-500">Total em aberto</span>
                  <span className="text-right font-medium">{brlReais(fin.TotalDevendo)}</span>
                  <span className="text-slate-500">Encargos de atraso</span>
                  <span className="text-right">{brlReais(fin.TotalEncargosAtraso)}</span>
                  <span className="text-slate-500">Total com encargos</span>
                  <span className="text-right font-semibold">{brlReais(fin.TotalDevendoComEncargos)}</span>
                  <span className="text-slate-500">Faturas atrasadas</span>
                  <span className="text-right">{fin.TotalAtrasadas}</span>
                </div>
                {faturas.length > 0 && (
                  <div className="overflow-x-auto">
                    <table className="w-full text-xs">
                      <thead>
                        <tr className="text-left text-slate-500 border-b border-slate-300/40">
                          <th className="py-1 pr-2 font-medium">Documento</th>
                          <th className="py-1 pr-2 font-medium">Vencimento</th>
                          <th className="py-1 pr-2 font-medium text-right">Valor</th>
                          <th className="py-1 font-medium text-right">Atraso</th>
                        </tr>
                      </thead>
                      <tbody>
                        {faturas.map((f, i) => (
                          <tr key={i} className="border-b border-slate-300/20 text-slate-700 dark:text-slate-200">
                            <td className="py-1 pr-2 font-mono">{f.NumeroDoc || '—'}</td>
                            <td className="py-1 pr-2">{dataBR(f.Vencimento)}</td>
                            <td className="py-1 pr-2 text-right">{brlReais(f.ValorDuplicata)}</td>
                            <td className={`py-1 text-right ${f.DiasAtraso > 0 ? 'text-red-600 dark:text-red-400' : ''}`}>
                              {f.DiasAtraso > 0 ? `${f.DiasAtraso}d` : '—'}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </>
            ) : (
              <p className="text-sm text-slate-500 dark:text-slate-400">
                Sem dados do 360 ainda.
                {podeGerar
                  ? ' Use “Gerar renegociação” na aba Acordo para puxar os valores em aberto.'
                  : ' Mova o card para a coluna Cobrança ou sincronize o cliente para atualizar.'}
              </p>
            )}
          </div>
        )}

        {aba === 'historico' && (
          <div className="mt-3">
            {historico.length > 0 ? (
              <ul className="text-xs text-slate-600 dark:text-slate-300 flex flex-col gap-1">
                {historico.map((c) => (
                  <li key={c.id} className="flex justify-between border-b border-slate-100 dark:border-slate-600/40 pb-1">
                    <span>
                      {formatCents(c.totalAbertoCents)} em {c.numeroVezes}x
                      {c.ativa && <span className="ml-1 text-emerald-600 dark:text-emerald-400">· ativa</span>}
                    </span>
                    <span className="uppercase">{c.status}</span>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-sm text-slate-500 dark:text-slate-400">Nenhum acordo registrado para este cliente.</p>
            )}
          </div>
        )}

        {orcAberto && <OrcamentoModal ticket={ticket} onFechar={() => setOrcAberto(false)} />}
      </aside>
    </div>
  );
}
