package main

// Voz na aba do Chrome (fallback do microfone do WebView2).
//
// Em máquinas onde o getUserMedia do WebView2 falha mas o Chrome normal
// funciona, o app injeta via CDP um gravador na aba que já está aberta no
// 360 (mesmo perfil-cdp da conexão assistida — sem abrir aba ou janela
// nova) e recebe os lotes de WAV de volta por Runtime.evaluate. Cada lote
// passa pelo mesmo whisper-cli da TranscreverVoz e o texto chega ao
// frontend pelos eventos "voz:lote" (parcial), "voz:fim" e "voz:erro".

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"rota-smart/internal/rota360"
)

const (
	evVozLote = "voz:lote"
	evVozFim  = "voz:fim"
	evVozErro = "voz:erro"
)

// jsVozInjetar instala o gravador na página. Replica a estratégia do
// voz.ts: taxa NATIVA do dispositivo, reamostragem para 16 kHz, lotes
// fechados por pausa (~1,2 s) ou teto de fala (~3 s). Os lotes saem em
// base64 por __rsVozLote(); __rsVozParar() encerra e sinaliza o fim.
const jsVozInjetar = `(async function(){
if (window.__rsVoz) return {ok:true, erro:''};
const TAXA=16000, QUADRO=4096, LIMIAR=0.01, SIL=1200, FALA_MAX=3000, MIN_FALA=350, MAX_MS=600000;
const esp=(ms)=>new Promise(r=>setTimeout(r,ms));
const estado={fila:[],fim:false,erro:'',parar:null};
window.__rsVoz=estado;
function b64(bytes){let s='';for(let i=0;i<bytes.length;i+=0x8000)s+=String.fromCharCode.apply(null,bytes.subarray(i,i+0x8000));return btoa(s);}
function wav(amostras,taxa){
  const pcm=new Int16Array(amostras.length);
  for(let i=0;i<amostras.length;i++){const v=Math.max(-1,Math.min(1,amostras[i]));pcm[i]=v<0?v*0x8000:v*0x7fff;}
  const cab=new ArrayBuffer(44),dv=new DataView(cab);
  const txt=(o,s)=>{for(let i=0;i<s.length;i++)dv.setUint8(o+i,s.charCodeAt(i));};
  txt(0,'RIFF');dv.setUint32(4,36+pcm.byteLength,true);txt(8,'WAVE');txt(12,'fmt ');
  dv.setUint32(16,16,true);dv.setUint16(20,1,true);dv.setUint16(22,1,true);
  dv.setUint32(24,taxa,true);dv.setUint32(28,taxa*2,true);dv.setUint16(32,2,true);dv.setUint16(34,16,true);
  txt(36,'data');dv.setUint32(40,pcm.byteLength,true);
  const out=new Uint8Array(44+pcm.byteLength);out.set(new Uint8Array(cab),0);out.set(new Uint8Array(pcm.buffer),44);
  return out;
}
function p16(a,t){if(t===TAXA)return a;const n=Math.round(a.length*TAXA/t),out=new Float32Array(n);
  for(let i=0;i<n;i++){const p=(i*(a.length-1))/(n-1||1),j=Math.floor(p),f=p-j;out[i]=a[j]*(1-f)+(a[j+1]??a[j])*f;}return out;}
window.__rsVozLote=()=>estado.fila.length?estado.fila.shift():'';
window.__rsVozParar=()=>{estado.fim=true;if(estado.parar)estado.parar();};
async function abrirMic(){
  const tentativas=[{audio:true},{audio:{channelCount:1,echoCancellation:true,noiseSuppression:true}},
    {audio:{channelCount:1,echoCancellation:false,noiseSuppression:false,autoGainControl:false}}];
  let ultimo=null;
  for(const c of tentativas){for(let v=0;v<4;v++){
    try{return await navigator.mediaDevices.getUserMedia(c);}catch(e){ultimo=e;
      const n=e&&e.name||'';if(n!=='NotAllowedError'&&n!=='NotReadableError'&&n!=='TrackStartError'&&n!=='AbortError')throw e;
      await esp(v===3?1200:400);}}}
  throw ultimo;
}
try{
  const stream=await abrirMic();
  const ctx=new AudioContext(),taxa=ctx.sampleRate;
  const fonte=ctx.createMediaStreamSource(stream),fat=ctx.createScriptProcessor(QUADRO,1,1);
  fonte.connect(fat);fat.connect(ctx.destination);
  let buffer=[],falaMs=0,silMs=0,totalMs=0,limpo=false;
  function emite(){
    if(!buffer.length)return;
    if(falaMs<MIN_FALA){buffer=[];falaMs=0;silMs=0;return;}
    const tudo=new Float32Array(buffer.reduce((n,p)=>n+p.length,0));let off=0;
    for(const p of buffer){tudo.set(p,off);off+=p.length;}
    buffer=[];falaMs=0;silMs=0;
    estado.fila.push(b64(wav(p16(tudo,taxa),TAXA)));
  }
  fat.onaudioprocess=(e)=>{
    if(limpo)return;
    const a=new Float32Array(e.inputBuffer.getChannelData(0));
    buffer.push(a);totalMs+=(QUADRO/taxa)*1000;
    let s=0;for(let i=0;i<a.length;i++)s+=a[i]*a[i];
    const rms=Math.sqrt(s/a.length);
    if(rms>LIMIAR){falaMs+=(QUADRO/taxa)*1000;silMs=0;}
    else if(falaMs>0)silMs+=(QUADRO/TAXA)*1000;
    if(falaMs>0&&(silMs>=SIL||falaMs>=FALA_MAX))emite();
    if(totalMs>=MAX_MS){limpo=true;estado.fim=true;emite();}
  };
  estado.parar=()=>{limpo=true;stream.getTracks().forEach(t=>t.stop());fat.onaudioprocess=null;emite();void ctx.close();};
  return {ok:true,erro:''};
}catch(e){
  const nome=e&&e.name||'';
  let msg=String(e&&(e.message||e));
  if(nome==='NotReadableError'||nome==='TrackStartError')msg='o microfone não pôde ser aberto no Chrome';
  else if(nome==='NotAllowedError'||nome==='SecurityError')msg='o acesso ao microfone foi negado no Chrome';
  else if(nome==='NotFoundError'||nome==='OverconstrainedError')msg='nenhum microfone encontrado';
  return {ok:false,erro:msg};
}
})()`

// sessaoCDP mantém uma conexão de Runtime.evaluate com uma aba.
type sessaoCDP struct {
	conn *websocket.Conn
	id   int
	mu   sync.Mutex
}

func novaSessaoCDP(ctx context.Context, wsURL string) (*sessaoCDP, error) {
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("conectar à aba do Chrome: %w", err)
	}
	return &sessaoCDP{conn: conn}, nil
}

// avaliar roda uma expressão na aba. Se awaitPromise, espera a promise
// resolver e devolve o valor dela.
func (s *sessaoCDP) avaliar(ctx context.Context, expr string, awaitPromise bool) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.id++
	params := map[string]any{"expression": expr, "returnByValue": true}
	if awaitPromise {
		params["awaitPromise"] = true
	}
	if err := s.conn.WriteJSON(map[string]any{"id": s.id, "method": "Runtime.evaluate", "params": params}); err != nil {
		return nil, fmt.Errorf("avaliar na aba: %w", err)
	}
	for {
		var msg struct {
			ID     int `json:"id"`
			Result struct {
				Result struct {
					Value json.RawMessage `json:"value"`
				} `json:"result"`
				ExceptionDetails *struct {
					Text string `json:"text"`
				} `json:"exceptionDetails"`
			} `json:"result"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := s.conn.ReadJSON(&msg); err != nil {
			return nil, fmt.Errorf("ler resposta da aba: %w", err)
		}
		if msg.ID != s.id {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("erro do CDP: %s", msg.Error.Message)
		}
		if msg.Result.ExceptionDetails != nil {
			return nil, fmt.Errorf("erro na página: %s", msg.Result.ExceptionDetails.Text)
		}
		return msg.Result.Result.Value, nil
	}
}

func (s *sessaoCDP) fechar() { _ = s.conn.Close() }

// IniciarVozChrome injeta o gravador na aba do Chrome do perfil do app e
// começa a bombear os lotes para o Whisper. O texto sai pelos eventos
// voz:lote / voz:fim / voz:erro. Termina por PararVozChrome ou pelo teto
// de 10 minutos do próprio gravador.
func (a *App) IniciarVozChrome() error {
	a.muVoz.Lock()
	defer a.muVoz.Unlock()
	if a.vozPumpAtivo {
		return fmt.Errorf("já existe uma gravação em andamento")
	}
	porta, ok := rota360.PortaCDPViva()
	if !ok {
		// O fluxo de conexão mata o Chrome do perfil depois de ler o cookie;
		// sem aba viva não há onde injetar o gravador — relança o perfil
		// (já logado) e deixa ele rodando para as próximas gravações.
		var errRelanca error
		porta, errRelanca = rota360.AbreChromeVoz()
		if errRelanca != nil {
			return fmt.Errorf("não consegui abrir o Chrome do Rota 360 para a voz: %w", errRelanca)
		}
	}
	rota360.LiberaMicrofone(porta)
	ws, err := rota360.AbaVoz(porta)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sess, err := novaSessaoCDP(ctx, ws)
	if err != nil {
		return err
	}
	raw, err := sess.avaliar(ctx, jsVozInjetar, true)
	if err != nil {
		sess.fechar()
		return fmt.Errorf("instalar o gravador no Chrome: %w", err)
	}
	var inicio struct {
		OK   bool   `json:"ok"`
		Erro string `json:"erro"`
	}
	if err := json.Unmarshal(raw, &inicio); err != nil || !inicio.OK {
		sess.fechar()
		if inicio.Erro != "" {
			return fmt.Errorf("%s", inicio.Erro)
		}
		return fmt.Errorf("resposta inesperada do gravador no Chrome")
	}

	parar := make(chan struct{})
	a.vozParar = parar
	a.vozPumpAtivo = true
	go a.bombeiaVozChrome(sess, parar)
	slog.Info("voz chrome iniciada", "porta", porta)
	return nil
}

// bombeiaVozChrome tira lotes de WAV da página, transcreve e emite o texto.
// A conexão da sessão é só dela: lotes saem por aqui; o fim sinalizado pelo
// gravador drena a fila e encerra.
func (a *App) bombeiaVozChrome(sess *sessaoCDP, parar chan struct{}) {
	defer func() {
		sess.fechar()
		a.muVoz.Lock()
		a.vozPumpAtivo = false
		a.muVoz.Unlock()
	}()
	var textos []string
	poll := time.NewTicker(350 * time.Millisecond)
	defer poll.Stop()
	fim := false
	for !fim {
		select {
		case <-parar:
			// usuário encerrou: sinaliza ao gravador e drena o resto
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = sess.avaliar(ctx, "window.__rsVozParar()", false)
			cancel()
		case <-poll.C:
		}
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			raw, err := sess.avaliar(ctx, "window.__rsVozLote()||''", false)
			cancel()
			if err != nil {
				slog.Warn("voz chrome", "err", err)
				fim = true
				break
			}
			var b64 string
			_ = json.Unmarshal(raw, &b64)
			if b64 == "" {
				break
			}
			wav, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				continue
			}
			texto, err := a.transcreverWAV(wav)
			if err != nil {
				slog.Warn("voz chrome: whisper", "err", err)
				continue
			}
			textos = append(textos, texto)
			wailsRuntime.EventsEmit(a.ctx, evVozLote, texto)
		}
		if fim {
			break
		}
		// o gravador terminou? (teto de 10 min ou PararVozChrome acima)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		raw, err := sess.avaliar(ctx, "!!window.__rsVoz&&window.__rsVoz.fim===true", false)
		cancel()
		if err == nil {
			_ = json.Unmarshal(raw, &fim)
		}
	}
	texto := ""
	for i, t := range textos {
		if i > 0 {
			texto += " "
		}
		texto += t
	}
	if texto != "" {
		wailsRuntime.EventsEmit(a.ctx, evVozFim, texto)
	} else {
		wailsRuntime.EventsEmit(a.ctx, evVozErro, "não captei fala nenhuma — fale mais perto do microfone")
	}
}

// PararVozChrome encerra a gravação na aba e faz o loop drenar os lotes
// restantes antes de emitir o texto final.
func (a *App) PararVozChrome() {
	a.muVoz.Lock()
	if !a.vozPumpAtivo {
		a.muVoz.Unlock()
		return
	}
	parar := a.vozParar
	a.muVoz.Unlock()
	if parar != nil {
		close(parar)
	}
}
