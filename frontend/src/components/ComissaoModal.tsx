import { useEffect, useState } from 'react';
import * as bindings from '../../wailsjs/go/main/App';
import type { Comissao, Ticket } from '../wails-types';
import { formatCents, parseCents } from '../money';

// Percentuais do comissionamento da coluna Upgrade — espelha db.Comissao*Pct.
const pctServico = 5;
const pctParcela = 75;

interface Props {
  ticket: Ticket;
  /** Comissão já gravada no card (pré-preenche os campos). */
  comissao?: Comissao | null;
  onSalvo: (comissao: Comissao) => void;
  onFechar: () => void;
}

// ComissaoModal: comissionamento do card na coluna Upgrade — valor do serviço
// (5% de comissão) e valor da parcela do upgrade (75%), com observações e data
// de registro. Abre ao soltar o card na coluna, pelo menu ⋯ do card ou pelo
// selo 💰.
export default function ComissaoModal({ ticket, comissao, onSalvo, onFechar }: Props) {
  const [servico, setServico] = useState(
    comissao && comissao.valorServicoCents > 0 ? formatCents(comissao.valorServicoCents) : '',
  );
  const [parcela, setParcela] = useState(
    comissao && comissao.valorParcelaCents > 0 ? formatCents(comissao.valorParcelaCents) : '',
  );
  const [data, setData] = useState(comissao?.dataRegistro || new Date().toISOString().slice(0, 10));
  const [obs, setObs] = useState(comissao?.observacoes ?? '');
  const [erro, setErro] = useState('');
  const [salvando, setSalvando] = useState(false);

  // Sem comissão gravada, tenta puxar do backend (card pode ter sido
  // comissionado pelo assistente antes do modal abrir).
  useEffect(() => {
    if (comissao) return;
    let vivo = true;
    bindings
      .ListarComissoes()
      .then((cs: Comissao[]) => {
        const achou = cs.find((c) => c.ticketId === ticket.id);
        if (vivo && achou) {
          if (achou.valorServicoCents > 0) setServico((s) => s || formatCents(achou.valorServicoCents));
          if (achou.valorParcelaCents > 0) setParcela((p) => p || formatCents(achou.valorParcelaCents));
          if (achou.dataRegistro) setData(achou.dataRegistro);
          if (achou.observacoes) setObs((o) => o || achou.observacoes);
        }
      })
      .catch(() => {
        // sem registro: campos ficam vazios mesmo
      });
    return () => {
      vivo = false;
    };
  }, [comissao, ticket.id]);

  const centsServico = parseCents(servico);
  const centsParcela = parseCents(parcela);
  const comServico = centsServico !== null ? Math.floor((centsServico * pctServico) / 100) : null;
  const comParcela = centsParcela !== null ? Math.floor((centsParcela * pctParcela) / 100) : null;
  const total = comServico !== null && comParcela !== null ? comServico + comParcela : null;

  async function salvar() {
    if (centsServico === null || centsParcela === null) {
      setErro('Confira os valores — use números como 5.000,00');
      return;
    }
    setErro('');
    setSalvando(true);
    try {
      const c = await bindings.SalvarComissao(ticket.id, centsServico, centsParcela, obs.trim(), data);
      onSalvo(c);
    } catch (e) {
      setErro(String(e));
    } finally {
      setSalvando(false);
    }
  }

  const linhaResumo = 'flex items-center justify-between text-xs text-slate-600 dark:text-slate-300';

  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/30 backdrop-blur-sm" onClick={onFechar}>
      <div
        className="glass rounded-2xl w-[26rem] max-w-full p-4 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start justify-between">
          <div className="min-w-0">
            <h2 className="text-lg font-semibold dark:text-slate-100 flex items-center gap-2">
              💰 Comissão de upgrade
            </h2>
            <p className="text-xs text-slate-500 truncate">
              {ticket.nomeCliente}
              {ticket.codService ? ` · #${ticket.codService}` : ''}
            </p>
          </div>
          <button aria-label="Fechar" className="text-slate-400 hover:text-slate-600" onClick={onFechar}>
            ✕
          </button>
        </div>

        <div className="mt-3 flex flex-col gap-2.5">
          <div className="flex gap-2">
            <label className="flex flex-col gap-0.5 text-sm flex-1">
              <span className="text-slate-600 dark:text-slate-300">Valor do serviço ({pctServico}%)</span>
              <input
                inputMode="decimal"
                placeholder="0,00"
                className="border rounded px-2 py-1.5 bg-white/70 dark:bg-slate-800/70 text-slate-800 dark:text-slate-100"
                value={servico}
                onChange={(e) => setServico(e.target.value)}
              />
            </label>
            <label className="flex flex-col gap-0.5 text-sm flex-1">
              <span className="text-slate-600 dark:text-slate-300">Parcela do upgrade ({pctParcela}%)</span>
              <input
                inputMode="decimal"
                placeholder="0,00"
                className="border rounded px-2 py-1.5 bg-white/70 dark:bg-slate-800/70 text-slate-800 dark:text-slate-100"
                value={parcela}
                onChange={(e) => setParcela(e.target.value)}
              />
            </label>
          </div>

          <label className="flex flex-col gap-0.5 text-sm w-40">
            <span className="text-slate-600 dark:text-slate-300">Data de registro</span>
            <input
              type="date"
              className="border rounded px-2 py-1.5 bg-white/70 dark:bg-slate-800/70 text-slate-800 dark:text-slate-100"
              value={data}
              onChange={(e) => setData(e.target.value)}
            />
          </label>

          <label className="flex flex-col gap-0.5 text-sm">
            <span className="text-slate-600 dark:text-slate-300">Observações</span>
            <textarea
              rows={2}
              maxLength={250}
              className="border rounded px-2 py-1.5 bg-white/70 dark:bg-slate-800/70 text-slate-800 dark:text-slate-100 resize-none"
              placeholder="Ex.: upgrade de câmeras, parcela 3 de 6…"
              value={obs}
              onChange={(e) => setObs(e.target.value)}
            />
          </label>

          <div className="rounded-lg bg-slate-100/70 dark:bg-slate-700/40 px-3 py-2 space-y-0.5">
            <p className={linhaResumo}>
              <span>{pctServico}% do serviço</span>
              <span className="font-mono">{comServico !== null ? formatCents(comServico) : '—'}</span>
            </p>
            <p className={linhaResumo}>
              <span>{pctParcela}% da parcela</span>
              <span className="font-mono">{comParcela !== null ? formatCents(comParcela) : '—'}</span>
            </p>
            <p className="flex items-center justify-between text-sm font-semibold text-emerald-700 dark:text-emerald-300">
              <span>Comissão total</span>
              <span className="font-mono">{total !== null ? formatCents(total) : '—'}</span>
            </p>
          </div>

          {erro && <p className="text-xs text-red-600 break-words">{erro}</p>}

          <div className="flex gap-2 justify-end">
            <button
              className="text-sm text-slate-500 hover:text-slate-700 dark:hover:text-white px-3 py-1.5"
              onClick={onFechar}
            >
              Cancelar
            </button>
            <button
              className="bg-gradient-to-r from-emerald-500 to-teal-600 text-white rounded-lg px-4 py-1.5 text-sm font-medium shadow hover:brightness-110 transition disabled:opacity-60"
              disabled={salvando || total === null}
              title="Grava a comissão e move o card para a coluna Upgrade"
              onClick={salvar}
            >
              {salvando ? 'Salvando…' : 'Registrar comissão'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
