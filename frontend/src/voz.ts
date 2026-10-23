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

// iniciarVoz abre o microfone e começa a transcrever. Devolve a função de
// parada, que encerra stream, reconhecedor e modelo.
export async function iniciarVoz(ouvintes: OuvintesVoz): Promise<() => void> {
  const { createModel } = await import('vosk-browser');

  const stream = await navigator.mediaDevices.getUserMedia({
    audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true },
  });
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
