import { useEffect, useState } from 'react';
import * as App from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import type { ConfigApp, ParametroNegociacao, SyncInfo } from '../wails-types';

const MODELOS_SUGERIDOS = [
  'openai/gpt-oss-120b',
  'openai/gpt-oss-20b',
  'qwen/qwen3.6-27b',
];

const MODELOS_DEEPINFRA = [
  'meta-llama/Llama-3.3-70B-Instruct',
  'meta-llama/Meta-Llama-3.1-8B-Instruct',
  'Qwen/Qwen2.5-72B-Instruct',
];

// Configuracoes reúne: sessão do 360, chave/modelo Groq e parâmetros
// de negociação (texto ou arquivo).
export default function Configuracoes(props: { onFechar: () => void; onToast: (msg: string) => void }) {
  const [cfg, setCfg] = useState<ConfigApp | null>(null);
  const [chave, setChave] = useState('');
  const [modelo, setModelo] = useState('');
  const [chaveDI, setChaveDI] = useState('');
  const [modeloDI, setModeloDI] = useState('');
  const [cookieTxt, setCookieTxt] = useState('');
  const [paramTexto, setParamTexto] = useState('');
  const [params, setParams] = useState<ParametroNegociacao[]>([]);
  const [salvando, setSalvando] = useState(false);
  const [fonte, setFonte] = useState('');
  const [verificando, setVerificando] = useState(false);
  const [atualizando, setAtualizando] = useState(false);
  const [infoAtual, setInfoAtual] = useState<import('../wails-types').InfoAtualizacao | null>(null);
  const [syncToken, setSyncToken] = useState('');
  const [syncRepo, setSyncRepo] = useState('');
  const [syncInfo, setSyncInfo] = useState<SyncInfo | null>(null);
  const [syncOcupado, setSyncOcupado] = useState(false);
  const [usoGroq, setUsoGroq] = useState<import('../wails-types').UsoGroq | null>(null);

  function recarregarSync() {
    App.SyncStatus().then(setSyncInfo).catch(() => setSyncInfo(null));
  }

  function recarregar() {
    App.GetConfig().then(setCfg).catch((e) => props.onToast(String(e)));
    App.ListarParametros().then(setParams).catch(() => {});
    App.UsoGroqHoje().then(setUsoGroq).catch(() => setUsoGroq(null));
  }

  useEffect(() => {
    recarregar();
    App.GetFonteAtualizacao().then(setFonte).catch(() => {});
    recarregarSync();
  }, []);

  async function verificarAtualizacao() {
    setVerificando(true);
    try {
      setInfoAtual(await App.VerificarAtualizacao());
    } catch (e) {
      props.onToast(String(e));
    } finally {
      setVerificando(false);
    }
  }

  const [progresso, setProgresso] = useState<number | null>(null);
  const [baixadosMb, setBaixadosMb] = useState(0);

  // Clicar em atualizar devolve na hora: o download do exe (~79 MB) roda em
  // background no Go e continua mesmo se o usuário fechar esta tela. O
  // resultado final (erro/pronto) é anunciado por toasts globais no App;
  // aqui fica só a barrinha de progresso.
  async function instalarAtualizacao() {
    setAtualizando(true);
    setProgresso(null);
    try {
      const versao = await App.InstalarAtualizacao();
      props.onToast(`Baixando a versão ${versao} em segundo plano…`);
    } catch (e) {
      props.onToast(String(e));
      setAtualizando(false);
    }
  }

  useEffect(() => {
    const offP = EventsOn('atualizacao:progresso', (d: { baixados: number; total: number; percento: number }) => {
      setProgresso(d.percento >= 0 ? d.percento : -1);
      setBaixadosMb(Math.round(d.baixados / (1024 * 1024)));
    });
    const offOk = EventsOn('atualizacao:pronto', () => {
      setProgresso(100);
      // o app reinicia sozinho em seguida
    });
    const offErr = EventsOn('atualizacao:erro', () => {
      setAtualizando(false);
      setProgresso(null);
    });
    return () => {
      offP();
      offOk();
      offErr();
    };
  }, []);

  async function salvarGroq() {
    setSalvando(true);
    try {
      await App.SetGroqConfig(chave.trim(), modelo.trim());
      setChave('');
      props.onToast('Configurações da Groq salvas.');
      recarregar();
    } catch (e) {
      props.onToast(String(e));
    } finally {
      setSalvando(false);
    }
  }

  async function salvarDeepInfra() {
    setSalvando(true);
    try {
      await App.SetDeepInfraConfig(chaveDI.trim(), modeloDI.trim());
      setChaveDI('');
      props.onToast('Configurações da DeepInfra salvas.');
      recarregar();
    } catch (e) {
      props.onToast(String(e));
    } finally {
      setSalvando(false);
    }
  }

  async function escolherProvedor(p: string) {
    try {
      await App.SetProvedorIA(p);
      props.onToast(p === 'deepinfra' ? 'Provedor: DeepInfra.' : 'Provedor: Groq.');
      recarregar();
    } catch (e) {
      props.onToast(String(e));
    }
  }

  const [autorTxt, setAutorTxt] = useState('');

  // O nome no 360 chega junto do cfg; sincroniza o input quando carregar.
  useEffect(() => {
    if (cfg) setAutorTxt(cfg.autor360 ?? '');
  }, [cfg]);

  async function salvarAutor() {
    try {
      await App.SetAutor360(autorTxt.trim());
      props.onToast('Nome no 360 salvo.');
      recarregar();
    } catch (e) {
      props.onToast(String(e));
    }
  }

  const [conectando360, setConectando360] = useState(false);

  // A conexão CDP roda em background no Go (pode esperar o login do usuário
  // na janela do Chrome); o resultado chega pelo evento rota360:conectado.
  async function conectarChrome() {
    setConectando360(true);
    try {
      await App.ConectarRota360();
    } catch (e) {
      props.onToast(String(e));
      setConectando360(false);
    }
  }

  useEffect(() => {
    if (!conectando360) return;
    const off = EventsOn('rota360:conectado', (st: { conectado: boolean; mensagem?: string }) => {
      setConectando360(false);
      if (st.conectado) {
        props.onToast('360 conectado.');
        recarregar();
      } else {
        props.onToast(st.mensagem || 'Não foi possível conectar ao 360.');
      }
    });
    return () => off();
  }, [conectando360]);

  async function colarCookie() {
    if (!cookieTxt.trim()) return;
    try {
      await App.DefinirCookieRota360(cookieTxt.trim());
      setCookieTxt('');
      props.onToast('Sessão do 360 configurada.');
      recarregar();
    } catch (e) {
      props.onToast(String(e));
    }
  }

  async function desconectar() {
    try {
      await App.DesconectarRota360();
      recarregar();
    } catch (e) {
      props.onToast(String(e));
    }
  }

  async function salvarParamTexto() {
    if (!paramTexto.trim()) return;
    try {
      await App.SalvarParametroTexto(paramTexto);
      setParamTexto('');
      recarregar();
    } catch (e) {
      props.onToast(String(e));
    }
  }

  async function anexarArquivo() {
    try {
      const p = await App.EscolherEAnexarParametro();
      if (p) recarregar();
    } catch (e) {
      props.onToast(String(e));
    }
  }

  async function remover(id: number) {
    try {
      await App.RemoverParametro(id);
      recarregar();
    } catch (e) {
      props.onToast(String(e));
    }
  }

  async function escolherPlanilha() {
    try {
      const res = await App.SelecionarPlanilhaOrcamento();
      if (res) props.onToast(res);
    } catch (e) {
      props.onToast(String(e));
    }
  }

  async function salvarSync() {
    setSyncOcupado(true);
    try {
      await App.SetSyncConfig(syncToken.trim(), syncRepo.trim());
      setSyncToken('');
      props.onToast('Configurações de sincronização salvas.');
      recarregarSync();
    } catch (e) {
      props.onToast(String(e));
    } finally {
      setSyncOcupado(false);
    }
  }

  async function syncAcao(acao: () => Promise<{ mensagem: string }>) {
    setSyncOcupado(true);
    try {
      props.onToast((await acao()).mensagem);
      recarregarSync();
    } catch (e) {
      props.onToast(String(e));
    } finally {
      setSyncOcupado(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 backdrop-blur-sm" onClick={props.onFechar}>
      <div
        className="glass rounded-2xl w-[36rem] max-w-full max-h-[85vh] overflow-y-auto p-5 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <h2 className="font-semibold text-lg dark:text-slate-100">Configurações</h2>
          <button aria-label="Fechar" className="text-slate-400 hover:text-slate-600" onClick={props.onFechar}>
            ✕
          </button>
        </div>

        {/* Sessão Rota 360 */}
        <section className="mt-4 border rounded-lg p-3">
          <h3 className="text-sm font-semibold text-slate-700">Sessão do 360 (rotagroup)</h3>
          <p className="text-xs text-slate-500 mt-1">
            Clique em <b>Conectar</b>: o app abre o site do 360 numa guia do Chrome. Faça login lá
            uma única vez — o app captura o cookie da guia e salva. Nas próximas vezes a conexão é
            automática (o Chrome principal não é fechado). Se preferir, cole o cookie manualmente:
            no site, F12 → Application → Cookies → copie o valor do cookie de sessão e cole abaixo.
          </p>
          {cfg && (
            <p className="mt-2 text-sm">
              Status:{' '}
              {cfg.rotaConectado ? (
                <span className="text-emerald-600 font-medium">
                  Conectado{cfg.rotaOrigem ? ` (${cfg.rotaOrigem})` : ''}
                </span>
              ) : (
                <span className="text-red-600 font-medium">Desconectado</span>
              )}
              {cfg.rotaMensagem && <span className="block text-xs text-slate-500">{cfg.rotaMensagem}</span>}
            </p>
          )}
          <div className="mt-2 flex gap-2">
            <button className="border rounded px-3 py-1 text-sm" disabled={conectando360} onClick={conectarChrome}>
              {conectando360 ? 'Conectando… faça login na guia do Chrome' : 'Conectar'}
            </button>
            {cfg?.rotaConectado && (
              <button className="border rounded px-3 py-1 text-sm text-red-600" onClick={desconectar}>
                Desconectar
              </button>
            )}
          </div>
          <div className="mt-2 flex gap-1">
            <input
              className="flex-1 border rounded px-2 py-1 text-sm"
              placeholder="Colar cookie da sessão (fallback)"
              value={cookieTxt}
              onChange={(e) => setCookieTxt(e.target.value)}
            />
            <button className="border rounded px-3 py-1 text-sm" disabled={!cookieTxt.trim()} onClick={colarCookie}>
              Usar
            </button>
          </div>
        </section>

        {/* IA (Groq ou DeepInfra) */}
        <section className="mt-3 border rounded-lg p-3">
          <h3 className="text-sm font-semibold text-slate-700">IA (polimento e assistente)</h3>
          <div className="mt-2 flex items-center gap-2 text-sm">
            <span className="text-slate-600">Provedor:</span>
            {[
              ['groq', 'Groq'],
              ['deepinfra', 'DeepInfra'],
            ].map(([valor, rotulo]) => (
              <button
                key={valor}
                className={
                  'border rounded px-3 py-1 text-sm ' +
                  (cfg?.provedor === valor
                    ? 'bg-blue-600 text-white border-blue-600'
                    : 'border-slate-300 text-slate-600')
                }
                onClick={() => escolherProvedor(valor)}
              >
                {rotulo}
              </button>
            ))}
          </div>
          {usoGroq && cfg?.provedor !== 'deepinfra' && (
            <p className="text-xs text-slate-500 mt-1">
              Uso hoje: <strong>{usoGroq.requisicoes}</strong> requisições ·{' '}
              <strong>{Math.round((usoGroq.promptTokens + usoGroq.completionTokens) / 100) / 10}k</strong> tokens{' '}
              <span className="text-slate-400">(limites do plano free: 1.000 req/dia, 200k tokens/dia)</span>
            </p>
          )}
          {cfg?.provedor === 'deepinfra' ? (
            <>
              <p className="text-xs text-slate-500 mt-2">
                Chave em deepinfra.com/dashboard/api-tokens. Modelo sugerido:{' '}
                <strong>meta-llama/Llama-3.3-70B-Instruct</strong> (pago por token, sem cota free).
                {cfg?.deepInfraTemChave && ' Uma chave já está salva.'}
              </p>
              <div className="mt-2 flex flex-col gap-2">
                <input
                  className="border rounded px-2 py-1 text-sm"
                  type="password"
                  placeholder="Chave da API DeepInfra"
                  value={chaveDI}
                  onChange={(e) => setChaveDI(e.target.value)}
                />
                <input
                  className="border rounded px-2 py-1 text-sm"
                  placeholder="Modelo (padrão: meta-llama/Llama-3.3-70B-Instruct)"
                  value={modeloDI}
                  onChange={(e) => setModeloDI(e.target.value)}
                  list="modelos-deepinfra"
                />
                <datalist id="modelos-deepinfra">
                  {MODELOS_DEEPINFRA.map((m) => (
                    <option key={m} value={m} />
                  ))}
                </datalist>
                <div className="flex gap-2">
                  <button
                    className="bg-blue-600 text-white rounded px-3 py-1 text-sm disabled:opacity-50"
                    disabled={salvando || (!chaveDI.trim() && !modeloDI.trim())}
                    onClick={salvarDeepInfra}
                  >
                    Salvar
                  </button>
                  {cfg?.deepInfraTemChave && (
                    <button
                      className="border rounded px-3 py-1 text-sm text-red-600"
                      onClick={() => App.LimparDeepInfraChave().then(recarregar)}
                    >
                      Remover chave
                    </button>
                  )}
                </div>
              </div>
            </>
          ) : (
            <>
              <p className="text-xs text-slate-500 mt-2">
                Chave gratuita em console.groq.com. Modelo sugerido:{' '}
                <strong>openai/gpt-oss-120b</strong> (alternativa rápida: openai/gpt-oss-20b — o
                antigo llama-3.3-70b-versatile foi descontinuado pela Groq).
                {cfg?.groqTemChave && ' Uma chave já está salva.'}
              </p>
              <div className="mt-2 flex flex-col gap-2">
                <input
                  className="border rounded px-2 py-1 text-sm"
                  type="password"
                  placeholder="Chave da API (gsk_…)"
                  value={chave}
                  onChange={(e) => setChave(e.target.value)}
                />
                <input
                  className="border rounded px-2 py-1 text-sm"
                  placeholder={cfg?.groqModelo || 'Modelo'}
                  value={modelo}
                  onChange={(e) => setModelo(e.target.value)}
                  list="modelos-groq"
                />
                <datalist id="modelos-groq">
                  {MODELOS_SUGERIDOS.map((m) => (
                    <option key={m} value={m} />
                  ))}
                </datalist>
                <div className="flex gap-2">
                  <button
                    className="bg-blue-600 text-white rounded px-3 py-1 text-sm disabled:opacity-50"
                    disabled={salvando || (!chave.trim() && !modelo.trim())}
                    onClick={salvarGroq}
                  >
                    Salvar
                  </button>
                  {cfg?.groqTemChave && (
                    <button
                      className="border rounded px-3 py-1 text-sm text-red-600"
                      onClick={() => App.LimparGroqChave().then(recarregar)}
                    >
                      Remover chave
                    </button>
                  )}
                </div>
              </div>
            </>
          )}
          <div className="mt-2 flex gap-1">
            <input
              className="border rounded px-2 py-1 text-sm"
              placeholder="Seu nome no 360 (como aparece nas suas respostas)"
              value={autorTxt}
              onChange={(e) => setAutorTxt(e.target.value)}
            />
            <button className="border rounded px-3 py-1 text-sm" disabled={!autorTxt.trim()} onClick={salvarAutor}>
              Salvar
            </button>
          </div>
          <p className="text-xs text-slate-500">
            O assistente resume o ticket com a descrição e a última mensagem que não é sua; diga aqui
            quem é você para ele filtrar direito.
          </p>
        </section>

        {/* Parâmetros de negociação */}
        <section className="mt-3 border rounded-lg p-3">
          <h3 className="text-sm font-semibold text-slate-700">Parâmetros de negociação</h3>
          <p className="text-xs text-slate-500 mt-1">
            Regras usadas pela IA ao polir a proposta (limites de desconto, entrada mínima etc.).
            Aceita texto digitado ou arquivo pdf/docx/txt.
          </p>
          <textarea
            className="mt-2 w-full border rounded p-2 text-sm h-24"
            placeholder="Ex.: entrada mínima de 20%; máximo de 12x; clientes com mais de 1 ano podem receber 1 mensalidade de cortesia…"
            value={paramTexto}
            onChange={(e) => setParamTexto(e.target.value)}
          />
          <div className="mt-2 flex gap-2">
            <button
              className="bg-blue-600 text-white rounded px-3 py-1 text-sm disabled:opacity-50"
              disabled={!paramTexto.trim()}
              onClick={salvarParamTexto}
            >
              Salvar texto
            </button>
            <button className="border rounded px-3 py-1 text-sm" onClick={anexarArquivo}>
              Anexar arquivo…
            </button>
          </div>
          {params.length > 0 && (
            <ul className="mt-3 flex flex-col gap-1 text-xs">
              {params.map((p) => (
                <li key={p.id} className="border rounded p-2 flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <p className="font-medium">
                      {p.fonte === 'arquivo' ? `📎 ${p.nomeArquivo || 'arquivo'}` : '✏️ Texto'}
                      <span className="ml-2 text-slate-400">{p.createdAt?.slice(0, 10)}</span>
                    </p>
                    <p className="text-slate-500 line-clamp-2 whitespace-pre-wrap">{p.conteudo.slice(0, 160)}</p>
                  </div>
                  <button className="shrink-0 text-red-600" onClick={() => remover(p.id)}>
                    Remover
                  </button>
                </li>
              ))}
            </ul>
          )}
        </section>
        {/* Sincronização */}
        <section className="mt-3 border rounded-lg p-3">
          <h3 className="text-sm font-semibold text-slate-700">Sincronização (GitHub)</h3>
          <p className="text-xs text-slate-500 mt-1">
            Envia o banco do Kanban para o repositório privado <code>banco-de-dados</code> e baixa
            dele — assim outro PC vê os mesmos dados. Crie um token em{' '}
            github.com/settings/tokens (clássico, escopo <strong>repo</strong>) e cole abaixo.
            {syncInfo?.temRemoto && (
              <> Última versão no repo: <strong>{syncInfo.remotoQuando}</strong>.</>
            )}
          </p>
          {syncInfo?.pendenteBaixar && (
            <p className="mt-1 text-xs text-amber-600">
              Há dados baixados aguardando — reinicie o app para aplicá-los.
            </p>
          )}
          <div className="mt-2 flex flex-col gap-2">
            <input
              className="border rounded px-2 py-1 text-sm"
              type="password"
              placeholder="Token do GitHub (ghp_…, escopo repo)"
              value={syncToken}
              onChange={(e) => setSyncToken(e.target.value)}
            />
            <input
              className="border rounded px-2 py-1 text-sm"
              placeholder={syncInfo?.repo || 'luisboneta/banco-de-dados'}
              value={syncRepo}
              onChange={(e) => setSyncRepo(e.target.value)}
            />
            <div className="flex gap-2">
              <button
                className="bg-blue-600 text-white rounded px-3 py-1 text-sm disabled:opacity-50"
                disabled={syncOcupado || !syncToken.trim()}
                onClick={salvarSync}
              >
                Salvar
              </button>
              {syncInfo?.configurado && (
                <>
                  <button
                    className="border rounded px-3 py-1 text-sm disabled:opacity-50"
                    disabled={syncOcupado}
                    onClick={() => syncAcao(App.SyncEnviar)}
                    title="Sobe o banco deste PC para o repo"
                  >
                    Enviar dados
                  </button>
                  <button
                    className="border rounded px-3 py-1 text-sm disabled:opacity-50"
                    disabled={syncOcupado}
                    onClick={() => {
                      if (window.confirm('Baixar os dados do repo? Eles substituem os deste PC quando o app reiniciar.'))
                        syncAcao(App.SyncReceber);
                    }}
                    title="Baixa o banco do repo (aplica ao reiniciar)"
                  >
                    Baixar dados
                  </button>
                </>
              )}
            </div>
          </div>
        </section>

        {/* Planilha da calculadora de orçamentos */}
        <section className="mt-3 border rounded-lg p-3">
          <h3 className="text-sm font-semibold text-slate-700">Planilha da calculadora (orçamentos)</h3>
          <p className="text-xs text-slate-500 mt-1">
            Planilha .xlsx com os preços usados no botão 🧮 Orçamento e no assistente (abas = tipos
            de sistema, seções de itens, taxa de instalação e mensalidade). Troque aqui para atualizar
            valores — uma aba por tipo (ex.: ALARME, CAMERAS WIFI, CAMERAS DVR, RASTREAMENTO).
          </p>
          <div className="mt-2 flex gap-2">
            <button className="border rounded px-3 py-1 text-sm" onClick={escolherPlanilha}>
              Escolher planilha…
            </button>
          </div>
        </section>

        {/* Voz do assistente (fala→texto local via Whisper) */}
        <section className="mt-3 border rounded-lg p-3">
          <h3 className="text-sm font-semibold text-slate-700">Voz do assistente (fala→texto)</h3>
          <p className="text-xs text-slate-500 mt-1">
            O botão 🎤 no chat grava sua fala e transcreve <strong>localmente</strong> com Whisper
            (whisper.cpp, modelo small em português — sem internet, sem gastar tokens). Clique no 🎤,
            fale e clique de novo para transcrever. Binários e modelo ficam na pasta de dados do app
            (<code>whisper\</code>).
          </p>
        </section>

        {/* Atualizações */}
        <section className="mt-3 border rounded-lg p-3">
          <h3 className="text-sm font-semibold text-slate-700">Atualizações</h3>
          <p className="text-xs text-slate-500 mt-1">
            O app busca a versão nova em um repositório GitHub (branch de release
            com <code>versao.txt</code> e <code>rotasuite.exe</code>) e se reinstala sozinho.
          </p>
          <div className="mt-2 flex gap-1">
            <input
              className="flex-1 border rounded px-2 py-1 text-sm"
              placeholder="URL da fonte (branch release)"
              value={fonte}
              onChange={(e) => setFonte(e.target.value)}
            />
            <button
              className="border rounded px-3 py-1 text-sm"
              onClick={() => App.SetFonteAtualizacao(fonte.trim()).then(() => props.onToast('Fonte salva.')).catch((e) => props.onToast(String(e)))}
            >
              Salvar
            </button>
          </div>
          <div className="mt-2 flex items-center gap-2">
            <button className="border rounded px-3 py-1 text-sm" disabled={verificando} onClick={verificarAtualizacao}>
              {verificando ? 'Verificando…' : 'Verificar atualizações'}
            </button>
            {infoAtual && (
              <span className="text-xs text-slate-500">
                Atual: {infoAtual.versaoAtual} ·{' '}
                {infoAtual.temNova ? (
                  <strong className="text-emerald-600">Nova: {infoAtual.versaoNova}</strong>
                ) : (
                  'já é a mais recente'
                )}
              </span>
            )}
          </div>
          {infoAtual?.temNova && (
            <>
              <button
                className="mt-2 bg-emerald-600 text-white rounded px-3 py-1 text-sm disabled:opacity-50"
                disabled={atualizando}
                onClick={instalarAtualizacao}
              >
                {atualizando ? 'Baixando em segundo plano…' : `Atualizar para ${infoAtual.versaoNova} (o app reinicia)`}
              </button>
              {progresso !== null && (
                <p className="mt-1 text-xs text-slate-500">
                  {progresso >= 0
                    ? `Baixando… ${progresso}%`
                    : `Baixando… ${baixadosMb} MB`}{' '}
                  — pode fechar esta tela, o download continua e o resultado aparece como aviso.
                </p>
              )}
            </>
          )}
        </section>
      </div>
    </div>
  );
}
