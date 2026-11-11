<script setup lang="ts">
import {computed,nextTick,onMounted,onUnmounted,ref,watch} from 'vue'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import {AlertTriangle,Camera,Image as ImageIcon,MapPin,Plus,RefreshCw,ScanLine,Trash2,Volume2,X} from '@lucide/vue'
import {api,bytes} from '../services/api'
import {plateDisplay,sensorLabel,speechLabel,collectorLabel,newRuleId,jstDay,type PlateRecord,type PlateRecordsResponse,type PlateRule,type PlateSettings,type PlateStatus} from '../services/plate'
import '../plate-recognition.css'

const tab=ref<'records'|'rules'|'status'>('records')
const status=ref<PlateStatus>(),settings=ref<PlateSettings>(),draft=ref<PlateSettings>()
const records=ref<PlateRecordsResponse['records']>([]),truncated=ref(false),listReason=ref('')
const loading=ref(false),saving=ref(false),error=ref(''),notice=ref('')
const from=ref(jstDay(-6)),to=ref(jstDay(0)),matchedOnly=ref(false),query=ref(''),online=ref(true)
const selectedId=ref(''),imageFor=ref<PlateRecord>()
const mapEl=ref<HTMLElement>(),listEl=ref<HTMLElement>()
let map:L.Map|undefined,layer:L.LayerGroup|undefined,tiles:L.TileLayer|undefined,resize:ResizeObserver|undefined,fitted=false,sequence=0,statusTimer:ReturnType<typeof setInterval>|undefined
const markers=new Map<string,L.CircleMarker>()

const placed=computed(()=>records.value.filter(r=>r.record.location))
const matchedCount=computed(()=>records.value.filter(r=>r.record.matches.length).length)
const dirty=computed(()=>JSON.stringify(draft.value)!==JSON.stringify(settings.value))
const time=(v:string)=>new Date(v).toLocaleString('ja-JP',{timeZone:'Asia/Tokyo',month:'numeric',day:'numeric',hour:'2-digit',minute:'2-digit',second:'2-digit'})
const message=(e:unknown)=>e instanceof Error?e.message:String(e)
// Reactive proxies cannot be structured-cloned; settings are plain JSON.
const clone=<T,>(value:T):T=>JSON.parse(JSON.stringify(value))

async function loadStatus(){try{status.value=await api<PlateStatus>('/plate/status')}catch(e){error.value=message(e)}}
async function loadSettings(){const s=await api<PlateSettings>('/plate/settings');settings.value=s;draft.value=clone(s)}
async function loadRecords(){const id=++sequence;loading.value=true;listReason.value='';try{const q=new URLSearchParams({from:from.value,to:to.value});if(matchedOnly.value)q.set('matched','true');if(query.value.trim())q.set('q',query.value.trim());const r=await api<PlateRecordsResponse>('/plate/records?'+q);if(id!==sequence)return;records.value=r.records;truncated.value=r.truncated;listReason.value=r.reason;if(!r.records.some(x=>x.record.id===selectedId.value))selectedId.value='';fitted=false;render()}catch(e){if(id===sequence){records.value=[];error.value=message(e);render()}}finally{if(id===sequence)loading.value=false}}

// Colors come from CSS classes (theme tokens); the option colors are only fallbacks.
function markerStyle(r:PlateRecord,selected:boolean):L.CircleMarkerOptions{const hit=r.matches.length>0;return{pane:hit?'plate-hit':'overlayPane',radius:hit?(selected?13:10):(selected?9:6),color:hit?'#8f2d14':'#2f4e5c',fillColor:hit?'#e8590c':'#5b7c8a',fillOpacity:hit?.95:.7,weight:selected?4:hit?3:1.5,className:hit?'plate-marker-hit':'plate-marker'}}
function popupNode(r:PlateRecord){const node=document.createElement('div');node.className='plate-popup';const b=document.createElement('strong');b.textContent=plateDisplay(r);node.appendChild(b);const t=document.createElement('div');t.textContent=time(r.firstSeen);node.appendChild(t);if(r.matches.length){const m=document.createElement('div');m.className='plate-popup-hit';m.textContent='条件: '+r.matches.map(x=>x.ruleName).join('、');node.appendChild(m)}return node}
function render(){if(!map||!layer)return;layer.clearLayers();markers.clear();const bounds:L.LatLngExpression[]=[];
 // Normal points first so condition hits are drawn above them.
 const ordered=[...placed.value].sort((a,b)=>a.record.matches.length-b.record.matches.length)
 for(const {record} of ordered){const loc=record.location!;const position:L.LatLngExpression=[loc.lat,loc.lon];bounds.push(position);const m=L.circleMarker(position,markerStyle(record,record.id===selectedId.value)).bindPopup(popupNode(record));m.on('click',()=>select(record.id,false));m.addTo(layer);markers.set(record.id,m)}
 if(bounds.length&&!fitted){map.fitBounds(L.latLngBounds(bounds),{padding:[30,30],maxZoom:16});fitted=true}}
async function select(id:string,fromList:boolean){const previous=selectedId.value;selectedId.value=id;for(const key of [previous,id]){const r=records.value.find(x=>x.record.id===key)?.record;const m=markers.get(key);if(r&&m)m.setStyle(markerStyle(r,key===id))}const m=markers.get(id);if(fromList&&m&&map){map.panTo(m.getLatLng());m.openPopup()}if(!fromList){await nextTick();listEl.value?.querySelector<HTMLElement>(`[data-record="${id}"]`)?.scrollIntoView({block:'nearest',behavior:'smooth'})}}
function renderTiles(){if(!map)return;if(online.value&&!tiles){tiles=L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png',{maxZoom:19,attribution:'© <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener">OpenStreetMap</a>'}).addTo(map)}else if(!online.value){tiles?.remove();tiles=undefined}}
function initMap(){if(map||!mapEl.value)return;map=L.map(mapEl.value).setView([36.2,138.2],5);map.attributionControl.setPrefix(false);map.createPane('plate-hit').style.zIndex='650';layer=L.layerGroup().addTo(map);renderTiles();resize=new ResizeObserver(()=>map?.invalidateSize());resize.observe(mapEl.value);render()}
function destroyMap(){resize?.disconnect();resize=undefined;map?.remove();map=undefined;layer=undefined;tiles=undefined;markers.clear()}

function addRule(){draft.value?.rules.push({id:newRuleId(),name:'',enabled:true,region:'',class:'',kana:'',number:'',saveImage:true,speak:false})}
function removeRule(rule:PlateRule){if(!draft.value)return;draft.value.rules=draft.value.rules.filter(r=>r!==rule)}
function resetDraft(){draft.value=clone(settings.value)}
async function save(){if(!draft.value||!settings.value)return;saving.value=true;error.value='';try{const next=await api<PlateSettings>('/plate/settings','PUT',{expectedRevision:settings.value.revision,settings:draft.value});settings.value=next;draft.value=clone(next);notice.value='設定を保存しました';await loadStatus()}catch(e){error.value=message(e)}finally{saving.value=false}}
async function reloadSettings(){error.value='';try{await loadSettings()}catch(e){error.value=message(e)}}

watch([from,to,matchedOnly],loadRecords)
let searchTimer:ReturnType<typeof setTimeout>|undefined
watch(query,()=>{if(searchTimer)clearTimeout(searchTimer);searchTimer=setTimeout(loadRecords,350)})
watch(online,renderTiles)
watch(tab,async value=>{if(value==='records'){await nextTick();initMap()}else destroyMap()})
watch(notice,v=>{if(v)setTimeout(()=>notice.value='',4000)})
onMounted(async()=>{await Promise.all([loadStatus(),reloadSettings(),loadRecords()]);initMap();statusTimer=setInterval(loadStatus,10000)})
onUnmounted(()=>{sequence++;if(statusTimer)clearInterval(statusTimer);destroyMap()})
</script>

<template>
 <div class="plate-page">
  <section class="panel plate-summary">
   <div class="plate-summary-main"><ScanLine :size="22"/><div><h2>ナンバー認識</h2><p>前方カメラで読み取ったナンバーと場所を記録し、条件に合う車両を目立たせます。</p></div></div>
   <dl class="plate-chips" v-if="status">
    <div><dt>記録</dt><dd :class="status.enabled?'on':'off'">{{status.enabled?'有効':'停止中'}}</dd></div>
    <div><dt>認識</dt><dd :class="status.sensor.state==='running'?'on':'warn'">{{sensorLabel(status.sensor.state)}}</dd></div>
    <div><dt>発話</dt><dd :class="status.speech.available?'on':'warn'">{{status.speech.available?'利用可能':'未対応'}}</dd></div>
   </dl>
  </section>
  <div v-if="error" class="banner error" role="alert"><AlertTriangle :size="16"/><span>{{error}}</span><button class="icon-button" aria-label="閉じる" @click="error=''"><X :size="15"/></button></div>
  <div v-if="notice" class="banner success" role="status">{{notice}}</div>
  <div v-if="status&&status.sensor.state!=='running'" class="banner neutral plate-sensor-note"><Camera :size="16"/><span>{{status.sensor.reason||sensorLabel(status.sensor.state)}}。認識が動作するまで新しい記録は増えません。</span></div>

  <nav class="plate-tabs" role="tablist" aria-label="ナンバー認識">
   <button v-for="t in [{id:'records',label:'記録'},{id:'rules',label:'条件と設定'},{id:'status',label:'状態'}] as const" :key="t.id" role="tab" :aria-selected="tab===t.id" :class="{active:tab===t.id}" @click="tab=t.id">{{t.label}}</button>
  </nav>

  <section v-if="tab==='records'" class="panel plate-records" aria-label="認識したナンバーの記録">
   <div class="plate-toolbar">
    <label>開始日<input v-model="from" type="date" :max="to"></label>
    <label>終了日<input v-model="to" type="date" :min="from"></label>
    <label class="plate-search">ナンバーで検索<input v-model="query" type="search" placeholder="例: 12-34、品川、わ" maxlength="20"></label>
    <label class="plate-check"><input v-model="matchedOnly" type="checkbox">条件に合ったものだけ</label>
    <label class="plate-check"><input v-model="online" type="checkbox">地図の背景を表示</label>
    <button class="secondary compact" :disabled="loading" @click="loadRecords"><RefreshCw :size="14"/>更新</button>
   </div>
   <p class="plate-count" role="status">{{loading?'読み込み中…':`${records.length.toLocaleString()} 件（条件に合致 ${matchedCount.toLocaleString()} 件 · 地図に表示 ${placed.length.toLocaleString()} 件）`}}<span v-if="truncated"> · 新しい順に上限まで表示しています</span><span v-if="listReason"> · {{listReason}}</span></p>
   <div class="plate-split">
    <div class="plate-map-wrap"><div ref="mapEl" class="plate-map" role="region" aria-label="記録した場所の地図"></div><div class="plate-legend"><span><i class="dot"></i>記録</span><span><i class="dot hit"></i>条件に合致</span></div><p v-if="!loading&&records.length&&!placed.length" class="plate-map-empty"><MapPin :size="16"/>測位できた記録がないため、地図には表示できません。</p></div>
    <div ref="listEl" class="plate-list">
     <p v-if="!loading&&!records.length" class="plate-empty">この期間の記録はありません。</p>
     <article v-for="{record,imageAvailable} in records" :key="record.id" :data-record="record.id" :class="['plate-item',{hit:record.matches.length,selected:record.id===selectedId}]">
      <button class="plate-item-main" :aria-pressed="record.id===selectedId" @click="select(record.id,true)">
       <span class="plate-badge" :class="{hit:record.matches.length}">{{plateDisplay(record)}}</span>
       <span class="plate-meta"><time :datetime="record.firstSeen">{{time(record.firstSeen)}}</time><span v-if="record.sightings>1">{{record.sightings}}回検出</span><span>信頼度 {{Math.round(record.reading.confidence*100)}}%</span><span :class="{'plate-nofix':!record.location}">{{record.location?'位置あり':'位置なし'}}</span></span>
      </button>
      <div v-if="record.matches.length||record.imageId||record.imageNote||record.speech" class="plate-item-extra">
       <span v-for="m in record.matches" :key="m.ruleId" class="plate-rule-tag">{{m.ruleName}}</span>
       <button v-if="imageAvailable" class="text-button" @click="imageFor=record"><ImageIcon :size="14"/>画像を見る</button>
       <span v-else-if="record.imageId" class="muted plate-small">画像は保存期間・容量上限により削除済み</span>
       <span v-else-if="record.imageNote" class="muted plate-small">{{record.imageNote}}</span>
       <span v-if="record.speech" class="plate-speech" :class="record.speech"><Volume2 :size="13"/>{{speechLabel(record.speech)}}</span>
      </div>
     </article>
    </div>
   </div>
  </section>

  <section v-else-if="tab==='rules'&&draft" class="plate-rules" aria-label="条件と設定">
   <div class="panel plate-settings">
    <label class="plate-switch"><input v-model="draft.enabled" type="checkbox"><span><strong>ナンバーの記録を有効にする</strong><small>オフの間は、認識結果を記録しません。</small></span></label>
    <div class="plate-grid">
     <label>記録の保存日数<input v-model.number="draft.retentionDays" type="number" min="1" max="3650" inputmode="numeric"></label>
     <label>画像の保存容量（MB）<input v-model.number="draft.imageQuotaMB" type="number" min="16" max="1024" inputmode="numeric"></label>
     <label>同じ車両とみなす間隔（秒）<input v-model.number="draft.mergeSeconds" type="number" min="5" max="300" inputmode="numeric"></label>
     <label>記録する信頼度の下限<input v-model.number="draft.minConfidence" type="number" min="0.3" max="0.99" step="0.05" inputmode="decimal"></label>
    </div>
    <p class="hint">保存日数や容量を超えた記録・画像は古いものから削除されます。記録と画像はこの端末内だけに保存されます。</p>
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
     <p v-if="rule.speak&&status&&!status.speech.available" class="plate-small plate-warn">{{status.speech.reason}}。記録には「発話未対応」と残ります。</p>
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
    <div><dt>本体での発話</dt><dd>{{status.speech.available?'利用可能':status.speech.reason}}</dd></div>
    <div><dt>記録</dt><dd>{{status.usage.recordDays}} 日分 · {{bytes(status.usage.recordBytes)}}<template v-if="status.usage.openRecords"> · 統合中 {{status.usage.openRecords}} 件</template></dd></div>
    <div><dt>画像</dt><dd>{{status.usage.imageCount.toLocaleString()}} 枚 · {{bytes(status.usage.imageBytes)}} / {{bytes(status.imageQuotaBytes)}}</dd></div>
   </dl>
  </section>

  <div v-if="imageFor" class="modal-backdrop" @click.self="imageFor=undefined">
   <section class="modal plate-image-modal" role="dialog" aria-modal="true" :aria-label="`${plateDisplay(imageFor)} の画像`">
    <div class="modal-title"><h2>{{plateDisplay(imageFor)}}</h2><button class="icon-button" aria-label="閉じる" @click="imageFor=undefined"><X :size="16"/></button></div>
    <div class="plate-image-frame"><img :src="`/api/v1/plate/records/${encodeURIComponent(imageFor.id)}/image`" :alt="`${plateDisplay(imageFor)} を検出したときの前方カメラ画像`"><i v-if="imageFor.box[2]>0" class="plate-box" :style="{left:imageFor.box[0]/19.2+'%',top:imageFor.box[1]/10.8+'%',width:imageFor.box[2]/19.2+'%',height:imageFor.box[3]/10.8+'%'}"></i></div>
    <p class="plate-small muted">{{time(imageFor.firstSeen)}} · {{imageFor.matches.map(m=>m.ruleName).join('、')}}</p>
   </section>
  </div>
 </div>
</template>
