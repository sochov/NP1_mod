<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { ArrowDown, ArrowUp, ExternalLink, Radio, Settings2, Wifi } from '@lucide/vue'
import { api, type Observation, type Settings, type Summary } from '../services/api'

type NetworkUsage={interface?:string;todayRX:string|null;todayTX:string|null;monthRX:string|null;monthTX:string|null;rxBytesPerSecond:number|null;txBytesPerSecond:number|null;sampledAt:string|null;observedSince:string|null;status:string;speedStatus:string;active:boolean;stale:boolean;monthObservedSeconds?:number}
type Usage={networks?:{cellular?:NetworkUsage;wifi?:NetworkUsage};startedAt?:string;sampledAt?:string;stale?:boolean}
type LimitSettings=Settings&{cellularLimitBytes?:string}
const props=withDefaults(defineProps<{summary:Summary;showDetails?:boolean}>(),{showDetails:true})
const emit=defineEmits<{navigate:[page:string]}>()
const latest=ref<Observation<Usage|null>>(),settings=ref<LimitSettings>(),settingsLoading=ref(false),saving=ref(false),formOpen=ref(false),draftLimit=ref(''),settingsError=ref(''),savedMessage=ref(''),pollError=ref(false),now=ref(Date.now()),limitInput=ref<HTMLInputElement>()
let timer:ReturnType<typeof setInterval>|undefined,inflight=false,active=true
const observation=computed(()=>{const a=latest.value,b=props.summary.usage;if(!a)return b;if(!b)return a;const at=Date.parse(a.data?.sampledAt||a.observedAt)||0,bt=Date.parse(b.data?.sampledAt||b.observedAt)||0;return at>=bt?a:b})
const usage=computed<Usage|undefined>(()=>['available','stale'].includes(observation.value?.status)?observation.value?.data||undefined:undefined)
const cellular=computed(()=>usage.value?.networks?.cellular),wifi=computed(()=>usage.value?.networks?.wifi)
function integer(value:unknown){return typeof value==='string'&&/^\d+$/.test(value)?BigInt(value):null}
function total(a:unknown,b:unknown){const x=integer(a),y=integer(b);return x===null||y===null?null:x+y}
function measured(network:NetworkUsage|undefined,period:'today'|'month'){return network?.status==='available'?total(network[period+'RX' as 'todayRX'|'monthRX'],network[period+'TX' as 'todayTX'|'monthTX']):null}
const monthBytes=computed(()=>measured(cellular.value,'month')),todayBytes=computed(()=>measured(cellular.value,'today'))
const limit=computed(()=>settings.value?integer(settings.value.cellularLimitBytes??'0'):null)
const remaining=computed(()=>limit.value!==null&&limit.value>0n&&monthBytes.value!==null?(monthBytes.value>=limit.value?0n:limit.value-monthBytes.value):null)
const excess=computed(()=>limit.value!==null&&limit.value>0n&&monthBytes.value!==null&&monthBytes.value>limit.value?monthBytes.value-limit.value:0n)
const remainingRatio=computed(()=>remaining.value!==null&&limit.value?Number(remaining.value*10000n/limit.value)/10000:0)
const ringCircumference=2*Math.PI*74
const ringColor=computed(()=>remaining.value===null?'var(--line)':remainingRatio.value<=.15?'var(--danger)':remainingRatio.value<=.3?'#bf8a36':'var(--accent)')
const sampledAt=computed(()=>usage.value?.sampledAt||observation.value?.observedAt)
const stale=computed(()=>pollError.value||usage.value?.stale||observation.value?.status==='stale'||(sampledAt.value&&now.value-Date.parse(sampledAt.value)>10000))
const knownLimit=computed(()=>limit.value!==null&&limit.value>0n)
function decimalGB(value:bigint,precision=1){const scale=10n**BigInt(precision),rounded=(value*scale+500000000n)/1000000000n;return (Number(rounded)/Number(scale)).toLocaleString('ja-JP',{minimumFractionDigits:precision,maximumFractionDigits:precision})}
function volume(value:bigint|null){if(value===null)return '—';for(const [unit,size] of [['TB',1000000000000n],['GB',1000000000n],['MB',1000000n],['KB',1000n]] as const){if(value>=size){const scaled=(value*10n+size/2n)/size;return `${(Number(scaled)/10).toLocaleString('ja-JP',{maximumFractionDigits:1})} ${unit}`}}return `${value.toLocaleString('ja-JP')} B`}
function speed(network:NetworkUsage|undefined,key:'rxBytesPerSecond'|'txBytesPerSecond'){const value=network?.[key];if(stale.value||network?.stale||network?.speedStatus!=='available'||typeof value!=='number'||!Number.isFinite(value)||value<0)return '—';const bits=value*8;if(!Number.isFinite(bits))return '—';for(const [unit,scale] of [['Gbps',1e9],['Mbps',1e6],['kbps',1e3]] as const){if(bits>=scale)return `${(bits/scale).toLocaleString('ja-JP',{minimumFractionDigits:bits/scale<10?1:0,maximumFractionDigits:1})} ${unit}`}return `${Math.round(bits).toLocaleString('ja-JP')} bps`}
function speedState(network:NetworkUsage|undefined){if(stale.value)return '更新待ち';if(network?.speedStatus==='inactive')return '現在は未接続';if(!network||network.speedStatus==='unknown')return '速度データ待ち';if(network.stale)return '更新待ち';if(network.speedStatus==='available')return '2秒ごとに更新';if(network.speedStatus==='baseline')return '最初の速度を計測中';if(['reset','reboot','gap'].includes(network.speedStatus))return '回線の変化を検出・再計測中';return '速度データ待ち'}
function time(value:string|undefined|null){if(!value||Number.isNaN(Date.parse(value)))return '—';return new Date(value).toLocaleTimeString('ja-JP',{hour:'2-digit',minute:'2-digit',second:'2-digit',hourCycle:'h23'})}
const since=computed(()=>{const value=cellular.value?.observedSince;return value&&!Number.isNaN(Date.parse(value))?new Date(value).toLocaleString('ja-JP',{month:'numeric',day:'numeric',hour:'2-digit',minute:'2-digit'}):null})
function inputGB(value:bigint){return `${value/1000000000n}${value%1000000000n?'.'+(value%1000000000n).toString().padStart(9,'0').replace(/0+$/,''):''}`}
function parseLimit(value:string){if(!/^(?:0|[1-9]\d{0,5})(?:\.\d{1,3})?$/.test(value.trim()))return null;const [whole,fraction='']=value.trim().split('.');const bytes=BigInt(whole)*1000000000n+BigInt(fraction.padEnd(9,'0'));return bytes<=100000000000000n?bytes.toString():null}
async function loadSettings(){settingsLoading.value=true;settingsError.value='';try{const value=await api<LimitSettings>('/settings');if(active)settings.value=value}catch(e){if(active)settingsError.value=e instanceof Error?e.message:String(e)}finally{if(active)settingsLoading.value=false}}
async function openSettings(){savedMessage.value='';formOpen.value=!formOpen.value;if(!formOpen.value)return;if(!settings.value)await loadSettings();draftLimit.value=limit.value!==null&&limit.value>0n?inputGB(limit.value):'';await nextTick();limitInput.value?.focus()}
async function saveLimit(){if(saving.value||!settings.value)return;const value=parseLimit(draftLimit.value);if(value===null){settingsError.value='0〜100,000 GBの範囲で、小数点以下3桁まで入力してください。';return}saving.value=true;settingsError.value='';savedMessage.value='';try{const updated=await api<LimitSettings>('/settings','PUT',{expectedRevision:settings.value.revision,settings:{...settings.value,cellularLimitBytes:value}});if(active){settings.value=updated;formOpen.value=false;savedMessage.value=value==='0'?'月間上限の設定を解除しました':'月間上限を保存しました'}}catch(e){if(active){const message=e instanceof Error?e.message:String(e);if(message.startsWith('409:')){await loadSettings();settingsError.value='別の操作で設定が変わりました。現在の設定を読み直しました。入力内容を確認して、もう一度保存してください。'}else settingsError.value=message}}finally{if(active)saving.value=false}}
async function refreshUsage(){if(inflight||document.hidden)return;inflight=true;try{const value=await api<Observation<Usage|null>>('/traffic/usage');if(active&&typeof value.status==='string'&&'data' in value){latest.value=value;pollError.value=false}}catch{if(active)pollError.value=true}finally{inflight=false}}
function visible(){if(!document.hidden){now.value=Date.now();refreshUsage()}}
onMounted(()=>{active=true;loadSettings();refreshUsage();timer=setInterval(()=>{now.value=Date.now();refreshUsage()},2000);document.addEventListener('visibilitychange',visible)})
onUnmounted(()=>{active=false;if(timer)clearInterval(timer);document.removeEventListener('visibilitychange',visible)})
</script>

<template>
 <section class="panel usage-panel traffic-usage" aria-labelledby="usage-heading">
  <div class="hero-head"><h2 id="usage-heading"><Radio :size="20"/>4G データ通信</h2><button v-if="showDetails" class="icon-button section-link" aria-label="通信量を開く" @click="emit('navigate','traffic')"><ExternalLink :size="18"/></button></div>
  <div class="traffic-plan">
   <div class="traffic-donut" role="img" :aria-label="!settings?'上限設定を取得中':!knownLimit?'4Gの月間上限は未設定':remaining===null?'通信量が未計測のため残量は不明':`設定上限${decimalGB(limit!,3)}GB、記録からの残り目安${decimalGB(remaining,3)}GB`">
    <svg viewBox="0 0 180 180" aria-hidden="true"><circle class="traffic-ring-track" cx="90" cy="90" r="74"/><circle v-if="remaining!==null" class="traffic-ring-value" cx="90" cy="90" r="74" :stroke="ringColor" :stroke-dasharray="ringCircumference" :stroke-dashoffset="ringCircumference*(1-remainingRatio)"/></svg>
    <div class="traffic-donut-center"><span>{{knownLimit?'記録からの残り目安':'月間上限'}}</span><strong v-if="!settings" class="traffic-donut-placeholder">{{settingsLoading?'取得中':'未取得'}}</strong><strong v-else-if="!knownLimit" class="traffic-donut-placeholder">未設定</strong><strong v-else data-testid="cellular-remaining">{{remaining===null?'—':decimalGB(remaining)}}<small>GB</small></strong><span v-if="knownLimit">/ {{decimalGB(limit!,limit!%1000000000n===0n?0:1)}} GB</span><span v-else>4Gのプランに合わせて設定</span></div>
   </div>
   <div class="traffic-plan-details"><div class="usage-month"><span>今月の記録分</span><strong data-testid="cellular-month">{{volume(monthBytes)}}</strong></div><div class="traffic-today"><span>本日の記録分</span><strong data-testid="traffic-total">{{volume(todayBytes)}}</strong></div><p v-if="excess>0n" class="traffic-limit-exceeded">設定上限を{{volume(excess)}}超過</p><button class="text-button traffic-limit-button" :disabled="saving||settingsLoading" @click="openSettings"><Settings2 :size="14"/>{{formOpen?'設定を閉じる':knownLimit?'月間上限を変更':'月間上限を設定'}}</button></div>
  </div>
  <form v-if="formOpen" class="traffic-limit-form" @submit.prevent="saveLimit"><label for="cellular-limit">4Gの月間上限 <span>1 GB = 1,000,000,000 B</span></label><div class="traffic-limit-input"><input id="cellular-limit" ref="limitInput" v-model="draftLimit" inputmode="decimal" autocomplete="off" placeholder="20" :disabled="saving||settingsLoading" aria-describedby="cellular-limit-hint"><span>GB</span><button class="primary compact" type="submit" :disabled="saving||!settings||settingsLoading">{{saving?'保存中…':'保存'}}</button></div><p id="cellular-limit-hint">毎月1日からの記録で集計します。上限で通信を停止する機能ではありません。</p><button class="text-button" type="button" :disabled="saving||!settings||settingsLoading" @click="draftLimit='0';saveLimit()">上限を設定しない</button></form>
  <p v-if="settingsError" class="traffic-feedback error" role="alert">{{settingsError}}<button v-if="!settings" class="text-button" @click="loadSettings">設定を再取得</button></p><p v-if="savedMessage" class="traffic-feedback" role="status">{{savedMessage}}</p>
  <div class="traffic-speed-heading"><span>現在の4G通信速度</span><span>{{speedState(cellular)}}</span></div><div class="traffic-speeds"><div><span><ArrowDown :size="16"/>下り</span><strong data-testid="cellular-download">{{speed(cellular,'rxBytesPerSecond')}}</strong></div><div><span><ArrowUp :size="16"/>上り</span><strong data-testid="cellular-upload">{{speed(cellular,'txBytesPerSecond')}}</strong></div></div>
  <div class="traffic-wifi"><div class="traffic-wifi-heading"><Wifi :size="17"/><strong>Wi-Fi STA</strong><span>{{speedState(wifi)}}</span></div><div class="traffic-wifi-counters"><span>本日 <b data-testid="wifi-today">{{volume(measured(wifi,'today'))}}</b></span><span>今月 <b data-testid="wifi-month">{{volume(measured(wifi,'month'))}}</b></span></div><div class="traffic-wifi-speeds"><span>↓ {{speed(wifi,'rxBytesPerSecond')}}</span><span>↑ {{speed(wifi,'txBytesPerSecond')}}</span></div></div>
  <p class="usage-note">{{since?`4G記録開始 ${since}。`:'4Gの観測をお待ちください。'}}残り目安は設定上限から記録分を引いた値で、通信会社の請求残量とは異なります。</p>
  <div class="hero-foot"><span :class="{'stale-label':stale}">{{stale?'更新が止まっています':'最終更新 '+time(sampledAt)}}</span><span>受信＋送信 · 観測できた通信のみ</span></div>
 </section>
</template>
