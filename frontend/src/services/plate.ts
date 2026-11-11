export type PlateReading={region:string;class:string;kana:string;number:string;confidence:number}
export type PlateLocation={at:string;lat:number;lon:number;accuracyM?:number}
export type PlateMatch={ruleId:string;ruleName:string;saveImage:boolean;speak:boolean}
export type PlateRecord={id:string;firstSeen:string;lastSeen:string;sightings:number;reading:PlateReading;display:string;location?:PlateLocation;box:[number,number,number,number];matches:PlateMatch[];imageId?:string;imageNote?:string;speech?:string}
export type PlateRecordsResponse={records:{record:PlateRecord;imageAvailable:boolean}[];truncated:boolean;reason:string}
export type PlateRule={id:string;name:string;enabled:boolean;region:string;class:string;kana:string;number:string;saveImage:boolean;speak:boolean}
export type PlateSettings={revision:number;enabled:boolean;rules:PlateRule[];retentionDays:number;imageQuotaMB:number;mergeSeconds:number;minConfidence:number}
export type PlateSensor={state:string;reason?:string;instance?:string;engine?:string;backend?:string;fps?:number;lastFrameAt?:string}
export type PlateStatus={enabled:boolean;revision:number;sensor:PlateSensor;collector:{lastPollAt?:string|null;error:string};speech:{available:boolean;reason:string};usage:{recordBytes:number;recordDays:number;imageBytes:number;imageCount:number;openRecords:number};imageQuotaBytes:number}

// Mirrors plate.Reading.Display on the server for records written by older builds.
export function plateDisplay(r:PlateRecord){return r.display||[r.reading.region,r.reading.class,r.reading.kana,r.reading.number].filter(Boolean).join(' ')}

const sensorLabels:Record<string,string>={running:'認識中',starting:'起動中',idle:'待機中（記録停止）','model-missing':'認識モデル未導入','engine-missing':'認識エンジン未導入',error:'認識エラー',unavailable:'認識プロセス未起動'}
export function sensorLabel(state:string){return sensorLabels[state]||'状態不明'}

const speechLabels:Record<string,string>={spoken:'発話済み',requested:'発話要求中',failed:'発話失敗',unsupported:'発話未対応'}
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
