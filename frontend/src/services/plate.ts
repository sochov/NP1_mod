export type PlateReading={region:string;class:string;kana:string;number:string;confidence:number}
// estimated: inferred from the positions logged before and after (no fix logged at that second).
export type PlateLocation={at:string;lat:number;lon:number;accuracyM?:number;estimated?:boolean}
export function plateLocationLabel(r:PlateRecord){return !r.location?'位置なし':r.location.estimated?'位置推定':'位置あり'}
export function plateLocationNote(r:PlateRecord){const l=r.location;if(!l?.estimated)return undefined;return `前後の走行記録から推定した位置です${l.accuracyM?`（誤差 約${l.accuracyM>=1000?(l.accuracyM/1000).toFixed(0)+'km':Math.round(l.accuracyM)+'m'}）`:''}`}
export type PlateMatch={ruleId:string;ruleName:string;saveImage:boolean;speak:boolean}
export type PlateRecord={id:string;firstSeen:string;lastSeen:string;sightings:number;reading:PlateReading;display:string;location?:PlateLocation;box:[number,number,number,number];matches:PlateMatch[];imageId?:string;imageNote?:string;speech?:string}
export type NumberShareResult={status:'exact'|'candidate'|'none';category?:string;comment?:string;metadataPresent?:boolean;checkedAt:string}
export type PlateRecordsResponse={records:{record:PlateRecord;imageAvailable:boolean;numberShare?:NumberShareResult}[];truncated:boolean;reason:string}
export type PlateLED={mode:'off'|'solid'|'rainbow';effect:'steady'|'breathe'|'flow';rgb:[number,number,number];brightness:number}
export const defaultPlateLED=():PlateLED=>({mode:'off',effect:'breathe',rgb:[64,160,255],brightness:180})
export type PlateRule={id:string;name:string;enabled:boolean;region:string;class:string;kana:string;number:string;saveImage:boolean;speak:boolean;message:string;messageAudio:string;led:PlateLED}
export type PlateVoice={style:number;volume:number;phrase:string}
export type PlateSettings={revision:number;enabled:boolean;detectionLED?:boolean;speakAllDetections?:boolean;rules:PlateRule[];retentionDays:number;imageQuotaMB:number;mergeSeconds:number;minConfidence:number;minSightings:number;instantConfidence:number;voice:PlateVoice}
export type SpeechStyle={id:number;speaker:string;name:string}
export type PlateSpeech={available:boolean;reason:string;styles:SpeechStyle[];status:{keyConfigured:boolean;player?:string;job:{state:string;total:number;ready:number;error?:string};rules:Record<string,{ready:boolean;error?:string}>;style?:SpeechStyle;credit:string}}
export type VoicevoxConfig={endpoint:string;defaultEndpoint:string;keyConfigured:boolean;apiKey:string}
export type NumberSharePlate={serial_number:string;region?:string;classification_number?:string;hiragana?:string}
export type NumberShareLocation={latitude:number;longitude:number;accuracy_m?:number;observed_at?:string}
export type NumberShareQueueItem={eventId:string;recordId:string;payload:{event_id:string;observed_at:string;plate:NumberSharePlate;location?:NumberShareLocation};state:'pending'|'sent'|'blocked';attempts:number;queuedAt:string;lastAttemptAt?:string;nextAttemptAt?:string;sentAt?:string;httpStatus?:number;lastError?:string;matchStatus?:string}
export type NumberShareStatus={config:{revision:number;enabled:boolean;endpoint:string;keyConfigured:boolean;apiKey:string;managementUrl?:string;adminKeyConfigured:boolean;adminApiKey?:string;userHandle?:string};pending:number;sent:number;blocked:number;items:NumberShareQueueItem[]}
export type NumberShareSighting={id:number;event_id:string;observed_at:string;received_at:string;plate_id?:number;match_status:'exact'|'candidate'|'none';recognition_revision:number;plate:NumberSharePlate;location?:NumberShareLocation;mine:boolean}
export type NumberShareSightings={items:NumberShareSighting[];truncated:boolean;syncedAt:string}
export type NumberShareUpload={total:number;queued:number;requeued:number;existing:number;skipped:number}
export type NumberShareVehicle={id:number;serial_number:string;region:string;classification_number:string;hiragana:string;category:string;comment:string;sightings:number;last_seen?:string;last_location?:NumberShareLocation}
export type NumberShareVehicles={items:NumberShareVehicle[];total:number;categories:string[]}

// The spoken form of a rule, for the editor preview (mirrors platespeech.Plan).
export function speechPreview(rule:PlateRule,voice:PlateVoice){
 const lead=(rule.messageAudio?'（録音した音声）':rule.message)+voice.phrase
 const fill=(s:string)=>s.replace(/\*/g,'1')
 const parts=[rule.region,fill(rule.class),rule.kana]
 const n=fill(rule.number).replace(/^0+/,'')
 const plate=[...parts.filter(Boolean),n.length===4?n.slice(0,2)+'-'+n.slice(2):n].filter(Boolean).join(' ')
 return lead+(plate?' '+plate:'')
}
export function jobLabel(s:PlateSpeech['status']){
 const j=s.job
 if(!s.keyConfigured)return 'APIキー未設定'
 if(j.state==='generating')return `音声を生成中（${j.ready} / ${j.total}）`
 if(j.state==='ready')return `準備完了（${j.total}件）`
 if(j.state==='error'||j.state==='waiting')return j.error||'生成エラー'
 return '待機中'
}

// Browser-side conversion of an uploaded mp3/wav/m4a to the device clip
// format (24 kHz mono 16-bit WAV), so the device needs no decoder.
export async function toClipWav(file:File,maxSeconds=30):Promise<Blob>{
 const ctx=new AudioContext()
 let decoded:AudioBuffer
 try{decoded=await ctx.decodeAudioData(await file.arrayBuffer())}catch{throw new Error('音声ファイルを読み込めませんでした（mp3・wav などを選んでください）')}finally{void ctx.close()}
 if(decoded.duration>maxSeconds)throw new Error(`音声は${maxSeconds}秒以内にしてください`)
 const rate=24000,frames=Math.max(1,Math.ceil(decoded.duration*rate))
 const offline=new OfflineAudioContext(1,frames,rate)
 const src=offline.createBufferSource();src.buffer=decoded;src.connect(offline.destination);src.start()
 const mono=(await offline.startRendering()).getChannelData(0)
 const out=new DataView(new ArrayBuffer(44+mono.length*2))
 const text=(o:number,s:string)=>{for(let i=0;i<s.length;i++)out.setUint8(o+i,s.charCodeAt(i))}
 text(0,'RIFF');out.setUint32(4,36+mono.length*2,true);text(8,'WAVEfmt ');out.setUint32(16,16,true);out.setUint16(20,1,true);out.setUint16(22,1,true);out.setUint32(24,rate,true);out.setUint32(28,rate*2,true);out.setUint16(32,2,true);out.setUint16(34,16,true);text(36,'data');out.setUint32(40,mono.length*2,true)
 for(let i=0;i<mono.length;i++){const v=Math.max(-1,Math.min(1,mono[i]));out.setInt16(44+i*2,v<0?v*0x8000:v*0x7fff,true)}
 return new Blob([out.buffer],{type:'audio/wav'})
}
export type PlateSensor={state:string;inputSource?:string;reason?:string;instance?:string;engine?:string;backend?:string;fps?:number;lastFrameAt?:string}
export type PlateStatus={enabled:boolean;revision:number;sensor:PlateSensor;collector:{lastPollAt?:string|null;error:string};speech:{available:boolean;reason:string};usage:{recordBytes:number;recordDays:number;imageBytes:number;imageCount:number;openRecords:number};imageQuotaBytes:number}

// Mirrors plate.Reading.Display on the server for records written by older builds.
// Fields the reader left blank (not read with enough confidence); the list shows them greyed.
export function plateMissing(r:PlateRecord){return !r.reading.region||!r.reading.class||!r.reading.kana}
// Serial as printed on a Japanese plate: four positions, unused leading ones shown as "・",
// and a hyphen between the pairs only when all four are digits ("15-89", "・8 10", "・・ 12").
export function plateSerial(number:string){const d=(number||'').replace(/\D/g,'').slice(-4);if(!d)return '';const s=d.padStart(4,'・');return d.length===4?`${s.slice(0,2)}-${s.slice(2)}`:`${s.slice(0,2)} ${s.slice(2)}`}
export function plateDisplay(r:PlateRecord){return r.display||[r.reading.region,r.reading.class,r.reading.kana,r.reading.number].filter(Boolean).join(' ')}

const sensorLabels:Record<string,string>={running:'認識中',starting:'起動中',idle:'待機中（記録停止）','camera-waiting':'カメラ画像待ち','model-missing':'認識モデル未導入','engine-missing':'認識エンジン未導入',error:'認識エラー',unavailable:'認識プロセス未起動'}
export function sensorLabel(state:string){return sensorLabels[state]||'状態不明'}

const speechLabels:Record<string,string>={spoken:'発話済み',requested:'発話要求中',failed:'発話失敗',unsupported:'発話できず（未設定）'}
export function speechLabel(state:string){return speechLabels[state]||state}

export function collectorLabel(s:PlateStatus){
 if(!s.enabled)return '記録が停止中のため取り込んでいません'
 if(s.collector.error==='sensor-unavailable')return '認識プロセスに接続できません'
 if(s.collector.error==='storage-unavailable')return '記録を保存できませんでした'
 if(s.collector.error==='invalid-sensor-cursor')return '認識プロセスの応答が不正です'
 if(!s.collector.lastPollAt)return 'まだ取り込んでいません'
 return '正常（'+new Date(s.collector.lastPollAt).toLocaleTimeString('ja-JP',{timeZone:'Asia/Tokyo'})+'）'
}

export function newRuleId(){const bytes=crypto.getRandomValues(new Uint8Array(5));return 'r'+Array.from(bytes,b=>b.toString(16).padStart(2,'0')).join('')}

// YYYY-MM-DD in Japan time, offset by whole days from today.
export function jstDay(offset:number){return new Intl.DateTimeFormat('sv-SE',{timeZone:'Asia/Tokyo'}).format(new Date(Date.now()+offset*86400000))}
