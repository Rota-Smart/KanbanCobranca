import { useEffect, useState } from 'react';
import * as bindings from '../../wailsjs/go/main/App';
import type { Ticket } from '../wails-types';

// Turnos do agendamento — espelha service.TurnosVisitaValidos (Go).
const turnos = ['Manhã', 'Tarde', 'Noite'] as const;

interface Props {
  ticket: Ticket;
  onSalvo: (ticket: Ticket) => void;
  onFechar: () => void;
}

// VisitaModal: agendamento de visita do card — data, turno, horário livre e
// o endereço (sugerido do cadastro do cliente no 360, editável), com link do
// Google Maps. Abre ao soltar o card na coluna "Visita Agendada" ou pelo
// menu ⋯ do card.
export default function VisitaModal({ ticket, onSalvo, onFechar }: Props) {
  const [data, setData] = useState(ticket.visitaData ?? '');
  const [turno, setTurno] = useState(ticket.visitaTurno ?? '');
  const [hora, setHora] = useState(ticket.visitaHora ?? '');
  const [endereco, setEndereco] = useState(ticket.visitaEndereco ?? '');
  const [carregando, setCarregando] = useState(!ticket.visitaEndereco);
  const [erro, setErro] = useState('');
  const [salvando, setSalvando] = useState(false);

  // Endereço sugerido: cadastro do cliente no 360 (cache local) ou o do card.
  useEffect(() => {
    if (ticket.visitaEndereco) return;
    let vivo = true;
    bindings
      .EnderecoSugeridoVisita(ticket.id)
      .then((sugestao: string) => {
        if (vivo && sugestao) setEndereco((atual) => atual || sugestao);
      })
      .catch(() => {
        // sem sugestão: o usuário digita
      })
      .finally(() => {
        if (vivo) setCarregando(false);
      });
    return () => {
      vivo = false;
    };
  }, [ticket.id, ticket.visitaEndereco]);

  // Mesmo link que o Go monta no AbrirNoMaps — só para mostrar/abrir no clique.
  const mapsHref = endereco.trim()
    ? `https://www.google.com/maps/search/?api=1&query=${encodeURIComponent(endereco.trim())}`
    : null;

  async function salvar() {
    setErro('');
    setSalvando(true);
    try {
      const t = await bindings.SalvarVisita(ticket.id, data, turno, hora, endereco.trim());
      onSalvo(t);
    } catch (e) {
      setErro(String(e));
    } finally {
      setSalvando(false);
    }
  }

  async function abrirMaps() {
    if (!mapsHref) return;
    try {
      await bindings.AbrirNoMaps(endereco.trim());
    } catch (e) {
      setErro(String(e));
    }
  }

  const dataCurta = data
    ? new Date(`${data}T12:00:00`).toLocaleDateString('pt-BR', { weekday: 'short', day: '2-digit', month: 'short' })
    : '';

  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/30 backdrop-blur-sm" onClick={onFechar}>
      <div
        className="glass rounded-2xl w-[26rem] max-w-full p-4 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start justify-between">
          <div className="min-w-0">
            <h2 className="text-lg font-semibold dark:text-slate-100 flex items-center gap-2">
              📅 Agendar visita
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
              <span className="text-slate-600 dark:text-slate-300">Dia</span>
              <input
                type="date"
                className="border rounded px-2 py-1.5 bg-white/70 dark:bg-slate-800/70 text-slate-800 dark:text-slate-100"
                value={data}
                onChange={(e) => setData(e.target.value)}
              />
              {dataCurta && <span className="text-[11px] text-slate-500">{dataCurta}</span>}
            </label>
            <label className="flex flex-col gap-0.5 text-sm w-28">
              <span className="text-slate-600 dark:text-slate-300">Turno</span>
              <select
                className="border rounded px-2 py-1.5 bg-white/70 dark:bg-slate-800/70 text-slate-800 dark:text-slate-100"
                value={turno}
                onChange={(e) => setTurno(e.target.value)}
              >
                <option value="">—</option>
                {turnos.map((t) => (
                  <option key={t} value={t}>{t}</option>
                ))}
              </select>
            </label>
            <label className="flex flex-col gap-0.5 text-sm w-28">
              <span className="text-slate-600 dark:text-slate-300">Horário</span>
              <input
                type="time"
                className="border rounded px-2 py-1.5 bg-white/70 dark:bg-slate-800/70 text-slate-800 dark:text-slate-100"
                value={hora}
                onChange={(e) => setHora(e.target.value)}
              />
            </label>
          </div>

          <label className="flex flex-col gap-0.5 text-sm">
            <span className="text-slate-600 dark:text-slate-300">
              Endereço da visita
              {!carregando && endereco && (
                <span className="ml-1 text-[11px] text-slate-400">(edite se precisar)</span>
              )}
            </span>
            <textarea
              rows={3}
              maxLength={250}
              className="border rounded px-2 py-1.5 bg-white/70 dark:bg-slate-800/70 text-slate-800 dark:text-slate-100 resize-none"
              placeholder={carregando ? 'Buscando o endereço do cadastro…' : 'Endereço do cadastro do cliente ou outro'}
              value={endereco}
              onChange={(e) => setEndereco(e.target.value)}
            />
          </label>

          {mapsHref && (
            <p className="text-xs text-slate-500 break-words">
              🗺️ <button className="text-sky-600 dark:text-sky-300 hover:underline" onClick={abrirMaps}>
                Abrir no Google Maps
              </button>
              <span className="block text-[11px] text-slate-400 mt-0.5">{mapsHref}</span>
            </p>
          )}

          {erro && <p className="text-xs text-red-600 break-words">{erro}</p>}

          <div className="flex gap-2 justify-end">
            <button
              className="text-sm text-slate-500 hover:text-slate-700 dark:hover:text-white px-3 py-1.5"
              onClick={onFechar}
            >
              Cancelar
            </button>
            <button
              className="bg-gradient-to-r from-sky-500 to-blue-600 text-white rounded-lg px-4 py-1.5 text-sm font-medium shadow hover:brightness-110 transition disabled:opacity-60"
              disabled={salvando || carregando || (!data && !turno && !hora && !endereco.trim() && !ticket.visitaData && !ticket.visitaTurno && !ticket.visitaHora && !ticket.visitaEndereco)}
              title="Grava o agendamento no card (tudo vazio limpa a visita)"
              onClick={salvar}
            >
              {salvando ? 'Salvando…' : 'Salvar visita'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
