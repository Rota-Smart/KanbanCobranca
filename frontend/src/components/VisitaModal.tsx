import { useEffect, useRef, useState } from 'react';
import * as bindings from '../../wailsjs/go/main/App';
import type { Ticket } from '../wails-types';

// Turnos do agendamento — espelha service.TurnosVisitaValidos (Go).
const turnos = ['Manhã', 'Tarde', 'Noite'] as const;

const MESES = [
  'Janeiro', 'Fevereiro', 'Março', 'Abril', 'Maio', 'Junho',
  'Julho', 'Agosto', 'Setembro', 'Outubro', 'Novembro', 'Dezembro',
];
const DIAS_SEMANA = ['D', 'S', 'T', 'Q', 'Q', 'S', 'S'];

// Data local como ISO (yyyy-mm-dd) sem depender de fuso do toISOString.
function paraISO(d: Date): string {
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const dia = String(d.getDate()).padStart(2, '0');
  return `${d.getFullYear()}-${m}-${dia}`;
}
function deISO(s: string): Date {
  const [a, m, d] = s.split('-').map(Number);
  return new Date(a, m - 1, d);
}
function hojeISO(): string {
  return paraISO(new Date());
}
function diasAteISO(n: number): string {
  const d = new Date();
  d.setDate(d.getDate() + n);
  return paraISO(d);
}

interface CalendarioProps {
  selecionada: string; // ISO ou ''
  aoEscolher: (iso: string) => void;
  onFechar: () => void;
}

// Calendário no tema do app (azul/índigo + vidro), abre ao clicar no campo Dia.
function Calendario({ selecionada, aoEscolher, onFechar }: CalendarioProps) {
  const base = selecionada ? deISO(selecionada) : new Date();
  const [ano, setAno] = useState(base.getFullYear());
  const [mes, setMes] = useState(base.getMonth());
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function fora(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) onFechar();
    }
    document.addEventListener('mousedown', fora);
    return () => document.removeEventListener('mousedown', fora);
  }, [onFechar]);

  const primeiroDia = new Date(ano, mes, 1).getDay();
  const totalDias = new Date(ano, mes + 1, 0).getDate();
  const celulas: (number | null)[] = [
    ...Array(primeiroDia).fill(null),
    ...Array.from({ length: totalDias }, (_, i) => i + 1),
  ];
  while (celulas.length % 7 !== 0) celulas.push(null);

  const hoje = hojeISO();
  const anterior = () => (mes === 0 ? (setAno(ano - 1), setMes(11)) : setMes(mes - 1));
  const proximo = () => (mes === 11 ? (setAno(ano + 1), setMes(0)) : setMes(mes + 1));

  return (
    <div
      ref={ref}
      className="absolute z-50 mt-2 left-0 w-72 p-3 painel-modal rounded-2xl shadow-xl"
      onClick={(e) => e.stopPropagation()}
    >
      <div className="flex items-center justify-between mb-2">
        <button
          aria-label="Mês anterior"
          className="w-8 h-8 rounded-lg text-slate-300 hover:bg-white/10 hover:text-white transition"
          onClick={anterior}
        >
          ‹
        </button>
        <span className="text-sm font-semibold text-slate-100">
          {MESES[mes]} <span className="text-slate-400 font-normal">{ano}</span>
        </span>
        <button
          aria-label="Próximo mês"
          className="w-8 h-8 rounded-lg text-slate-300 hover:bg-white/10 hover:text-white transition"
          onClick={proximo}
        >
          ›
        </button>
      </div>

      <div className="grid grid-cols-7 gap-1 text-center text-[11px] font-medium text-sky-300/80 mb-1">
        {DIAS_SEMANA.map((d, i) => (
          <span key={i}>{d}</span>
        ))}
      </div>

      <div className="grid grid-cols-7 gap-1">
        {celulas.map((dia, i) => {
          if (dia === null) return <span key={i} />;
          const iso = paraISO(new Date(ano, mes, dia));
          const isSelecionado = iso === selecionada;
          const isHoje = iso === hoje;
          return (
            <button
              key={i}
              onClick={() => aoEscolher(iso)}
              className={[
                'h-8 rounded-lg text-sm transition',
                isSelecionado
                  ? 'bg-gradient-to-r from-sky-500 to-blue-600 text-white font-semibold shadow'
                  : 'text-slate-200 hover:bg-white/10',
                !isSelecionado && isHoje ? 'ring-1 ring-sky-400/70 font-semibold' : '',
              ].join(' ')}
            >
              {dia}
            </button>
          );
        })}
      </div>

      <div className="flex gap-1.5 mt-2.5">
        <button
          className="flex-1 text-xs py-1.5 rounded-lg bg-white/10 text-slate-200 hover:bg-white/20 transition"
          onClick={() => {
            aoEscolher(hoje);
            onFechar();
          }}
        >
          Hoje
        </button>
        <button
          className="flex-1 text-xs py-1.5 rounded-lg bg-white/10 text-slate-200 hover:bg-white/20 transition"
          onClick={() => {
            aoEscolher(diasAteISO(1));
            onFechar();
          }}
        >
          Amanhã
        </button>
        <button
          className="flex-1 text-xs py-1.5 rounded-lg bg-white/10 text-slate-300 hover:bg-white/20 transition"
          onClick={() => {
            aoEscolher('');
            onFechar();
          }}
        >
          Limpar
        </button>
      </div>
    </div>
  );
}

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
  const [calendarioAberto, setCalendarioAberto] = useState(false);

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

  async function cancelar() {
    setErro('');
    setSalvando(true);
    try {
      const t = await bindings.CancelarVisita(ticket.id);
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

  const dataExtenso = data
    ? deISO(data).toLocaleDateString('pt-BR', { weekday: 'long', day: '2-digit', month: 'long' })
    : '';

  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/30 backdrop-blur-sm" onClick={onFechar}>
      <div
        className="painel-modal rounded-3xl w-[26rem] max-w-full p-4 shadow-xl"
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
            <label className="flex flex-col gap-0.5 text-sm flex-1 relative">
              <span className="text-slate-600 dark:text-slate-300">Dia</span>
              <button
                type="button"
                className={`text-left border rounded px-2 py-1.5 bg-white/70 dark:bg-slate-800/70 text-slate-800 dark:text-slate-100 hover:border-sky-400 transition ${data ? '' : 'text-slate-400 dark:text-slate-500'}`}
                onClick={() => setCalendarioAberto((v) => !v)}
              >
                {dataExtenso || 'Escolher data…'}
              </button>
              {dataExtenso && (
                <span className="text-[11px] text-slate-500 mt-0.5">📅 {dataExtenso}</span>
              )}
              {calendarioAberto && (
                <Calendario
                  selecionada={data}
                  aoEscolher={setData}
                  onFechar={() => setCalendarioAberto(false)}
                />
              )}
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

          <div className="flex gap-1.5">
            <button
              type="button"
              className="flex-1 text-xs py-1.5 rounded-lg bg-gradient-to-r from-sky-500 to-blue-600 text-white font-medium shadow hover:brightness-110 transition"
              onClick={() => setData(hojeISO())}
            >
              Hoje
            </button>
            <button
              type="button"
              className="flex-1 text-xs py-1.5 rounded-lg bg-sky-100 dark:bg-sky-500/15 text-sky-700 dark:text-sky-200 font-medium border border-sky-200 dark:border-sky-400/30 hover:bg-sky-200 dark:hover:bg-sky-500/25 transition"
              onClick={() => setData(diasAteISO(1))}
            >
              Amanhã
            </button>
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
            <p className="text-xs text-slate-400 break-words">
              🗺️ <button className="text-sky-600 dark:text-sky-300 hover:underline" onClick={abrirMaps}>
                Abrir no Google Maps
              </button>
              <span className="block text-[11px] text-slate-400 mt-0.5">{mapsHref}</span>
            </p>
          )}

          {erro && <p className="text-xs text-red-600 break-words">{erro}</p>}

          <div className="flex gap-2 justify-end items-center">
            {ticket.visitaData && (
              <button
                className="text-sm text-red-600 dark:text-red-400 hover:underline px-3 py-1.5 mr-auto disabled:opacity-60"
                disabled={salvando}
                title="Apaga o agendamento deste card (some do site e do app)"
                onClick={cancelar}
              >
                Cancelar visita agendada
              </button>
            )}
            <button
              className="text-sm text-slate-500 hover:text-slate-700 dark:hover:text-white px-3 py-1.5"
              onClick={onFechar}
            >
              Fechar
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
