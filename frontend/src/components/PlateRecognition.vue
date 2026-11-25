<script setup lang="ts">
import {computed,nextTick,onMounted,onUnmounted,ref,watch} from 'vue'
import {VectorMap,collection,feature,routeFeatures,routeLayers,type LatLon} from '../services/vector-map'
import {recordedTrailBetween} from '../services/trail'
import type {TrackPoint} from '../services/playbackTrack'
import {AlertTriangle,Camera,Image as ImageIcon,MapPin,Play,Plus,RefreshCw,ScanLine,Trash2,Upload,Volume2,X} from '@lucide/vue'
import {api,apiUpload,bytes} from '../services/api'
import {plateDisplay,plateSerial,plateMissing,plateLocationLabel,plateLocationNote,sensorLabel,speechLabel,collectorLabel,newRuleId,jstDay,speechPreview,jobLabel,toClipWav,defaultPlateLED,type PlateRecord,type PlateRecordsResponse,type PlateRule,type PlateSettings,type PlateSpeech,type PlateStatus} from '../services/plate'
import PlateClipDialog from './PlateClipDialog.vue'
import PlateShare from './PlateShare.vue'
import '../plate-recognition.css'
import '../radio-map.css'

const tab=ref<'records'|'share'|'rules'|'status'>('records')
const status=ref<PlateStatus>(),settings=ref<PlateSettings>(),draft=ref<PlateSettings>()
const records=ref<PlateRecordsResponse['records']>([]),truncated=ref(false),listReason=ref(''),listError=ref('')
const loading=ref(false),saving=ref(false),error=ref(''),notice=ref('')
const from=ref(jstDay(-6)),to=ref(jstDay(0)),matchedOnly=ref(false),query=ref(''),online=ref(true)
const selectedId=ref(''),imageFor=ref<PlateRecord>(),clipFor=ref<PlateRecord>()
const selectedRecord=computed(()=>records.value.find(x=>x.record.id===selectedId.value)?.record)
const trail=ref<TrackPoint[]>([]),trailPoint=ref<TrackPoint>(),trailMessage=ref('')
let trailRequest:AbortController|undefined
const speech=ref<PlateSpeech>(),uploading=ref(''),previewing=ref(''),listen=ref('')
const tileFailed=ref(false)
const mapEl=ref<HTMLElement>(),listEl=ref<HTMLElement>()
let map:VectorMap|undefined,fitted=false,sequence=0,fetchingRecords=false,statusTimer:ReturnType<typeof setInterval>|undefined,recordTimer:ReturnType<typeof setInterval>|undefined

const placed=computed(()=>records.value.filter(r=>r.record.location))
const mappedCount=computed(()=>placed.value.length+(selectedRecord.value&&!selectedRecord.value.location&&(trail.value.length||trailPoint.value)?1:0))
const recordDay=new Intl.DateTimeFormat('sv-SE',{timeZone:'Asia/Tokyo',year:'numeric',month:'2-digit',day:'2-digit'})
const recordDayLabel=new Intl.DateTimeFormat('ja-JP',{timeZone:'Asia/Tokyo',year:'numeric',month:'long',day:'numeric',weekday:'short'})
const plateImageURL=(id:string)=>`/api/v1/plate/records/${encodeURIComponent(id)}/image`
function plateThumbnail(record:PlateRecord){
 const [x,y,w,h]=record.box
 const [left,top]=[Math.max(0,x),Math.max(0,y)]
 const [width,height]=[Math.min(1920,x+w)-left,Math.min(1080,y+h)-top]
 if(!(width>0&&height>0))return
 return {aspectRatio:`${width} / ${height}`,image:{left:`${-100*left/width}%`,top:`${-100*top/height}%`,width:`${192000/width}%`,height:`${108000/height}%`}}
}
const recordGroups=computed(()=>{
 const days=new Map<string,{day:string;label:string;items:(PlateRecordsResponse['records'][number]&{thumbnail?:ReturnType<typeof plateThumbnail>})[]}>()
 for(const item of records.value){
  const date=new Date(item.record.firstSeen),day=recordDay.format(date)
  if(!days.has(day))days.set(day,{day,label:recordDayLabel.format(date),items:[]})
  days.get(day)!.items.push({...item,thumbnail:item.imageAvailable?plateThumbnail(item.record):undefined})
 }
 return [...days.values()].sort((a,b)=>b.day.localeCompare(a.day))
})
const matchedCount=computed(()=>records.value.filter(r=>r.record.matches.length).length)
const dirty=computed(()=>JSON.stringify(draft.value)!==JSON.stringify(settings.value))
const time=(v:string)=>new Date(v).toLocaleString('ja-JP',{timeZone:'Asia/Tokyo',month:'numeric',day:'numeric',hour:'2-digit',minute:'2-digit',second:'2-digit'})
const endTime=(r:PlateRecord)=>recordDay.format(new Date(r.firstSeen))===recordDay.format(new Date(r.lastSeen))
 ? new Date(r.lastSeen).toLocaleTimeString('ja-JP',{timeZone:'Asia/Tokyo',hour:'2-digit',minute:'2-digit',second:'2-digit'}) : time(r.lastSeen)
const message=(e:unknown)=>e instanceof Error?e.message:String(e)
// Reactive proxies cannot be structured-cloned; settings are plain JSON.
const clone=<T,>(value:T):T=>JSON.parse(JSON.stringify(value))

async function loadStatus(){try{status.value=await api<PlateStatus>('/plate/status')}catch(e){error.value=message(e)}}
async function loadSpeech(){try{speech.value=await api<PlateSpeech>('/plate/speech')}catch{/* shown via status */}}
const speaking=computed(()=>(settings.value?.rules||[]).some(r=>r.speak)||(draft.value?.rules||[]).some(r=>r.speak))
const needsAPI=computed(()=>speech.value&&!speech.value.status.keyConfigured&&(tab.value==='rules'||speaking.value))
const styleOptions=computed(()=>{const list=[...(speech.value?.styles||[])];const id=draft.value?.voice.style;if(id!==undefined&&!list.some(s=>s.id===id))list.unshift({id,speaker:id===1?'ずんだもん':'スタイル',name:id===1?'あまあま':String(id)});return list})
function savedRule(rule:PlateRule){return settings.value?.rules.find(r=>r.id===rule.id)}
function canPreview(rule:PlateRule){const saved=savedRule(rule);return Boolean(saved&&saved.speak&&JSON.stringify(saved)===JSON.stringify(rule)&&JSON.stringify(settings.value?.voice)===JSON.stringify(draft.value?.voice)&&speech.value?.available)}
function canTestLED(rule:PlateRule){const saved=savedRule(rule);return Boolean(saved&&rule.led.mode!=='off'&&JSON.stringify(saved)===JSON.stringify(rule)&&!rule.speak)}
const ledColor=(rule:PlateRule)=>'#'+rule.led.rgb.map(v=>v.toString(16).padStart(2,'0')).join('')
function setLEDColor(rule:PlateRule,event:Event){const s=(event.target as HTMLInputElement).value;rule.led.rgb=[1,3,5].map(i=>parseInt(s.slice(i,i+2),16)) as [number,number,number]}
function clipLabel(rule:PlateRule){const saved=savedRule(rule);if(!saved||JSON.stringify(saved)!==JSON.stringify(rule))return '保存すると音声を生成します';const c=speech.value?.status.rules[rule.id];if(!c)return '';if(c.error)return c.error;if(c.ready)return '音声を準備済み';return speech.value?.status.keyConfigured?'音声を生成中…':'APIキーを設定すると生成します'}
async function upload(rule:PlateRule,event:Event){const input=event.target as HTMLInputElement;const file=input.files?.[0];input.value='';if(!file)return;uploading.value=rule.id;error.value='';try{const wav=await toClipWav(file);const r=await apiUpload<{id:string;durationMs:number}>('/plate/speech/uploads',wav,'audio/wav');rule.messageAudio=r.id;notice.value=`音声を読み込みました（${(r.durationMs/1000).toFixed(1)}秒）。設定を保存すると使われます`}catch(e){error.value=message(e)}finally{uploading.value=''}}
async function preview(rule:PlateRule){previewing.value=rule.id;error.value='';try{await api('/plate/speech/preview','POST',{ruleId:rule.id});notice.value=rule.speak?'本体のスピーカーで再生しました':'LED通知を開始しました'}catch(e){error.value=message(e)}finally{previewing.value=''}}
const normalizeSettings=(value:PlateSettings)=>({...value,rules:(value.rules??[]).map(rule=>({...rule,led:{...defaultPlateLED(),...rule.led}}))})
async function loadSettings(){const s=normalizeSettings(await api<PlateSettings>('/plate/settings'));settings.value=s;draft.value=clone(s)}
// Errors of the record list stay in the list panel and clear on the next good load.
const rangeDays=()=>Math.round((Date.parse(to.value)-Date.parse(from.value))/86400000)
async function loadRecords(background=false){
 if(background&&fetchingRecords)return
 const id=++sequence
 if(!from.value||!to.value||rangeDays()<0||rangeDays()>366){
  listError.value='期間は366日以内で、開始日を終了日以前にしてください'
  fetchingRecords=loading.value=false
  return
 }
 fetchingRecords=true
 if(!background){listReason.value='';loading.value=true}
 try{
  const q=new URLSearchParams({from:from.value,to:to.value})
  if(matchedOnly.value)q.set('matched','true')
  if(query.value.trim())q.set('q',query.value.trim())
  const r=await api<PlateRecordsResponse>('/plate/records?'+q)
  if(id!==sequence)return
  records.value=r.records;truncated.value=r.truncated;listReason.value=r.reason;listError.value=''
  if(!r.records.some(x=>x.record.id===selectedId.value))selectedId.value=''
  if(!background){
   if(selectedRecord.value&&!trail.value.length&&!trailRequest)void loadTrail()
   fitted=false
  }
  render()
 }catch(e){if(!background&&id===sequence){records.value=[];listError.value=message(e);render()}}
 finally{if(id===sequence){fetchingRecords=false;loading.value=false}}
}

function popupNode(r:PlateRecord){const node=document.createElement('div');node.className='plate-popup';const b=document.createElement('strong');b.textContent=plateDisplay(r);node.appendChild(b);const t=document.createElement('div');t.textContent=time(r.firstSeen);node.appendChild(t);const note=plateLocationNote(r);if(note){const e=document.createElement('div');e.className='plate-popup-estimated';e.textContent=note;node.appendChild(e)}if(r.matches.length){const m=document.createElement('div');m.className='plate-popup-hit';m.textContent='条件: '+r.matches.map(x=>x.ruleName).join('、');node.appendChild(m)}return node}
function render(){
 if(!map)return
 const record=selectedRecord.value,hasTrail=trail.value.length>0
 const pins=records.value.flatMap(({record})=>{
  const selected=record.id===selectedId.value
  const fix=selected?trailPoint.value:undefined
  const loc=fix??record.location
  if(!loc||(selected&&hasTrail))return []
  return [{record,loc,selected,estimated:!fix&&!!record.location?.estimated}]
 })
 const points=pins.map(({record,loc,selected,estimated})=>feature({type:'Point',coordinates:[loc.lon,loc.lat]},{id:record.id,hit:record.matches.length>0,selected,estimated}))
 const layer=(hit:boolean)=>({id:hit?'plate-hit':'plate-points',type:'circle' as const,source:'plates',filter:['==',['get','hit'],hit] as any,paint:{'circle-radius':['case',['get','selected'],hit?13:9,hit?9:5] as any,'circle-color':hit?'#e8590c':'#397fd7','circle-opacity':['case',['get','estimated'],.35,.85] as any,'circle-stroke-color':hit?'#8f2d14':'#f8fafc','circle-stroke-width':['case',['get','selected'],3,1.5] as any}})
 map.setOverlay('plates',collection(points),[layer(false),layer(true)])
 const ends=collection()
 if(record&&hasTrail){
  for(const [point,at,start] of [[trail.value[0]!,record.firstSeen,true],[trail.value.at(-1)!,record.lastSeen,false]] as const){
   if(Date.parse(point.at)===Date.parse(at))ends.features.push(feature({type:'Point',coordinates:[point.lon,point.lat]},{start}))
  }
 }
 map.setOverlay('plate-trail-ends',ends,[
  {id:'plate-trail-endpoints',type:'circle',source:'plate-trail-ends',paint:{'circle-radius':6,'circle-color':['case',['get','start'],'#fff','#008b80'],'circle-stroke-color':'#00635c','circle-stroke-width':1}}])
 // Install the line above the circular markers, so a short route stays visible.
 map.setOverlay('plate-trail',routeFeatures(trail.value),routeLayers('plate-trail','#008b80',5))
 const bounds:LatLon[]=hasTrail?trail.value.map(p=>[p.lat,p.lon]):pins.map(({loc})=>[loc.lat,loc.lon])
 if(bounds.length&&!fitted){map.fit(bounds,hasTrail?20:16);fitted=true}
 renderTiles()
}
async function loadTrail(){
 trailRequest?.abort();trailRequest=undefined;trail.value=[];trailPoint.value=undefined;trailMessage.value=''
 const record=selectedRecord.value
 if(!record||tab.value!=='records'){map?.closePopup();render();return}
 render()
 const first=Date.parse(record.firstSeen),last=Date.parse(record.lastSeen)
 if(record.sightings<=1||!(last>first))return
 const request=new AbortController();trailRequest=request;trailMessage.value='検出区間の走行軌跡を読み込み中…'
 try{
  const {points,point}=await recordedTrailBetween(first,last,request.signal)
  if(request.signal.aborted)return
  trail.value=points;trailPoint.value=points.length?undefined:point
  trailMessage.value=points.length?'':point||record.location?'検出地点を表示しています':'検出区間の位置記録がありません'
  render()
  if(points.length&&map){map.closePopup();map.fit(points.map(p=>[p.lat,p.lon]),20)}
  else if(point&&map){const position:LatLon=[point.lat,point.lon];map.centre(position);map.popup(position,popupNode({...record,location:point}))}
 }catch{if(!request.signal.aborted)trailMessage.value='検出区間の走行軌跡を取得できません'}
 finally{if(trailRequest===request)trailRequest=undefined}
}
async function select(id:string,fromList:boolean){if(id===selectedId.value&&!trail.value.length&&!trailRequest)void loadTrail();selectedId.value=id;render();const record=selectedRecord.value,loc=trailPoint.value??record?.location;if(!trail.value.length&&record&&loc&&map){const point:LatLon=[loc.lat,loc.lon];if(fromList)map.centre(point);map.popup(point,popupNode({...record,location:loc}))}else map?.closePopup();if(!fromList){await nextTick();listEl.value?.querySelector<HTMLElement>(`[data-record="${id}"]`)?.scrollIntoView({block:'nearest',behavior:'smooth'})}}
function renderTiles(){if(map){const hasLocation=!!(placed.value.length||trail.value.length||trailPoint.value);map.background(online.value&&hasLocation);if(!online.value||!hasLocation)tileFailed.value=false}}
function initMap(){if(map||!mapEl.value)return;try{map=new VectorMap({view:'plates',container:mapEl.value,failed:()=>tileFailed.value=true});map.map.on('click',event=>{const hit=map?.features(event,['plate-hit','plate-points'])[0];if(hit)void select(String(hit.properties.id),false)});render()}catch{tileFailed.value=true}}
function destroyMap(){map?.destroy();map=undefined}

function addRule(){draft.value?.rules.push({id:newRuleId(),name:'',enabled:true,region:'',class:'',kana:'',number:'',saveImage:true,speak:false,message:'',messageAudio:'',led:defaultPlateLED()})}
function removeRule(rule:PlateRule){if(!draft.value)return;draft.value.rules=draft.value.rules.filter(r=>r!==rule)}
function resetDraft(){draft.value=clone(settings.value)}
async function save(){if(!draft.value||!settings.value)return;saving.value=true;error.value='';try{const next=await api<PlateSettings>('/plate/settings','PUT',{expectedRevision:settings.value.revision,settings:draft.value});settings.value=next;draft.value=clone(next);notice.value='設定を保存しました';await Promise.all([loadStatus(),loadSpeech()])}catch(e){error.value=message(e)}finally{saving.value=false}}
async function reloadSettings(){error.value='';try{await loadSettings()}catch(e){error.value=message(e)}}

watch([from,to,matchedOnly],()=>void loadRecords())
let searchTimer:ReturnType<typeof setTimeout>|undefined
watch(query,()=>{if(searchTimer)clearTimeout(searchTimer);searchTimer=setTimeout(loadRecords,350)})
watch(online,renderTiles)
watch(()=>{const r=selectedRecord.value;return r&&tab.value==='records'?`${r.id}/${r.firstSeen}/${r.lastSeen}`:''},loadTrail,{flush:'sync'})
watch(tab,async value=>{if(value==='records'){await nextTick();initMap()}else destroyMap()})
watch(notice,v=>{if(v)setTimeout(()=>notice.value='',4000)})
onMounted(async()=>{await Promise.all([loadStatus(),reloadSettings(),loadRecords(),loadSpeech()]);initMap();statusTimer=setInterval(()=>{void loadStatus();void loadSpeech()},10000)
 recordTimer=setInterval(()=>{const today=jstDay(0);if(tab.value==='records'&&!document.hidden&&from.value<=today&&today<=to.value)void loadRecords(true)},5000)
})
onUnmounted(()=>{sequence++;trailRequest?.abort();if(statusTimer)clearInterval(statusTimer);if(recordTimer)clearInterval(recordTimer);destroyMap()})
</script>

<template>
 <div class="plate-page">
  <section class="panel plate-summary">
   <div class="plate-summary-main"><ScanLine :size="22"/><div><h2>ナンバー</h2><p>前方カメラで読み取ったナンバーと場所を記録し、条件に合う車両を目立たせます。</p></div></div>
   <dl class="plate-chips" v-if="status">
    <div><dt>記録</dt><dd :class="status.enabled?'on':'off'">{{status.enabled?'有効':'停止中'}}</dd></div>
    <div><dt>認識</dt><dd :class="status.sensor.state==='running'?'on':'warn'">{{sensorLabel(status.sensor.state)}}</dd></div>
    <div><dt>発話</dt><dd :class="status.speech.available?'on':'warn'">{{status.speech.available?'利用可能':'未設定'}}</dd></div>
   </dl>
  </section>
  <div v-if="error" class="banner error" role="alert"><AlertTriangle :size="16"/><span>{{error}}</span><button class="icon-button" aria-label="閉じる" @click="error=''"><X :size="15"/></button></div>
  <div v-if="notice" class="banner success" role="status">{{notice}}</div>
  <div v-if="needsAPI" class="banner neutral plate-api-note" role="note"><Volume2 :size="16"/><span>発話には VOICEVOX API の設定が必要です。</span><a class="text-button" href="#system/led">システム › LED / 音声 で設定する</a></div>
  <div v-if="status&&status.sensor.state!=='running'" class="banner neutral plate-sensor-note"><Camera :size="16"/><span>{{status.sensor.reason||sensorLabel(status.sensor.state)}}。認識が動作するまで新しい記録は増えません。</span></div>

  <nav class="plate-tabs" role="tablist" aria-label="ナンバー">
   <button v-for="t in [{id:'records',label:'記録'},{id:'share',label:'共有'},{id:'rules',label:'条件と設定'},{id:'status',label:'状態'}] as const" :key="t.id" role="tab" :aria-selected="tab===t.id" :class="{active:tab===t.id}" @click="tab=t.id">{{t.label}}</button>
  </nav>

  <section v-if="tab==='records'" class="panel plate-records" aria-label="認識したナンバーの記録">
   <div class="plate-toolbar">
    <label>開始日<input v-model="from" type="date" :max="to"></label>
    <label>終了日<input v-model="to" type="date" :min="from"></label>
    <label class="plate-search">ナンバーで検索<input v-model="query" type="search" placeholder="例: 12-34、品川、わ" maxlength="20"></label>
    <label class="plate-check"><input v-model="matchedOnly" type="checkbox">条件に合ったものだけ</label>
    <label class="plate-check"><input v-model="online" type="checkbox">地図の背景を表示</label>
    <button class="secondary compact" :disabled="loading" @click="loadRecords()"><RefreshCw :size="14"/>更新</button>
   </div>
   <p v-if="listError" class="plate-list-error" role="alert">{{listError}}</p>
   <p class="plate-count" role="status">{{loading?'読み込み中…':`${records.length.toLocaleString()} 件（条件に合致 ${matchedCount.toLocaleString()} 件 · 地図に表示 ${mappedCount.toLocaleString()} 件）`}}<span v-if="truncated"> · 新しい順に上限まで表示しています</span><span v-if="listReason"> · {{listReason}}</span></p>
   <div class="plate-split">
    <div class="plate-map-wrap"><div class="plate-map-frame"><div ref="mapEl" class="plate-map" role="region" aria-label="記録した場所の地図"></div><div v-if="!loading&&!placed.length&&!trail.length&&!trailPoint" class="plate-map-placeholder"><MapPin :size="24"/><strong>地図に表示できる記録はありません</strong><span>{{records.length?'位置情報付きの記録を選ぶか、一覧から走行軌跡を確認できます。':'位置のある記録を取得すると、ここに表示します。'}}</span></div></div><div class="plate-legend"><span><i class="dot"></i>記録</span><span><i class="dot hit"></i>条件に合致</span><span v-if="trail.length" title="白丸が開始、緑の丸が終了"><i class="trail"></i>選択した記録の検出区間</span></div><p v-if="trailMessage" class="plate-map-empty" role="status">{{trailMessage}}</p><p v-if="tileFailed" class="plate-map-empty" role="note">背景地図を読み込めませんでした。記録の位置は引き続き表示します。</p></div>
    <div ref="listEl" class="plate-list">
     <p v-if="!loading&&!records.length" class="plate-empty">この期間の記録はありません。</p>
     <template v-for="group in recordGroups" :key="group.day">
     <h3 class="plate-day-heading"><time :datetime="group.day">{{group.label}}</time><span>{{group.items.length.toLocaleString()}} 件</span></h3>
     <article v-for="{record,imageAvailable,numberShare,thumbnail} in group.items" :key="record.id" :data-record="record.id" :class="['plate-item',{hit:record.matches.length,'shared-danger':numberShare?.status==='exact'&&numberShare.category==='危険',selected:record.id===selectedId}]">
      <!-- The plate is drawn like a Japanese number plate; details and actions sit to its right. -->
      <div class="plate-item-visual">
      <button class="plate-item-main" :aria-pressed="record.id===selectedId" :aria-label="plateDisplay(record)" @click="select(record.id,true)">
       <span class="plate-badge" :class="{hit:record.matches.length}" :title="plateMissing(record)?'グレーの部分は読み取れなかった項目です':undefined" aria-hidden="true">
        <span class="plate-top"><span :class="{missing:!record.reading.region}">{{record.reading.region||'？？'}}</span> <span :class="{missing:!record.reading.class}">{{record.reading.class||'？？？'}}</span></span>
        <span class="plate-kana" :class="{missing:!record.reading.kana}">{{record.reading.kana||'？'}}</span>
        <span class="plate-serial"><span v-for="(character,index) in plateSerial(record.reading.number)" :key="index">{{character}}</span></span>
       </span>
      </button>
       <button v-if="thumbnail" class="plate-thumbnail" :style="{aspectRatio:thumbnail.aspectRatio}" :aria-label="`${plateDisplay(record)} の切り抜き画像を開く`" @click="imageFor=record">
        <img :src="plateImageURL(record.id)" :style="thumbnail.image" alt="" loading="lazy" decoding="async" @error="($event.target as HTMLImageElement).hidden=true"/>
       </button>
      </div>
      <div class="plate-item-info">
       <span class="plate-meta" @click="select(record.id,true)"><span><time :datetime="record.firstSeen">{{time(record.firstSeen)}}</time><template v-if="time(record.firstSeen)!==time(record.lastSeen)">〜<time :datetime="record.lastSeen">{{endTime(record)}}</time></template></span><span>{{record.sightings}}回検出</span><span>精度 {{Math.round(record.reading.confidence*100)}}%</span><span :class="{'plate-nofix':!record.location,'plate-estimated':record.location?.estimated}" :title="plateLocationNote(record)">{{plateLocationLabel(record)}}</span></span>
      <div class="plate-item-extra">
       <span v-for="m in record.matches" :key="m.ruleId" class="plate-rule-tag">{{m.ruleName}}</span>
       <template v-if="numberShare?.status==='exact'">
        <span class="plate-shared-tag" :class="{danger:numberShare.category==='危険'}">NumberShare: {{numberShare.category||'登録済み'}}</span>
        <span v-if="numberShare.comment" class="plate-shared-comment">{{numberShare.comment}}</span>
       </template>
       <button class="text-button" :aria-label="`${plateDisplay(record)} の録画を再生`" @click="clipFor=record"><Play :size="14"/>録画を再生</button>
       <button v-if="imageAvailable" class="text-button" @click="imageFor=record"><ImageIcon :size="14"/>画像を見る</button>
       <span v-else-if="record.imageId" class="muted plate-small">画像は保存期間・容量上限により削除済み</span>
       <span v-else-if="record.imageNote" class="muted plate-small">{{record.imageNote}}</span>
       <span v-if="record.speech" class="plate-speech" :class="record.speech"><Volume2 :size="13"/>{{speechLabel(record.speech)}}</span>
      </div>
      </div>
     </article>
     </template>
    </div>
   </div>
  </section>

  <PlateShare v-else-if="tab==='share'"/>
  <section v-else-if="tab==='rules'&&draft" class="plate-rules" aria-label="条件と設定">
   <div class="panel plate-settings">
    <label class="plate-switch"><input v-model="draft.enabled" type="checkbox"><span><strong>ナンバーの記録を有効にする</strong><small>オフの間は、認識結果を記録しません。</small></span></label>
    <label class="plate-switch"><input v-model="draft.detectionLED" type="checkbox" :disabled="!draft.enabled"><span><strong>条件に関係なく、ナンバー検出時にLEDを光らせる</strong><small>新たに記録した車両ごとに白く光ります。条件ごとに設定した点灯が優先されます。</small></span></label>
    <div class="plate-grid">
     <label>記録の保存日数<input v-model.number="draft.retentionDays" type="number" min="1" max="3650" inputmode="numeric"></label>
     <label>画像の保存容量（MB）<input v-model.number="draft.imageQuotaMB" type="number" min="16" max="1024" inputmode="numeric"></label>
     <label>記録する信頼度の下限<input v-model.number="draft.minConfidence" type="number" min="0.3" max="0.99" step="0.05" inputmode="decimal"></label>
     <label>記録に必要な検出回数<input v-model.number="draft.minSightings" type="number" min="1" max="5" inputmode="numeric"></label>
     <label title="1回の読みで記録するための信頼度です。写り幅50px以上では0.75、45〜49pxでは0.80が下限です。1で無効。">1回で記録する信頼度<input v-model.number="draft.instantConfidence" type="number" min="0.6" max="1" step="0.05" inputmode="decimal"></label>
    </div>
    <p class="hint">同じ車両として指定回数以上読めたものだけを記録します（1回だけの誤読を除くため、既定は2回）。保存日数や容量を超えた記録・画像は古いものから削除されます。記録と画像はこの端末内だけに保存されます。</p>
   </div>
   <div class="panel plate-voice">
    <div class="panel-head"><h2>発話</h2><span class="muted plate-small" v-if="speech">{{jobLabel(speech.status)}}</span></div>
    <div class="plate-grid">
     <label>声<select v-model.number="draft.voice.style"><option v-for="s in styleOptions" :key="s.id" :value="s.id">{{s.speaker}}（{{s.name}}）</option></select></label>
     <label>音量（%）<input v-model.number="draft.voice.volume" type="range" min="10" max="100" step="5"><span class="plate-small muted">{{draft.voice.volume}}%</span></label>
     <label>定型文<input v-model="draft.voice.phrase" maxlength="40" placeholder="番号を検知しました。"></label>
    </div>
    <p class="hint">条件に合ったナンバーを「メッセージ → 定型文 → ナンバー」の順に本体のスピーカーで読み上げます。音声は条件を保存したときに VOICEVOX API で生成して本体に保存し、検知したときは保存済みの音声を再生します（他の車のナンバーは送信しません）。声を変えると読み上げ用の音声を作り直します。<template v-if="speech"> 音声: {{speech.status.credit}}</template></p>
   </div>
   <div class="panel">
    <div class="panel-head"><h2>条件（{{draft.rules.length}} / 100）</h2><button class="secondary compact" :disabled="draft.rules.length>=100" @click="addRule"><Plus :size="14"/>条件を追加</button></div>
    <p class="plate-rules-help">指定した項目がすべて一致したナンバーを「条件に合致」として扱います。空欄の項目は問いません。<code>*</code> は任意の1文字です（例: 番号 <code>12**</code>、分類番号 <code>3**</code>）。</p>
    <p v-if="!draft.rules.length" class="plate-empty">条件はまだありません。</p>
    <fieldset v-for="(rule,i) in draft.rules" :key="rule.id" class="plate-rule">
     <legend>条件 {{i+1}}</legend>
     <div class="plate-rule-head"><label class="plate-rule-name">名前<input v-model="rule.name" maxlength="40" placeholder="例: 家族の車"></label><label class="plate-check"><input v-model="rule.enabled" type="checkbox">有効</label><button class="icon-button" :aria-label="`条件${i+1}を削除`" @click="removeRule(rule)"><Trash2 :size="15"/></button></div>
     <div class="plate-rule-fields">
      <label>地名<input v-model="rule.region" maxlength="8" placeholder="品川"></label>
      <label>分類番号<input v-model="rule.class" maxlength="3" placeholder="300" autocapitalize="characters"></label>
      <label>ひらがな<input v-model="rule.kana" maxlength="1" placeholder="あ"></label>
      <label>番号<input v-model="rule.number" maxlength="4" placeholder="1234" inputmode="numeric"></label>
     </div>
     <div class="plate-rule-actions"><label class="plate-check"><input v-model="rule.saveImage" type="checkbox">画像を保存する</label><label class="plate-check"><input v-model="rule.speak" type="checkbox">本体で発話する</label></div>
     <div class="plate-led-edit">
      <label>LED通知<select v-model="rule.led.mode"><option value="off">なし</option><option value="solid">単色</option><option value="rainbow">レインボー</option></select></label>
      <template v-if="rule.led.mode!=='off'">
       <label v-if="rule.led.mode==='solid'">色<input type="color" :value="ledColor(rule)" @input="setLEDColor(rule,$event)"></label>
       <label v-if="rule.led.mode==='solid'">光り方<select v-model="rule.led.effect"><option value="steady">点灯</option><option value="breathe">ゆっくり点滅</option><option value="flow">流れる光</option></select></label>
       <label>明るさ<input v-model.number="rule.led.brightness" type="range" min="1" max="255"><span class="plate-small muted">{{Math.round(rule.led.brightness/255*100)}}%</span></label>
       <p class="plate-small muted">発話中と終了後2秒間点灯します。発話なしの場合は2秒間です。</p>
       <button v-if="!rule.speak" class="text-button" :disabled="!canTestLED(rule)||previewing===rule.id" @click="preview(rule)"><Play :size="13"/>LEDを試す</button>
      </template>
     </div>
     <div v-if="rule.speak" class="plate-speech-edit">
      <label>発話メッセージ<input v-model="rule.message" maxlength="60" :placeholder="rule.messageAudio?'録音した音声を使います':'例: やりますねぇ！'" :disabled="Boolean(rule.messageAudio)"></label>
      <div class="plate-speech-audio">
       <label class="secondary compact plate-upload" :class="{disabled:uploading===rule.id}"><Upload :size="14"/>{{uploading===rule.id?'読み込み中…':rule.messageAudio?'別の音声ファイル':'音声ファイル（mp3・wav）を使う'}}<input type="file" accept="audio/*,.mp3,.wav,.m4a" :disabled="uploading===rule.id" @change="upload(rule,$event)"></label>
       <template v-if="rule.messageAudio"><audio :src="`/api/v1/plate/speech/uploads/${rule.messageAudio}`" controls preload="none"></audio><button class="text-button" @click="rule.messageAudio=''">音声ファイルをやめて文字から生成</button></template>
      </div>
      <p class="plate-small">読み上げ：「{{speechPreview(rule,draft.voice)}}」</p>
      <p class="plate-small plate-speech-state"><span :class="{'plate-warn':speech?.status.rules[rule.id]?.error}">{{clipLabel(rule)}}</span><button class="text-button" :disabled="!canPreview(rule)" @click="listen=listen===rule.id?'':rule.id"><Volume2 :size="13"/>この画面で聴く</button><button class="text-button" :disabled="!canPreview(rule)||previewing===rule.id" :title="canPreview(rule)?'':'保存済みで、発話が利用可能なときに試聴できます'" @click="preview(rule)"><Play :size="13"/>{{previewing===rule.id?'再生中…':'本体で試聴'}}</button></p>
      <audio v-if="listen===rule.id&&canPreview(rule)" class="plate-listen" :src="`/api/v1/plate/speech/rules/${encodeURIComponent(rule.id)}/preview?v=${settings?.revision}`" controls autoplay></audio>
     </div>
    </fieldset>
   </div>
   <div class="plate-save-bar"><span class="muted plate-small">rev. {{settings?.revision}}<template v-if="dirty"> · 未保存の変更があります</template></span><button class="secondary" :disabled="!dirty||saving" @click="resetDraft">変更を取り消す</button><button class="primary" :disabled="!dirty||saving" @click="save">{{saving?'保存中…':'設定を保存'}}</button></div>
  </section>
  <section v-else-if="tab==='rules'" class="panel plate-empty">設定を読み込めませんでした。<button class="text-button" @click="reloadSettings"><RefreshCw :size="14"/>再読み込み</button></section>

  <section v-else class="panel plate-status" aria-label="状態">
   <div class="panel-head"><h2>認識と保存の状態</h2><button class="text-button" @click="loadStatus"><RefreshCw :size="14"/>更新</button></div>
   <dl v-if="status" class="details plate-details">
    <div><dt>認識プロセス</dt><dd>{{sensorLabel(status.sensor.state)}}</dd></div>
    <div v-if="status.sensor.reason"><dt>理由</dt><dd>{{status.sensor.reason}}</dd></div>
    <div v-if="status.sensor.engine"><dt>認識エンジン</dt><dd>{{status.sensor.engine}}<template v-if="status.sensor.backend"> · {{status.sensor.backend}}</template></dd></div>
    <div v-if="status.sensor.fps"><dt>処理速度</dt><dd>{{status.sensor.fps.toFixed(1)}} fps</dd></div>
    <div v-if="status.sensor.lastFrameAt"><dt>最後に処理したフレーム</dt><dd>{{time(status.sensor.lastFrameAt)}}</dd></div>
    <div><dt>取り込み</dt><dd>{{collectorLabel(status)}}</dd></div>
    <div><dt>本体での発話</dt><dd>{{status.speech.available?'利用可能':status.speech.reason}}<template v-if="speech"> · {{jobLabel(speech.status)}}</template></dd></div>
    <div v-if="speech"><dt>音声</dt><dd>{{speech.status.credit}}（WEB版VOICEVOX API）</dd></div>
    <div><dt>記録</dt><dd>{{status.usage.recordDays}} 日分 · {{bytes(status.usage.recordBytes)}}<template v-if="status.usage.openRecords"> · 統合中 {{status.usage.openRecords}} 件</template></dd></div>
    <div><dt>画像</dt><dd>{{status.usage.imageCount.toLocaleString()}} 枚 · {{bytes(status.usage.imageBytes)}} / {{bytes(status.imageQuotaBytes)}}</dd></div>
   </dl>
  </section>

  <PlateClipDialog v-if="clipFor" :record="clipFor" @close="clipFor=undefined"/>
  <div v-if="imageFor" class="modal-backdrop" @click.self="imageFor=undefined">
   <section class="modal plate-image-modal" role="dialog" aria-modal="true" :aria-label="`${plateDisplay(imageFor)} の画像`">
    <div class="modal-title"><h2>{{plateDisplay(imageFor)}}</h2><button class="icon-button" aria-label="閉じる" @click="imageFor=undefined"><X :size="16"/></button></div>
    <div class="plate-image-frame"><img :src="plateImageURL(imageFor.id)" :alt="`${plateDisplay(imageFor)} を検出したときの前方カメラ画像`"><i v-if="imageFor.box[2]>0" class="plate-box" :style="{left:imageFor.box[0]/19.2+'%',top:imageFor.box[1]/10.8+'%',width:imageFor.box[2]/19.2+'%',height:imageFor.box[3]/10.8+'%'}"></i></div>
    <p class="plate-small muted">{{time(imageFor.firstSeen)}} · {{imageFor.matches.map(m=>m.ruleName).join('、')}}</p>
   </section>
  </div>
 </div>
</template>
