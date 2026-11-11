<script setup lang="ts">
import { computed, ref } from 'vue'
import { ExternalLink, Database, Gauge, HardDrive, MapPin, Radio, Route, ShieldCheck, Video, Wifi, WifiHigh } from '@lucide/vue'
import { bytes, type Summary } from '../services/api'
import TrafficUsage from './TrafficUsage.vue'

type TripDay = { date:string; distanceKm:number|null; drivingSeconds:number|null; observed?:boolean; sampleCount?:number }
type Bucket = { key:string; label:string; fullLabel:string; value:number|null; observedDays:number; totalDays:number }
const props = defineProps<{summary:Summary}>()
const emit = defineEmits<{navigate:[page:string]}>()
const period = ref(7)
const tripMetric = ref<'distance'|'time'>('distance')
const selectedBucket = ref<string|null>(null)
function data(key:string) { const item=props.summary[key]; return item && ['available','stale'].includes(item.status) ? item.data : undefined }
function number(value:unknown):value is number { return typeof value==='number' && Number.isFinite(value) }
function decimal(value:unknown):string|null { return typeof value==='string' && /^\d+$/.test(value) ? value : null }
function byteText(value:unknown) { const n=decimal(value);return n===null?'—':bytes(n) }
function percent(value:unknown) { return number(value)&&value>=0&&value<=100?`${value.toFixed(0)}%`:'—' }
function shortDate(date:string) { return `${Number(date.slice(5,7))}/${Number(date.slice(8,10))}` }
function duration(seconds:unknown) { if(!number(seconds)||seconds<0)return '—';const minutes=Math.floor(seconds/60);return `${Math.floor(minutes/60)}時間 ${minutes%60}分` }
const wifi=computed(()=>data('wifi'))
const vpn=computed(()=>data('tailscale'))
const sd=computed(()=>data('sd'))
const ram=computed(()=>data('ram'))
const cpu=computed(()=>data('cpu'))
const storage=computed(()=>data('internalStorage'))
const modem=computed(()=>data('modem'))
const hotspot=computed(()=>data('hotspot'))
const gps=computed(()=>data('gps')||(props.summary.gps?.data?.fix===false?props.summary.gps.data:undefined))
const trip=computed(()=>data('tripStats'))
const selectedTrip=computed(()=>trip.value?.periods?.[String(period.value)])
const tripDistance=computed(()=>number(selectedTrip.value?.distanceKm)?selectedTrip.value.distanceKm.toLocaleString('ja-JP',{maximumFractionDigits:1}):'—')
const tripTime=computed(()=>duration(selectedTrip.value?.drivingSeconds))
const temperature=computed(()=>{const values=data('temperatures');if(!Array.isArray(values))return null;const cpus=values.filter(t=>typeof t.name==='string'&&t.name.toLowerCase().includes('cpu')&&number(t.celsius)&&t.celsius>=-40&&t.celsius<=150);return cpus.length?Math.max(...cpus.map(t=>t.celsius)):null})
const recording=computed(()=>{const value=data('recording');if(value?.writing===true)return '録画ファイルを更新中';if(value?.writing===false)return '直近の録画更新なし';if(number(value?.finalizedClips))return `保存済みの録画 ${value.finalizedClips.toLocaleString('ja-JP')} 件`;const service=(data('services') as {name:string;state:string}[]|undefined)?.find(s=>s.name==='app-@DriveRec.service');return service?.state==='failed'?'録画処理を確認してください':'録画状況は確認中'})
const gpsLabel=computed(()=>gps.value?.fix===true?'測位済み':gps.value?.fix===false?'測位待ち':'測位データ未取得')
const mobileLabel=computed(()=>modem.value?.pdpConnected===true?'データ接続中':modem.value?.registered===true?'回線登録済み':modem.value?.registered===false?'未登録':'取得待ち')
const apLabel=computed(()=>hotspot.value?.enabled===true?'提供中':hotspot.value?.enabled===false?'停止中':'取得待ち')
const wifiLabel=computed(()=>{const state=wifi.value?.wpa_state;if(state==='COMPLETED')return '接続済み';if(['DISCONNECTED','INACTIVE','INTERFACE_DISABLED'].includes(state))return '未接続';return state?'接続中':'取得待ち'})
const vpnLabel=computed(()=>{if(vpn.value?.online===true)return 'オンライン';if(vpn.value?.backendState==='NeedsLogin')return '認証待ち';if(vpn.value?.backendState==='Stopped')return '停止中';return vpn.value?.online===false?'オフライン':'取得待ち'})
const vpnTitle=computed(()=>vpn.value?.online===true?'管理ネットワークに接続':vpn.value?.backendState==='NeedsLogin'?'Tailscaleで認証が必要':'接続状態を確認中')
const vpnPaths=computed(()=>{const paths=vpn.value?.activePeerConnections;if(!Array.isArray(paths)||!paths.length)return vpn.value?.online===true?'通信待ち':'—';return [...new Set(paths.map((p:string)=>p==='Direct'?'直接':p==='DERP'?'リレー':'確認中'))].join(' / ')})
const history=computed(()=>Array.isArray(selectedTrip.value?.days)?selectedTrip.value.days as TripDay[]:[])
const granularity=computed(()=>period.value===365?'月別':period.value===90?'週別':'日別')
const metricUnit=computed(()=>tripMetric.value==='distance'?'km':'時間')
const rangeEnd=computed(()=>new Intl.DateTimeFormat('sv-SE',{timeZone:'Asia/Tokyo'}).format(new Date()))
const buckets=computed<Bucket[]>(()=>{
 const entries=new Map(history.value.map(d=>[d.date,d]))
 const end=new Date(rangeEnd.value+'T00:00:00Z')
 const groups=new Map<string,Bucket>()
 for(let offset=period.value-1;offset>=0;offset--){
  const day=new Date(end.getTime()-offset*86400000).toISOString().slice(0,10)
  const index=period.value-1-offset
  const key=period.value===365?day.slice(0,7):period.value===90?String(Math.floor(index/7)):day
  let bucket=groups.get(key)
  if(!bucket){bucket={key,label:period.value===365?`${Number(day.slice(5,7))}月`:shortDate(day),fullLabel:day,value:null,observedDays:0,totalDays:0};groups.set(key,bucket)}
  bucket.totalDays++
  const entry=entries.get(day)
  const value=tripMetric.value==='distance'?entry?.distanceKm:entry?.drivingSeconds
  if(entry?.observed!==false&&number(value)&&value>=0){bucket.value=(bucket.value??0)+(tripMetric.value==='distance'?value:value/3600);bucket.observedDays++}
  if(period.value===90&&bucket.totalDays>1)bucket.fullLabel=bucket.fullLabel.slice(0,10)+'〜'+day
 }
 return [...groups.values()]
})
const hasHistory=computed(()=>buckets.value.some(b=>b.value!==null))
const maximum=computed(()=>Math.max(1,...buckets.value.map(b=>b.value??0)))
const selectedDetail=computed(()=>buckets.value.find(b=>b.key===selectedBucket.value))
const observedDays=computed(()=>number(selectedTrip.value?.validObservedDays)?selectedTrip.value.validObservedDays:history.value.filter(d=>d.observed!==false&&(number(d.distanceKm)||number(d.drivingSeconds))).length)
const rangeStart=computed(()=>new Date(new Date(rangeEnd.value+'T00:00:00Z').getTime()-(period.value-1)*86400000).toISOString().slice(0,10))
function bucketDescription(b:Bucket){return `${b.fullLabel} · ${b.value===null?'未記録':`${b.value.toLocaleString('ja-JP',{maximumFractionDigits:1})} ${metricUnit.value}`} · 記録 ${b.observedDays}/${b.totalDays}日`}
</script>

<template>
 <div class="dashboard-hero">
  <section class="panel journey-panel" aria-labelledby="journey-heading">
   <div class="hero-head"><h2 id="journey-heading"><Route :size="20"/>走行の記録</h2><button class="icon-button section-link" aria-label="走行マップを開く" @click="emit('navigate','trips')"><ExternalLink :size="18"/></button></div>
   <div class="journey-values">
    <div><span class="metric-label">走行距離</span><strong data-testid="trip-distance">{{tripDistance}}<small>km</small></strong></div>
    <div><span class="metric-label">走行時間</span><strong class="duration-value" data-testid="trip-time">{{tripTime}}</strong></div>
   </div>
   <div class="chart-toolbar"><div class="period-switch" role="group" aria-label="走行記録の期間"><button v-for="days in [7,30,90,365]" :key="days" :class="{selected:period===days}" :aria-pressed="period===days" @click="period=days;selectedBucket=null">{{days}}日</button></div><select v-model="tripMetric" aria-label="走行グラフの指標" @change="selectedBucket=null"><option value="distance">距離</option><option value="time">時間</option></select></div>
   <template v-if="hasHistory">
    <div class="chart-scale"><span>{{granularity}} · {{metricUnit}}</span><span>{{maximum.toLocaleString('ja-JP',{maximumFractionDigits:1})}} {{metricUnit}}</span></div>
    <div class="journey-chart" role="group" :aria-label="`${period}日間の${tripMetric==='distance'?'走行距離':'走行時間'}`" :style="{gridTemplateColumns:`repeat(${buckets.length},minmax(0,1fr))`}" :class="{'chart-dense':buckets.length>14}">
     <button v-for="b in buckets" :key="b.key" class="day-column" :class="{missing:b.value===null,zero:b.value===0,selected:selectedBucket===b.key}" :title="bucketDescription(b)" :aria-label="bucketDescription(b)" :aria-pressed="selectedBucket===b.key" @click="selectedBucket=b.key"><span v-if="b.value!==null&&b.value>0" class="day-bar" :class="{partial:b.observedDays<b.totalDays}" :style="{height:(b.value/maximum*100)+'%'}"></span><span v-else-if="b.value===0" class="zero-mark"></span><span v-else class="missing-mark"></span></button>
    </div>
    <div class="chart-dates"><span>{{shortDate(rangeStart)}}</span><span>{{period===365?'月ごとに集計':period===90?'7日ごとに集計':'1日ごとに集計'}}</span><span>{{shortDate(rangeEnd)}}</span></div>
    <p class="chart-detail" role="status">{{selectedDetail?bucketDescription(selectedDetail):`記録のある ${observedDays} 日分を集計。棒をタップして詳細を確認できます。`}}</p>
   </template>
   <div v-else class="journey-chart empty-chart"><Route :size="25" stroke-width="1.4"/><strong>この期間の走行記録はありません</strong><span>距離と時間は、測位できた記録から集計します。</span></div>
   <div class="hero-foot"><MapPin :size="14"/><span>{{gps?.fix===true?'GPS測位済み':gps?.fix===false?'GPS測位待ち':'GPSデータ待ち'}}</span><span>過去{{period}}日間 · 未記録は集計対象外</span></div>
  </section>
  <TrafficUsage :summary="summary" @navigate="emit('navigate',$event)"/>
 </div>

 <div class="dash-section-title"><h2>接続状況</h2><span>モバイル・Wi-Fi・遠隔接続を個別に表示</span></div>
 <div class="connection-grid">
  <section class="panel connection-card"><div class="connection-title"><Radio :size="20"/><h3>4G / モバイル回線</h3><span class="connection-badge" :class="{online:modem?.registered===true}">{{mobileLabel}}</span><button class="icon-button section-link" aria-label="回線の詳細を開く" @click="emit('navigate','internet')"><ExternalLink :size="18"/></button></div><strong class="connection-primary">{{modem?.carrier||'回線情報待ち'}}</strong><dl><div><dt>バンド</dt><dd>{{modem?.band?`Band ${modem.band}`:'—'}}</dd></div><div><dt>電波強度</dt><dd>{{number(modem?.rsrp)?`${modem.rsrp} dBm`:'—'}}</dd></div></dl></section>
  <section class="panel connection-card"><div class="connection-title"><WifiHigh :size="20"/><h3>Wi-Fi AP</h3><span class="connection-badge" :class="{online:hotspot?.enabled===true}">{{apLabel}}</span><button class="icon-button section-link" aria-label="ホットスポットを開く" @click="emit('navigate','wifi')"><ExternalLink :size="18"/></button></div><strong class="connection-primary" :title="hotspot?.ssid">{{hotspot?.ssid||'ホットスポット情報待ち'}}</strong><dl><div><dt>接続端末</dt><dd>{{number(hotspot?.clientCount)?`${hotspot.clientCount} 台`:'—'}}</dd></div><div><dt>動作モード</dt><dd>{{hotspot?.mode==='always-on'?'常時ON':hotspot?.mode==='LAN-always-available'?'LANを維持':hotspot?.mode||'—'}}</dd></div></dl></section>
  <section class="panel connection-card"><div class="connection-title"><Wifi :size="20"/><h3>Wi-Fi STA</h3><span class="connection-badge" :class="{online:wifi?.wpa_state==='COMPLETED'}">{{wifiLabel}}</span><button class="icon-button section-link" aria-label="Wi-Fiの詳細を開く" @click="emit('navigate','wifi')"><ExternalLink :size="18"/></button></div><strong class="connection-primary" :title="wifi?.ssid">{{wifi?.ssid||'接続先情報待ち'}}</strong><dl><div><dt>認証</dt><dd>{{wifi?.key_mgmt||'—'}}</dd></div><div><dt>周波数</dt><dd>{{wifi?.freq?`${wifi.freq} MHz`:'—'}}</dd></div></dl></section>
  <section class="panel connection-card"><div class="connection-title"><ShieldCheck :size="20"/><h3>Tailscale</h3><span class="connection-badge" :class="{online:vpn?.online===true}">{{vpnLabel}}</span><button class="icon-button section-link" aria-label="遠隔接続の詳細を開く" @click="emit('navigate','internet')"><ExternalLink :size="18"/></button></div><strong class="connection-primary">{{vpnTitle}}</strong><dl><div><dt>アドレス</dt><dd class="mono">{{vpn?.ips?.find((ip:string)=>ip.includes('.'))||'—'}}</dd></div><div><dt>通信経路</dt><dd>{{vpnPaths}}</dd></div></dl></section>
 </div>

 <div class="dashboard-lower">
  <section class="panel gps-panel"><div class="hero-head"><h2><MapPin :size="20"/>GPS・録画</h2><button class="icon-button section-link" aria-label="電波マップを開く" @click="emit('navigate','trips')"><ExternalLink :size="18"/></button></div><div class="gps-status"><div class="gps-orbit"><MapPin :size="27" stroke-width="1.4"/></div><div><strong>{{gpsLabel}}</strong><p>衛星 {{number(gps?.satellites)?gps.satellites:'—'}} / 精度 {{number(gps?.accuracyM)&&gps.accuracyM>0?`${gps.accuracyM} m`:'—'}}</p></div></div><div class="recording-health"><Video :size="21"/><div><strong>{{recording}}</strong><small>最新の映像と録画区間を確認できます</small></div><button class="icon-button section-link" aria-label="ドラレコ再生を開く" @click="emit('navigate','recordings')"><ExternalLink :size="18"/></button></div></section>
  <section class="panel body-panel"><div class="hero-head"><h2><Gauge :size="20"/>本体の状態</h2><button class="icon-button section-link" aria-label="詳細を開く" @click="emit('navigate','system')"><ExternalLink :size="18"/></button></div><div class="device-metrics"><div class="device-meter"><span>CPU</span><strong>{{percent(cpu?.usedPercent)}}</strong><progress v-if="number(cpu?.usedPercent)" max="100" :value="cpu.usedPercent" aria-label="CPU使用率"></progress><span v-else class="unknown-meter" aria-label="CPU使用率は未取得"></span></div><div class="device-meter"><span>RAM</span><strong>{{percent(ram?.usedPercent)}}</strong><progress v-if="number(ram?.usedPercent)" max="100" :value="ram.usedPercent" aria-label="RAM使用率"></progress><span v-else class="unknown-meter" aria-label="RAM使用率は未取得"></span><small>{{ram?`${byteText(ram.usedBytes)} / ${byteText(ram.totalBytes)}`:'計測待ち'}}</small></div><div class="device-meter"><span>本体温度</span><strong>{{temperature===null?'—':`${temperature.toFixed(1)} °C`}}</strong><small>CPU周辺センサー</small></div></div><div class="storage-row"><HardDrive :size="21"/><div><strong>SDカード <span class="connection-badge" :class="{online:summary.sd?.status==='available'}">{{summary.sd?.status==='available'?'利用可能':'状態未取得'}}</span></strong><small>残り {{byteText(sd?.freeBytes)}} / {{byteText(sd?.totalBytes)}}</small></div><span>{{percent(sd?.usedPercent)}} 使用</span></div><div class="storage-row"><Database :size="20"/><div><strong>本体空き容量</strong><small>アプリ・設定の保存領域</small></div><span>{{byteText(storage?.freeBytes)}}</span></div></section>
 </div>
</template>
