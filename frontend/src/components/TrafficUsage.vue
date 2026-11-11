<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { ArrowDown, ArrowUp, ExternalLink, Radio, Settings2 } from '@lucide/vue'
import { api, type Settings, type Summary } from '../services/api'
import { useTraffic,trafficInteger as integer,networkTotal as measured,trafficVolume as volume,trafficSpeed,type NetworkUsage } from '../services/traffic'

type LimitSettings=Settings&{cellularLimitBytes?:string}
const props=withDefaults(defineProps<{summary:Summary;showDetails?:boolean}>(),{showDetails:true})
const emit=defineEmits<{navigate:[page:string]}>()
const settings=ref<LimitSettings>(),settingsLoading=ref(false),saving=ref(false),formOpen=ref(false),draftLimit=ref(''),settingsError=ref(''),savedMessage=ref(''),limitInput=ref<HTMLInputElement>()
let active=true
const {cellular,sampledAt,stale}=useTraffic(()=>props.summary)
const modem=computed(()=>['available','stale'].includes(props.summary.modem?.status)?props.summary.modem.data:undefined)
// ITU E.212 bulletin 1280 (2023); PLMN identifies the network, never an MVNO plan.
const networkNames:Record<string,string>={'44010':'NTT docomo','44020':'SoftBank','44021':'SoftBank','44050':'KDDI','44051':'KDDI','44052':'KDDI','44053':'KDDI','44054':'KDDI','44011':'Rakuten Mobile','44003':'IIJ'}
const carrier=computed(()=>modem.value?.carrier||networkNames[modem.value?.plmn]||(modem.value?.plmn?`PLMN ${modem.value.plmn}`:'回線情報未取得'))
const registrationLabel=computed(()=>modem.value?.registered===true?'回線登録済み':modem.value?.registered===false?'未登録':'状態未取得')
const rsrp=computed(()=>typeof modem.value?.rsrp==='number'&&Number.isFinite(modem.value.rsrp)?modem.value.rsrp:null)
const signalLevel=computed(()=>rsrp.value===null?null:rsrp.value>=-80?5:rsrp.value>=-90?4:rsrp.value>=-100?3:rsrp.value>=-110?2:1)
const signalLabel=computed(()=>rsrp.value===null?'未取得':rsrp.value>=-90?'良好':rsrp.value>=-105?'標準':'弱い')
const bandLabel=computed(()=>typeof modem.value?.band==='string'&&/^B?\d+$/.test(modem.value.band)?'LTE Band '+modem.value.band.replace(/^B/,''):modem.value?.band||'—')
const wanIP=computed(()=>{const base=cellular.value?.interface,interfaces=props.summary.interfaces?.data;if(!base||!Array.isArray(interfaces))return '—';const addresses=interfaces.filter(i=>(i.ifname===base||i.ifname?.startsWith(base+'.'))&&i.flags?.includes('UP')).flatMap(i=>Array.isArray(i.addr_info)?i.addr_info:[]).filter(a=>a.family==='inet'&&a.scope==='global'&&typeof a.local==='string').map(a=>a.local);return [...new Set(addresses)].join(' / ')||'—'})
const monthBytes=computed(()=>measured(cellular.value,'month')),todayBytes=computed(()=>measured(cellular.value,'today'))
const limit=computed(()=>settings.value?integer(settings.value.cellularLimitBytes??'0'):null)
const remaining=computed(()=>limit.value!==null&&limit.value>0n&&monthBytes.value!==null?(monthBytes.value>=limit.value?0n:limit.value-monthBytes.value):null)
const excess=computed(()=>limit.value!==null&&limit.value>0n&&monthBytes.value!==null&&monthBytes.value>limit.value?monthBytes.value-limit.value:0n)
const remainingRatio=computed(()=>remaining.value!==null&&limit.value?Number(remaining.value*10000n/limit.value)/10000:0)
const ringCircumference=2*Math.PI*74
const ringColor=computed(()=>remaining.value===null?'var(--line)':remainingRatio.value<=.15?'var(--danger)':remainingRatio.value<=.3?'#bf8a36':'var(--accent)')
const knownLimit=computed(()=>limit.value!==null&&limit.value>0n)
function decimalGB(value:bigint,precision=1){const scale=10n**BigInt(precision),rounded=(value*scale+500000000n)/1000000000n;return (Number(rounded)/Number(scale)).toLocaleString('ja-JP',{minimumFractionDigits:precision,maximumFractionDigits:precision})}
function speed(network:NetworkUsage|undefined,key:'rxBytesPerSecond'|'txBytesPerSecond'){return trafficSpeed(network,key,stale.value)}
function time(value:string|undefined|null){if(!value||Number.isNaN(Date.parse(value)))return '—';return new Date(value).toLocaleTimeString('ja-JP',{hour:'2-digit',minute:'2-digit',second:'2-digit',hourCycle:'h23'})}
function inputGB(value:bigint){return `${value/1000000000n}${value%1000000000n?'.'+(value%1000000000n).toString().padStart(9,'0').replace(/0+$/,''):''}`}
function parseLimit(value:string){if(!/^(?:0|[1-9]\d{0,5})(?:\.\d{1,3})?$/.test(value.trim()))return null;const [whole,fraction='']=value.trim().split('.');const bytes=BigInt(whole)*1000000000n+BigInt(fraction.padEnd(9,'0'));return bytes<=100000000000000n?bytes.toString():null}
async function loadSettings(){settingsLoading.value=true;settingsError.value='';try{const value=await api<LimitSettings>('/settings');if(active)settings.value=value}catch(e){if(active)settingsError.value=e instanceof Error?e.message:String(e)}finally{if(active)settingsLoading.value=false}}
async function openSettings(){savedMessage.value='';formOpen.value=!formOpen.value;if(!formOpen.value)return;if(!settings.value)await loadSettings();draftLimit.value=limit.value!==null&&limit.value>0n?inputGB(limit.value):'';await nextTick();limitInput.value?.focus()}
async function saveLimit(){if(saving.value||!settings.value)return;const value=parseLimit(draftLimit.value);if(value===null){settingsError.value='0〜100,000 GBの範囲で、小数点以下3桁まで入力してください。';return}saving.value=true;settingsError.value='';savedMessage.value='';try{const updated=await api<LimitSettings>('/settings','PUT',{expectedRevision:settings.value.revision,settings:{...settings.value,cellularLimitBytes:value}});if(active){settings.value=updated;formOpen.value=false;savedMessage.value=value==='0'?'月間上限の設定を解除しました':'月間上限を保存しました'}}catch(e){if(active){const message=e instanceof Error?e.message:String(e);if(message.startsWith('409:')){await loadSettings();settingsError.value='別の操作で設定が変わりました。現在の設定を読み直しました。入力内容を確認して、もう一度保存してください。'}else settingsError.value=message}}finally{if(active)saving.value=false}}
onMounted(()=>{active=true;loadSettings()})
onUnmounted(()=>{active=false})
</script>

<template>
 <section class="panel usage-panel traffic-usage" aria-labelledby="usage-heading">
  <div class="hero-head"><h2 id="usage-heading"><Radio :size="20"/>4G データ通信</h2><button v-if="showDetails" class="icon-button section-link" aria-label="通信量を開く" @click="emit('navigate','internet')"><ExternalLink :size="18"/></button></div>
  <div class="cellular-info">
   <div class="cellular-carrier"><div><span>通信キャリア / APN <small class="cellular-registration">{{registrationLabel}}</small></span><strong data-testid="cellular-carrier">{{carrier}}<small v-if="modem?.apn">{{modem.apn}}</small></strong></div><div class="cellular-bars" role="img" :aria-label="rsrp===null?'電波強度未取得':`RSRP ${rsrp} dBm`"><i v-for="n in 5" :key="n" :class="{lit:signalLevel!==null&&n<=signalLevel}" :style="{height:(6+n*5)+'px'}"></i></div></div>
   <dl class="cellular-details"><div><dt>接続バンド</dt><dd class="cellular-band">{{bandLabel}}</dd></div><div><dt>電波品質 <span>(RSRP)</span></dt><dd>{{rsrp===null?'—':`${rsrp} dBm`}} <span v-if="rsrp!==null" :class="{'signal-good':signalLabel==='良好'}">{{signalLabel}}</span></dd></div><div><dt>WAN IP</dt><dd class="mono">{{wanIP}}</dd></div></dl>
  </div>
  <div class="traffic-plan">
   <div class="traffic-donut" role="img" :aria-label="!settings?'上限設定を取得中':!knownLimit?'4Gの月間上限は未設定':remaining===null?'通信量が未計測のため残量は不明':`設定上限${decimalGB(limit!,3)}GB、記録からの残り目安${decimalGB(remaining,3)}GB`">
    <svg viewBox="0 0 180 180" aria-hidden="true"><circle class="traffic-ring-track" cx="90" cy="90" r="74"/><circle v-if="remaining!==null" class="traffic-ring-value" cx="90" cy="90" r="74" :stroke="ringColor" :stroke-dasharray="ringCircumference" :stroke-dashoffset="ringCircumference*(1-remainingRatio)"/></svg>
    <div class="traffic-donut-center"><span>{{knownLimit?'残り':'月間上限'}}</span><strong v-if="!settings" class="traffic-donut-placeholder">{{settingsLoading?'取得中':'未取得'}}</strong><strong v-else-if="!knownLimit" class="traffic-donut-placeholder">未設定</strong><strong v-else data-testid="cellular-remaining">{{remaining===null?'—':decimalGB(remaining)}}<small>GB</small></strong><span v-if="knownLimit">/ {{decimalGB(limit!,limit!%1000000000n===0n?0:1)}} GB</span><span v-else>4Gのプランに合わせて設定</span></div>
   </div>
   <div class="traffic-plan-details"><div class="usage-month"><span>今月の記録分</span><strong data-testid="cellular-month">{{volume(monthBytes)}}</strong></div><div class="traffic-today"><span>本日の記録分</span><strong data-testid="traffic-total">{{volume(todayBytes)}}</strong></div><p v-if="excess>0n" class="traffic-limit-exceeded">設定上限を{{volume(excess)}}超過</p><button class="text-button traffic-limit-button" :disabled="saving||settingsLoading" @click="openSettings"><Settings2 :size="14"/>{{formOpen?'設定を閉じる':knownLimit?'月間上限を変更':'月間上限を設定'}}</button></div>
  </div>
  <form v-if="formOpen" class="traffic-limit-form" @submit.prevent="saveLimit"><label for="cellular-limit">4Gの月間上限 <span>1 GB = 1,000,000,000 B</span></label><div class="traffic-limit-input"><input id="cellular-limit" ref="limitInput" v-model="draftLimit" inputmode="decimal" autocomplete="off" placeholder="20" :disabled="saving||settingsLoading" aria-describedby="cellular-limit-hint"><span>GB</span><button class="primary compact" type="submit" :disabled="saving||!settings||settingsLoading">{{saving?'保存中…':'保存'}}</button></div><p id="cellular-limit-hint">毎月1日からの記録で集計します。上限で通信を停止する機能ではありません。</p><button class="text-button" type="button" :disabled="saving||!settings||settingsLoading" @click="draftLimit='0';saveLimit()">上限を設定しない</button></form>
  <p v-if="settingsError" class="traffic-feedback error" role="alert">{{settingsError}}<button v-if="!settings" class="text-button" @click="loadSettings">設定を再取得</button></p><p v-if="savedMessage" class="traffic-feedback" role="status">{{savedMessage}}</p>
  <div class="traffic-speeds"><div><span><ArrowDown :size="16"/>下り</span><strong data-testid="cellular-download">{{speed(cellular,'rxBytesPerSecond')}}</strong></div><div><span><ArrowUp :size="16"/>上り</span><strong data-testid="cellular-upload">{{speed(cellular,'txBytesPerSecond')}}</strong></div></div>
  <div class="hero-foot"><span :class="{'stale-label':stale}">{{stale?'更新が止まっています':'最終更新 '+time(sampledAt)}}</span><span>受信＋送信 · 観測できた通信のみ</span></div>
 </section>
</template>
