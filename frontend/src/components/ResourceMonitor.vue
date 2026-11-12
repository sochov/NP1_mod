<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch} from 'vue'
import {Activity,ExternalLink} from '@lucide/vue'
import {bytes,type Summary} from '../services/api'
import {useTraffic,trafficSpeed} from '../services/traffic'
import {chartPath,finite,remember,type MetricHistory} from '../services/resource-history'
const props=defineProps<{summary:Summary}>(),emit=defineEmits<{open:[]}>(),minutes=ref(5),now=ref(Date.now()),history=ref<MetricHistory>({})
const {cellular,wifi,sampledAt,stale}=useTraffic(()=>props.summary)
let timer:ReturnType<typeof setInterval>|undefined
const valid=(key:string)=>{const value=props.summary[key],at=Date.parse(value?.observedAt||'');return value?.status==='available'&&Number.isFinite(at)&&now.value-at<=30000&&at-now.value<=2000?value:undefined}
const percent=(value:unknown)=>finite(value)&&value>=0&&value<=100?value:null
const temperatures=computed(()=>{const values=valid('temperatures')?.data;return Array.isArray(values)?values.filter(t=>typeof t.name==='string'&&t.name.toLowerCase().includes('cpu')&&finite(t.celsius)&&t.celsius>=-40&&t.celsius<=150).map(t=>t.celsius):[]})
const metrics=computed(()=>[
 {key:'cpu',label:'CPU',value:percent(valid('cpu')?.data?.usedPercent),unit:'%',detail:'CPU使用率',max:100,at:valid('cpu')?.observedAt},
 {key:'ram',label:'RAM',value:percent(valid('ram')?.data?.usedPercent),unit:'%',detail:valid('ram')?`${bytes(valid('ram')!.data.usedBytes)} / ${bytes(valid('ram')!.data.totalBytes)}`:'使用状況未取得',max:100,at:valid('ram')?.observedAt},
 {key:'temperature',label:'本体温度',value:temperatures.value.length?Math.max(...temperatures.value):null,unit:'°C',detail:'CPU周辺センサー',max:150,at:valid('temperatures')?.observedAt}
])
const networks=computed(()=>[{key:'cellular',label:'4G',network:cellular.value},{key:'wifi',label:'Wi-Fi',network:wifi.value}])
function record(){now.value=Date.now();if(document.hidden)return
 for(const item of networks.value)for(const direction of ['rxBytesPerSecond','txBytesPerSecond'] as const){const n=item.network,value=!stale.value&&!n?.stale&&n?.speedStatus==='available'&&finite(n[direction])&&n[direction]>=0?n[direction]!:null;remember(history.value,item.key+direction,value===null?now.value:Date.parse(n?.sampledAt||sampledAt.value||'')||now.value,value)}
 for(const item of metrics.value){const at=Date.parse(item.at||'');remember(history.value,item.key,Number.isFinite(at)?at:now.value,item.value)}
}
watch([()=>props.summary,sampledAt,stale],record,{immediate:true})
onMounted(()=>timer=setInterval(record,2000));onUnmounted(()=>{if(timer)clearInterval(timer)})
const from=computed(()=>now.value-minutes.value*60000)
function maximum(keys:string[],fixed=0){if(fixed)return fixed;return Math.max(1,...keys.flatMap(key=>(history.value[key]||[]).filter(p=>p.at>=from.value&&p.value!==null).map(p=>p.value!)))*1.1}
function path(key:string,max:number){return chartPath(history.value[key]||[],from.value,now.value,max)}
function value(item:typeof metrics.value[number]){return item.value===null?'—':item.unit==='bytes'?bytes(item.value):item.value.toFixed(item.unit==='°C'?1:0)+' '+item.unit}
</script>
<template>
 <section class="panel resource-monitor" aria-label="リソースモニター"><div class="hero-head"><h2><Activity :size="20"/>リソースモニター</h2><div class="resource-toolbar"><select v-model.number="minutes" aria-label="リソースモニターの表示期間"><option :value="5">5分</option><option :value="15">15分</option><option :value="60">60分</option></select><button class="icon-button section-link" aria-label="システムを開く" @click="emit('open')"><ExternalLink :size="18"/></button></div></div>
  <div class="resource-networks"><article v-for="item in networks" :key="item.key" class="resource-network"><div class="resource-metric-heading"><h3>{{item.label}}</h3><div class="resource-transfers"><span class="resource-rx">↓ <strong>{{trafficSpeed(item.network,'rxBytesPerSecond',stale)}}</strong></span><span class="resource-tx">↑ <strong>{{trafficSpeed(item.network,'txBytesPerSecond',stale)}}</strong></span></div></div><svg viewBox="0 0 300 64" preserveAspectRatio="none" role="img" :aria-label="item.label+'の上り・下り速度の推移'"><path class="resource-gridline" d="M0 4H300M0 32H300M0 60H300"/><path class="resource-line resource-rx" :d="path(item.key+'rxBytesPerSecond',maximum([item.key+'rxBytesPerSecond',item.key+'txBytesPerSecond']))"/><path class="resource-line resource-tx" :d="path(item.key+'txBytesPerSecond',maximum([item.key+'rxBytesPerSecond',item.key+'txBytesPerSecond']))"/></svg></article></div>
  <div class="resource-device"><article v-for="item in metrics" :key="item.key" class="resource-metric" :data-metric="item.key"><div class="resource-metric-heading"><h3>{{item.label}}</h3><strong>{{value(item)}}</strong></div><small>{{item.detail}}</small><svg viewBox="0 0 300 64" preserveAspectRatio="none" role="img" :aria-label="item.label+'の推移'"><path class="resource-gridline" d="M0 4H300M0 32H300M0 60H300"/><path class="resource-line" :d="path(item.key,maximum([item.key],item.max))"/></svg></article></div>
 </section>
</template>
<style scoped>
.resource-monitor{overflow:hidden;min-width:0}.resource-toolbar{display:flex;align-items:center;gap:8px}.resource-toolbar select{min-height:34px;padding:5px 24px 5px 10px;font-size:12px;background:var(--paper);color:var(--text);border:1px solid var(--line);border-radius:5px}.resource-networks{display:grid;grid-template-columns:1fr 1fr;border-top:1px solid var(--line)}.resource-network{padding:16px 22px;min-width:0}.resource-network+.resource-network{border-left:1px solid var(--line)}.resource-metric-heading{display:flex;justify-content:space-between;align-items:baseline;gap:10px;flex-wrap:wrap}.resource-metric-heading h3{font-size:12px;font-weight:650;margin:0}.resource-metric-heading>strong{font-size:19px;font-weight:600;font-variant-numeric:tabular-nums}.resource-transfers{display:flex;gap:14px;font-size:12px;font-variant-numeric:tabular-nums;flex-wrap:wrap}.resource-transfers strong{font-weight:600}.resource-monitor svg[role=img]{display:block;width:100%;height:64px;margin-top:12px;overflow:visible}.resource-gridline{stroke:var(--line);stroke-width:1;fill:none;vector-effect:non-scaling-stroke}.resource-line{fill:none;stroke:var(--accent);stroke-width:2;stroke-linecap:round;stroke-linejoin:round;vector-effect:non-scaling-stroke}.resource-rx{color:var(--accent);stroke:var(--accent)}.resource-tx{color:var(--resource-upload);stroke:var(--resource-upload)}.resource-device{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));border-top:1px solid var(--line)}.resource-metric{padding:16px 18px;min-width:0}.resource-metric+.resource-metric{border-left:1px solid var(--line)}.resource-metric small{display:block;color:var(--muted);font-size:10px;line-height:1.6;min-height:32px;margin-top:7px}@media(max-width:1100px){.resource-device{grid-template-columns:repeat(3,minmax(0,1fr))}.resource-metric:nth-child(4){border-left:0}.resource-metric:nth-child(n+4){border-top:1px solid var(--line)}}@media(max-width:600px){.resource-network{padding:12px}.resource-network .resource-transfers{display:grid;gap:5px}.resource-device{grid-template-columns:1fr 1fr}.resource-metric:nth-child(odd){border-left:0}.resource-metric:nth-child(even){border-left:1px solid var(--line)}.resource-metric:nth-child(n+3){border-top:1px solid var(--line)}.resource-metric{padding:14px}.resource-metric-heading>strong{font-size:17px}.resource-toolbar{gap:2px}}
</style>
<style scoped>
.resource-monitor{align-self:start}.resource-network .resource-metric-heading{flex-direction:column;align-items:flex-start;gap:8px}.resource-network .resource-transfers{gap:6px 10px}
</style>
<style scoped>
.resource-monitor .hero-head{gap:8px;padding:16px 18px}.resource-monitor .hero-head h2{white-space:nowrap}.resource-toolbar .section-link{width:32px;min-width:32px;height:36px;min-height:36px}.resource-toolbar select{width:80px}@media(max-width:600px){.resource-monitor .hero-head{padding:14px}.resource-monitor .hero-head h2{font-size:14px;gap:8px}.resource-toolbar select{width:78px}.resource-toolbar .section-link{width:30px;min-width:30px}}
</style>
