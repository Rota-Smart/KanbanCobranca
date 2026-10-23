// Captura de fala→texto com Vosk rodando em WASM dentro da WebView
// (vosk-browser). Segue os princípios do streaming clássico: o microfone é
// capturado contínuo, fatiado e já reamostrado para 16 kHz mono pelo
// AudioContext, cada fatia vai direto ao reconhecedor (AcceptWaveform),
// resultados parciais chegam enquanto o usuário fala e o resultado final
// vem na pausa. Sem WebSocket/servidor: o túnel é interno à página.
//
// O modelo é servido pelo backend do app em /vosk/ (arquivos do disco).

interface OuvintesVoz {
  parcial: (texto: string) => void;
  final: (texto: string) => void;
  erro: (e: unknown) => void;
}

type ModeloVosk = Awaited<ReturnType<typeof import('vosk-browser').createModel>>;

// esperar aguarda ms e resolve.
const esperar = (ms: number) => new Promise((r) => setTimeout(r, ms));

// abrirMicrofone tenta abrir o dispositivo com restrições progressivas.
// O NotReadableError costuma significar que a WebView2 não conseguiu INICIAR
// o microfone (ocupado por outro programa — inclusive o próprio Windows em
// modo exclusivo —, acesso de apps de área de trabalho bloqueado, ou o driver
// não aceita o processamento pedido). Não há como expulsar o app que segura
// o microfone em modo exclusivo, então a estratégia é insistir: percorrer
// TODOS os microfones com restrições relaxadas, com retentativas espaçadas,
// porque na prática o dispositivo costuma ser liberado em poucos segundos.
async function abrirMicrofone(): Promise<MediaStream> {
  const cheias: MediaStreamConstraints = {
    audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true },
  };
  const tentativas: MediaStreamConstraints[] = [
    cheias,
    { audio: true },
    { audio: { deviceId: { ideal: 'default' } } }, // último recurso: sem processamento nenhum
  ];

  let ultimoErro: unknown;
  // 3 rodadas (normal, básica, sem restrição) × 4 repetições espaçadas
  for (let rodada = 0; rodada < tentativas.length; rodada++) {
    for (let vez = 0; vez < 4; vez++) {
      try {
        return await navigator.mediaDevices.getUserMedia(tentativas[rodada]);
      } catch (e) {
        ultimoErro = e;
        const nome = e instanceof DOMException ? e.name : '';
        // O app roda com auto-concede (--use-fake-ui-for-media-stream); se
        // mesmo assim vier negado, é o ambiente do WebView2 ainda preparando
        // — repetir resolve. Erro de dispositivo ocupado/lento também repete.
        if (
          nome !== 'NotAllowedError' &&
          nome !== 'NotReadableError' &&
          nome !== 'TrackStartError' &&
          nome !== 'AbortError' &&
          nome !== 'NotSupportedError'
        )
          throw e;
        await esperar(vez === 3 ? 1200 : 400); // espera cresce na última vez da rodada
      }
    }
  }
  throw ultimoErro;
}

// mensagemDeErro traduz os erros do getUserMedia para algo acionável.
function mensagemDeErro(e: unknown): string {
  const nome = e instanceof DOMException ? e.name : '';
  switch (nome) {
    case 'NotReadableError':
    case 'TrackStartError':
      return 'o microfone não pôde ser aberto: feche outros programas que usem microfone (Teams, Chrome em chamada, gravador) e verifique Configurações do Windows › Privacidade › Microfone › permitir que apps de área de trabalho acessem o microfone';
    case 'NotAllowedError':
    case 'SecurityError':
      return 'o acesso ao microfone foi negado — reinicie o app';
    case 'NotFoundError':
    case 'OverconstrainedError':
      return 'nenhum microfone compatível encontrado — verifique se há um microfone conectado e selecionado como padrão no Windows';
    default:
      return String(e);
  }
}

// iniciarVoz abre o microfone e começa a transcrever. Devolve a função de
// parada, que encerra stream, reconhecedor e modelo.
export async function iniciarVoz(ouvintes: OuvintesVoz): Promise<() => void> {
  const { createModel } = await import('vosk-browser');

  let stream: MediaStream;
  try {
    stream = await abrirMicrofone();
  } catch (e) {
    throw new Error(mensagemDeErro(e));
  }
  // 16 kHz mono no próprio navegador: o Vosk exige esse formato e o
  // Chromium reamostra a faixa do microfone na criação do contexto.
  const audioCtx = new AudioContext({ sampleRate: 16000 });

  let modelo: ModeloVosk | null = null;
  let rec: KaldiRecognizer | null = null;
  let limpo = false;

  function encerrar() {
    if (limpo) return;
    limpo = true;
    stream.getTracks().forEach((t) => t.stop());
    void audioCtx.close();
    if (rec) {
      try {
        rec.retrieveFinalResult();
        rec.remove();
      } catch {
        /* recognizer já encerrado */
      }
    }
    modelo?.terminate();
  }

  try {
    modelo = await createModel('/vosk');
    rec = new modelo.KaldiRecognizer(audioCtx.sampleRate);
    rec.on('partialresult', (msg) => {
      if (msg.event === 'partialresult' && msg.result.partial) ouvintes.parcial(msg.result.partial);
    });
    rec.on('result', (msg) => {
      if (msg.event === 'result' && msg.result.text) ouvintes.final(msg.result.text);
    });

    // Fatiador contínuo: cada bloco de ~256 ms vai ao reconhecedor.
    const fatiador = audioCtx.createScriptProcessor(4096, 1, 1);
    const fonte = audioCtx.createMediaStreamSource(stream);
    fonte.connect(fatiador);
    fatiador.connect(audioCtx.destination); // keep-alive do grafo de áudio
    fatiador.onaudioprocess = (e) => {
      if (limpo) return;
      const amostra = e.inputBuffer.getChannelData(0);
      try {
        rec?.acceptWaveformFloat(new Float32Array(amostra), audioCtx.sampleRate);
      } catch (err) {
        ouvintes.erro(err);
      }
    };

    return encerrar;
  } catch (e) {
    encerrar();
    throw e;
  }
}

// KaldiRecognizer é exportado como propriedade da Model no vosk-browser.
type KaldiRecognizer = InstanceType<ModeloVosk['KaldiRecognizer']>;
