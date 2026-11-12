<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch} from 'vue'
import {MapPin} from '@lucide/vue'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import '../radio-map.css'
import type {Observation} from '../services/api'

type Position={fix?:boolean;at?:string;lat?:number;lon?:number;satellites?:number;accuracyM?:number}
const props=defineProps<{observation?:Observation<Position>}>()
const container=ref<HTMLElement>(),now=ref(Date.now()),tileFailed=ref(false)
let map:L.Map|undefined,marker:L.CircleMarker|undefined,accuracy:L.Circle|undefined,resize:ResizeObserver|undefined,timer:ReturnType<typeof setInterval>|undefined,mounted=false
const finite=(value:unknown):value is number=>typeof value==='number'&&Number.isFinite(value)
const current=computed(()=>{
 const gps=props.observation?.data,at=Date.parse(gps?.at||'')
 if(props.observation?.status!=='available'||gps?.fix!==true||!finite(gps.lat)||!finite(gps.lon)||Math.abs(gps.lat)>90||Math.abs(gps.lon)>180||!Number.isFinite(at)||now.value-at>17000||at-now.value>2000)return undefined
 return gps
})
const label=computed(()=>current.value?'現在地':props.observation?.data?.fix===false?'測位待ち':props.observation?.data?.fix===true?'現在地を更新できません':'位置情報を取得できません')
function clear(){resize?.disconnect();resize=undefined;map?.remove();map=undefined;marker=undefined;accuracy=undefined}
function update(){
 if(!mounted||!container.value)return
 const gps=current.value
 if(!gps){clear();return}
 const point:L.LatLngTuple=[gps.lat!,gps.lon!]
 if(!map){
  tileFailed.value=false
  map=L.map(container.value,{scrollWheelZoom:false,dragging:false,keyboard:false,doubleClickZoom:false}).setView(point,15)
  map.attributionControl.setPrefix(false)
  L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png',{maxZoom:19,attribution:'© <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener">OpenStreetMap</a>'}).on('tileerror',()=>tileFailed.value=true).addTo(map)
  resize=new ResizeObserver(()=>map?.invalidateSize());resize.observe(container.value)
 }
 if(!marker)marker=L.circleMarker(point,{radius:8,color:'#fff',weight:3,fillColor:'#397fd7',fillOpacity:1,className:'current-location-marker'}).addTo(map).bindTooltip('現在地')
 else marker.setLatLng(point)
 accuracy?.remove();accuracy=undefined
 if(finite(gps.accuracyM)&&gps.accuracyM>0)accuracy=L.circle(point,{radius:gps.accuracyM,color:'#397fd7',weight:1,fillOpacity:.1,interactive:false}).addTo(map)
 map.setView(point,map.getZoom(),{animate:false})
}
watch(current,update,{flush:'post'})
onMounted(()=>{mounted=true;timer=setInterval(()=>now.value=Date.now(),1000);update()})
onUnmounted(()=>{mounted=false;if(timer)clearInterval(timer);clear()})
</script>

<template>
 <div class="current-location radio-survey" role="region" aria-label="現在地の地図">
  <div class="current-location-frame">
   <div ref="container" class="current-location-canvas radio-map-canvas" :class="{unavailable:!current}" aria-label="現在地を示す地図"></div>
   <span v-if="current" class="current-location-label"><i></i>現在地</span>
   <div v-else class="current-location-empty"><MapPin :size="28" stroke-width="1.5"/><strong>{{label}}</strong><p>測位すると、ここに現在地の地図を表示します。</p></div>
  </div>
  <div class="current-location-details"><span>衛星 {{finite(observation?.data?.satellites)?observation?.data?.satellites:'—'}}</span><span>精度 {{current && finite(current.accuracyM) && current.accuracyM>0?`${Math.round(current.accuracyM)} m`:'—'}}</span><span v-if="current" class="current-location-time">{{new Date(current.at!).toLocaleTimeString('ja-JP',{hour:'2-digit',minute:'2-digit',second:'2-digit'})}}</span></div>
  <p v-if="current&&tileFailed" class="current-location-note">背景地図を取得できません。接続を確認してください。</p>
 </div>
</template>

<style scoped>
.current-location.radio-survey{padding:0 23px 14px}.current-location-frame{height:220px;border:1px solid var(--line);border-radius:8px;position:relative;overflow:hidden}.current-location-canvas.radio-map-canvas{width:100%;height:100%;min-height:0}.current-location-canvas.unavailable{visibility:hidden}.current-location-label{position:absolute;top:12px;left:58px;z-index:450;display:flex;align-items:center;gap:7px;padding:7px 10px;border:1px solid var(--line);border-radius:5px;background:var(--paper);color:var(--text);font-size:12px;font-weight:650;box-shadow:0 1px 6px #00000018;pointer-events:none}.current-location-label i{width:7px;height:7px;border-radius:50%;background:var(--accent)}.current-location-empty{position:absolute;inset:0;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:10px;padding:18px;text-align:center;background-color:var(--bg);background-image:linear-gradient(var(--line) 1px,transparent 1px),linear-gradient(90deg,var(--line) 1px,transparent 1px);background-size:44px 44px;color:var(--muted)}.current-location-empty strong{color:var(--text);font-size:15px;font-weight:600}.current-location-empty p{font-size:12px;line-height:1.7;margin:0}.current-location-details{display:flex;flex-wrap:wrap;gap:8px 16px;margin-top:10px;font-size:11px;color:var(--muted);font-variant-numeric:tabular-nums}.current-location-time{margin-left:auto}.current-location-note{font-size:11px;line-height:1.7;color:var(--muted);margin:8px 0 0}@media(max-width:520px){.current-location.radio-survey{padding:0 20px 14px}.current-location-frame{height:210px}.current-location-empty p{max-width:200px}}
</style>
