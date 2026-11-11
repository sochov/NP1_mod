import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api, type Observation, type Summary } from './api'

export type NetworkUsage={interface?:string;todayRX:string|null;todayTX:string|null;monthRX:string|null;monthTX:string|null;rxBytesPerSecond:number|null;txBytesPerSecond:number|null;sampledAt:string|null;observedSince:string|null;status:string;speedStatus:string;active:boolean;stale:boolean}
type Usage={networks?:{cellular?:NetworkUsage;wifi?:NetworkUsage};sampledAt?:string;stale?:boolean}
const latest=ref<Observation<Usage|null>>(),failed=ref(false),now=ref(Date.now())
let users=0,generation=0,inflight=false,timer:ReturnType<typeof setInterval>|undefined
async function refresh(){
 if(inflight||document.hidden||!users)return
 inflight=true;const current=generation
 try{const value=await api<Observation<Usage|null>>('/traffic/usage');if(current!==generation||!users)return;if(typeof value.status!=='string'||!('data' in value))return;latest.value=value;failed.value=false}catch{if(current===generation&&users)failed.value=true}finally{inflight=false}
}
function visible(){if(!document.hidden){now.value=Date.now();refresh()}}
export function useTraffic(summary:()=>Summary){
 const observation=computed(()=>{const a=latest.value,b=summary().usage;if(!a)return b;if(!b)return a;return (Date.parse(a.data?.sampledAt||a.observedAt)||0)>=(Date.parse(b.data?.sampledAt||b.observedAt)||0)?a:b})
 const usage=computed<Usage|undefined>(()=>['available','stale'].includes(observation.value?.status)?observation.value?.data||undefined:undefined)
 const sampledAt=computed(()=>usage.value?.sampledAt||observation.value?.observedAt)
 const stale=computed(()=>!!(failed.value||usage.value?.stale||observation.value?.status==='stale'||(sampledAt.value&&now.value-Date.parse(sampledAt.value)>10000)))
 onMounted(()=>{if(users++===0){generation++;now.value=Date.now();refresh();timer=setInterval(()=>{now.value=Date.now();refresh()},2000);document.addEventListener('visibilitychange',visible)}})
 onUnmounted(()=>{if(--users===0){generation++;if(timer)clearInterval(timer);timer=undefined;document.removeEventListener('visibilitychange',visible);latest.value=undefined;failed.value=false}})
 return {usage,sampledAt,stale,cellular:computed(()=>usage.value?.networks?.cellular),wifi:computed(()=>usage.value?.networks?.wifi)}
}
export function trafficInteger(value:unknown){return typeof value==='string'&&/^\d+$/.test(value)?BigInt(value):null}
export function networkTotal(network:NetworkUsage|undefined,period:'today'|'month'){
 if(network?.status!=='available')return null
 const rx=trafficInteger(network[period==='today'?'todayRX':'monthRX']),tx=trafficInteger(network[period==='today'?'todayTX':'monthTX'])
 return rx===null||tx===null?null:rx+tx
}
export function trafficVolume(value:bigint|null){if(value===null)return '—';for(const [unit,size] of [['TB',1000000000000n],['GB',1000000000n],['MB',1000000n],['KB',1000n]] as const){if(value>=size)return `${(Number((value*10n+size/2n)/size)/10).toLocaleString('ja-JP',{maximumFractionDigits:1})} ${unit}`}return `${value.toLocaleString('ja-JP')} B`}
export function trafficSpeed(network:NetworkUsage|undefined,key:'rxBytesPerSecond'|'txBytesPerSecond',stale:boolean){const value=network?.[key];if(stale||network?.stale||network?.speedStatus!=='available'||typeof value!=='number'||!Number.isFinite(value)||value<0)return '—';const bits=value*8;if(!Number.isFinite(bits))return '—';for(const [unit,scale] of [['Gbps',1e9],['Mbps',1e6],['kbps',1e3]] as const){if(bits>=scale)return `${(bits/scale).toLocaleString('ja-JP',{minimumFractionDigits:bits/scale<10?1:0,maximumFractionDigits:1})} ${unit}`}return `${Math.round(bits).toLocaleString('ja-JP')} bps`}
