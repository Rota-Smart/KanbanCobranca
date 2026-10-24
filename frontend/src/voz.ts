// Captura de fala→texto local: o microfone é gravado em WAV 16 kHz mono no
// próprio navegador e os bytes vão ao backend, que roda o whisper-cli
// (whisper.cpp, modelo small quantizado) e devolve o texto. Sem Vosk/WASM:
// o modelo pequeno era impreciso e o grande estourava a memória do WebView.
//
// A transcrição é em LOTES: o segmento fecha a cada pausa (~1,2 s de
// silêncio) ou a cada ~3 s de fala (4-5 palavras), e vai direto pro
// whisper-cli — o texto aparece no chat na ordem, lote a lote.

interface OuvintesVoz {
  // parcial: texto dos lotes já transcritos enquanto ainda grava.
  parcial: (texto: string) => void;
  final: (texto: string) => void;
  erro: (e: unknown) => void;
  etapa?: (s: string) => void;
}

// esperar aguarda ms e resolve.
const esperar = (ms: number) => new Promise((r) => setTimeout(r, ms));

// abrirMicrofone tenta abrir o dispositivo com restrições progressivas
// (ocupado por outro programa é o caso comum; insistir costuma resolver).
async function abrirMicrofone(): Promise<MediaStream> {
  const cheias: MediaStreamConstraints = {
    audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true },
  };
  const tentativas: MediaStreamConstraints[] = [
    cheias,
    { audio: true },
  ];
  let ultimoErro: unknown;
  for (let rodada = 0; rodada < tentativas.length; rodada++) {
    for (let vez = 0; vez < 4; vez++) {
      try {
        return await navigator.mediaDevices.getUserMedia(tentativas[rodada]);
      } catch (e) {
        ultimoErro = e;
        const nome = e instanceof DOMException ? e.name : '';
        if (nome !== 'NotAllowedError' && nome !== 'NotReadableError' && nome !== 'TrackStartError' && nome !== 'AbortError') {
          throw e;
        }
        await esperar(vez === 3 ? 1200 : 400);
      }
    }
  }
  throw ultimoErro;
}

// mensagemDeErro traduz os erros do getUserMedia para algo acionável.
function mensagemDeErro(e: unknown): string {
  const nome = e instanceof DOMException ? e.name : '';
  const detalhe = e instanceof Error && e.message ? ` [${nome}: ${e.message}]` : '';
  switch (nome) {
    case 'NotReadableError':
    case 'TrackStartError':
      return `o microfone não pôde ser aberto: feche outros programas que usem microfone (Teams, Chrome em chamada, gravador)${detalhe}`;
    case 'NotAllowedError':
    case 'SecurityError':
      return `o acesso ao microfone foi negado — reinicie o app${detalhe}`;
    case 'NotFoundError':
    case 'OverconstrainedError':
      return `nenhum microfone compatível encontrado — verifique se há um microfone conectado e selecionado como padrão no Windows${detalhe}`;
    default:
      return String(e);
  }
}

// wav16kHz empacota as amostras (Float32, -1..1) num WAV PCM 16 bits mono.
function wav16kHz(amostras: Float32Array, taxa: number): Uint8Array {
  const pcm = new Int16Array(amostras.length);
  for (let i = 0; i < amostras.length; i++) {
    const v = Math.max(-1, Math.min(1, amostras[i]));
    pcm[i] = v < 0 ? v * 0x8000 : v * 0x7fff;
  }
  const cab = new ArrayBuffer(44);
  const dv = new DataView(cab);
  const texto = (off: number, s: string) => {
    for (let i = 0; i < s.length; i++) dv.setUint8(off + i, s.charCodeAt(i));
  };
  texto(0, 'RIFF');
  dv.setUint32(4, 36 + pcm.byteLength, true);
  texto(8, 'WAVE');
  texto(12, 'fmt ');
  dv.setUint32(16, 16, true);
  dv.setUint16(20, 1, true); // PCM
  dv.setUint16(22, 1, true); // mono
  dv.setUint32(24, taxa, true);
  dv.setUint32(28, taxa * 2, true);
  dv.setUint16(32, 2, true);
  dv.setUint16(34, 16, true);
  texto(36, 'data');
  dv.setUint32(40, pcm.byteLength, true);
  const saida = new Uint8Array(44 + pcm.byteLength);
  saida.set(new Uint8Array(cab), 0);
  saida.set(new Uint8Array(pcm.buffer), 44);
  return saida;
}

// bridge transcreve via Wails (base64: a ponte não carrega Uint8Array).
async function transcrever(wav: Uint8Array): Promise<string> {
  let b64 = '';
  const BLOCO = 0x8000;
  for (let i = 0; i < wav.length; i += BLOCO) {
    b64 += String.fromCharCode(...wav.subarray(i, i + BLOCO));
  }
  const ponte = (window as unknown as {
    go: { main: { App: { TranscreverVoz: (b: string) => Promise<string> } } };
  }).go.main.App;
  return ponte.TranscreverVoz(btoa(b64));
}

const TAXA = 16000;
const QUADRO = 4096; // amostras por callback do ScriptProcessor (~256 ms)
// limiar de RMS pra considerar "falando" (com cancelamento de eco ligado,
// o ruído de fundo fica bem abaixo disso).
const LIMIAR_RMS = 0.01;
const SIL_ENCERRA_MS = 1200; // pausa que fecha o lote
const FALA_MAX_MS = 3000; // ~4-5 palavras de fala contínua por lote
const MINIMO_FALA_MS = 350; // ignora estalos/cliques curtíssimos
const MAXIMO_GRAVACAO_MS = 10 * 60_000;

// iniciarVoz abre o microfone e começa a transcrever em lotes. Devolve a
// função de parada, que transcreve o trecho restante e entrega tudo no
// ouvinte final.
export async function iniciarVoz(ouvintes: OuvintesVoz): Promise<() => void> {
  let stream: MediaStream;
  try {
    stream = await abrirMicrofone();
  } catch (e) {
    throw new Error(mensagemDeErro(e));
  }

  const audioCtx = new AudioContext({ sampleRate: TAXA });
  const fonte = audioCtx.createMediaStreamSource(stream);
  const fatiador = audioCtx.createScriptProcessor(QUADRO, 1, 1);
  fonte.connect(fatiador);
  fatiador.connect(audioCtx.destination); // keep-alive do grafo de áudio

  let limpo = false;
  // lote corrente: amostras desde o último corte
  let buffer: Float32Array[] = [];
  let falaMs = 0; // fala dentro do lote corrente
  let silencioMs = 0; // silêncio no fim do lote corrente
  const textos: string[] = []; // lotes já transcritos, em ordem
  let fila: Float32Array[] | null = null; // lote aguardando transcrição
  let ocupado = false;
  let totalMs = 0;

  const parciais = () => ouvintes.parcial(textos.join(' '));

  const bombeia = async () => {
    if (ocupado || !fila) return;
    ocupado = true;
    const segmento = fila;
    fila = null;
    try {
      const total = segmento.reduce((n, p) => n + p.length, 0);
      const tudo = new Float32Array(total);
      let off = 0;
      for (const p of segmento) {
        tudo.set(p, off);
        off += p.length;
      }
      ouvintes.etapa?.('transcrevendo…');
      const texto = (await transcrever(wav16kHz(tudo, TAXA))).trim();
      if (texto) {
        textos.push(texto);
        parciais();
      }
    } catch (e) {
      ouvintes.erro(e);
    } finally {
      ocupado = false;
      if (fila) void bombeia();
    }
  };

  const emiteLote = () => {
    if (!buffer.length) return;
    if (falaMs < MINIMO_FALA_MS) {
      buffer = [];
      falaMs = 0;
      silencioMs = 0;
      return;
    }
    const segmento = buffer;
    buffer = [];
    falaMs = 0;
    silencioMs = 0;
    fila = fila ? fila.concat(segmento) : segmento;
    void bombeia();
  };

  fatiador.onaudioprocess = (e) => {
    if (limpo) return;
    const amostra = new Float32Array(e.inputBuffer.getChannelData(0));
    buffer.push(amostra);
    totalMs += (QUADRO / TAXA) * 1000;
    // RMS do quadro
    let soma = 0;
    for (let i = 0; i < amostra.length; i++) soma += amostra[i] * amostra[i];
    const rms = Math.sqrt(soma / amostra.length);
    if (rms > LIMIAR_RMS) {
      falaMs += (QUADRO / TAXA) * 1000;
      silencioMs = 0;
    } else if (falaMs > 0) {
      silencioMs += (QUADRO / TAXA) * 1000;
    }
    if (falaMs > 0 && (silencioMs >= SIL_ENCERRA_MS || falaMs >= FALA_MAX_MS)) {
      emiteLote();
    }
    if (totalMs >= MAXIMO_GRAVACAO_MS) {
      limpo = true; // para de aceitar áudio; o usuário clica pra encerrar
    }
  };

  const encerrar = async () => {
    limpo = true;
    stream.getTracks().forEach((t) => t.stop());
    fatiador.onaudioprocess = null;
    fonte.disconnect();
    fatiador.disconnect();
    void audioCtx.close();
    // derruba o lote pendente na fila e espera tudo transcrever
    emiteLote();
    while (fila || ocupado) {
      await bombeia();
      await esperar(120);
      if (!fila && !ocupado) break;
    }
    const textoFinal = textos.join(' ').trim();
    if (textoFinal) ouvintes.final(textoFinal);
    else ouvintes.erro(new Error('não captei fala nenhuma — fale mais perto do microfone'));
  };

  // teto de duração: encerra e entrega sozinho
  setTimeout(() => void encerrar(), MAXIMO_GRAVACAO_MS + 30_000);

  return () => {
    void encerrar();
  };
}
