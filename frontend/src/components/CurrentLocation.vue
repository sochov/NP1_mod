<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch} from 'vue'
import {MapPin} from '@lucide/vue'
import L from 'leaflet'
import {mapAttribution,mapTiles} from '../services/map-tiles'
import 'leaflet/dist/leaflet.css'
import '../radio-map.css'
import type {Observation} from '../services/api'

type Position={fix?:boolean;at?:string;lat?:number;lon?:number;satellites?:number;accuracyM?:number}
// `inset` is the part of a hero map hidden behind other panels, in pixels from each edge.
// `trail` is the recent driving track as separate line segments of [lat, lon].
const props=defineProps<{observation?:Observation<Position>;hero?:boolean;inset?:{top:number;right:number;bottom:number;left:number};trail?:[number,number][][]}>()
// The stock recorder writes positions when a clip closes (about every 3 minutes), so a
// position up to ~10 minutes old is shown, always with its age.
const MAX_AGE=11*60*1000
const container=ref<HTMLElement>(),now=ref(Date.now()),tileFailed=ref(false)
let zoom=15,map:L.Map|undefined,marker:L.CircleMarker|undefined,accuracy:L.Circle|undefined,trailLayer:L.LayerGroup|undefined,resize:ResizeObserver|undefined,timer:ReturnType<typeof setInterval>|undefined,mounted=false
const finite=(value:unknown):value is number=>typeof value==='number'&&Number.isFinite(value)
const current=computed(()=>{
 const gps=props.observation?.data,at=Date.parse(gps?.at||'')
 if(props.observation?.status!=='available'||gps?.fix!==true||!finite(gps.lat)||!finite(gps.lon)||Math.abs(gps.lat)>90||Math.abs(gps.lon)>180||!Number.isFinite(at)||now.value-at>MAX_AGE||at-now.value>2000)return undefined
 return gps
})
const age=computed(()=>current.value?Math.max(0,Math.round((now.value-Date.parse(current.value.at!))/1000)):0)
const ageText=computed(()=>!current.value||age.value<=20?'':age.value<60?`${age.value}秒前`:`${Math.floor(age.value/60)}分前`)
const lastTrail=computed<[number,number]|undefined>(()=>{const segments=props.trail||[];for(let i=segments.length-1;i>=0;i--){const seg=segments[i]!;if(seg.length)return seg[seg.length-1]}return undefined})
const shown=computed(()=>!!current.value||!!lastTrail.value)
const label=computed(()=>current.value?'現在地':props.observation?.data?.fix===false?'測位待ち':props.observation?.data?.fix===true?'現在地を更新できません':'位置情報を取得できません')
const insetStyle=computed(()=>props.inset?{'--hero-pad-top':props.inset.top+'px','--hero-pad-right':props.inset.right+'px','--hero-pad-bottom':props.inset.bottom+'px','--hero-pad-left':props.inset.left+'px'}:undefined)
// A hero map is drawn larger than its frame, past the edge opposite the wider covered side, so that the
// middle of the drawing is the middle of the uncovered part. The position then stays put while zooming.
const canvasStyle=computed(()=>{
 const hidden=props.hero?props.inset:undefined;if(!hidden)return undefined
 const x=hidden.left-hidden.right,y=hidden.top-hidden.bottom
 return {left:Math.min(0,x)+'px',right:Math.min(0,-x)+'px',top:Math.min(0,y)+'px',bottom:Math.min(0,-y)+'px'}
})
function centre(point:L.LatLngTuple){map?.setView(point,map.getZoom(),{animate:false})}
function clear(){resize?.disconnect();resize=undefined;map?.remove();map=undefined;marker=undefined;accuracy=undefined;trailLayer=undefined}
function anchor():L.LatLngTuple|undefined{const gps=current.value;return gps?[gps.lat!,gps.lon!]:lastTrail.value}
function drawTrail(){if(!map)return;trailLayer?.remove();trailLayer=L.layerGroup();for(const seg of props.trail||[])if(seg.length>1)L.polyline(seg,{color:'#2f5bd8',weight:4,opacity:.75,interactive:false,className:'current-location-trail'}).addTo(trailLayer);trailLayer.addTo(map)}
function update(){
 if(!mounted||!container.value)return
 const gps=current.value,point=anchor()
 if(!point){clear();return}
 if(!map){
  tileFailed.value=false
  // The map follows the vehicle, so it is never dragged. A hero map can be zoomed about the position:
  // with the wheel on a PC, by pinching (or double-tapping) on a touch screen.
  map=props.hero?L.map(container.value,{zoomControl:false,attributionControl:false,scrollWheelZoom:'center',touchZoom:'center',doubleClickZoom:'center',dragging:false,keyboard:false,minZoom:4,maxZoom:19}).setView(point,zoom):L.map(container.value,{zoomControl:true,scrollWheelZoom:false,dragging:false,keyboard:false,doubleClickZoom:false}).setView(point,15)
  map.attributionControl?.setPrefix(false)
  if(props.hero)map.on('zoomend',()=>{if(map)zoom=map.getZoom()})
  mapTiles(()=>tileFailed.value=true).addTo(map)
  resize=new ResizeObserver(()=>{map?.invalidateSize({pan:false});const p=anchor();if(p)centre(p)});resize.observe(container.value)
  drawTrail()
 }
 // The marker is the newest recorded position; without one, the end of the trail is shown greyed.
 const colour=gps?'#397fd7':'#8a94a3'
 if(!marker)marker=L.circleMarker(point,{radius:8,color:'#fff',weight:3,fillColor:colour,fillOpacity:1,className:gps?'current-location-marker':'current-location-last'}).addTo(map).bindTooltip(gps?'現在地':'最後の記録位置')
 else{marker.setLatLng(point);marker.setStyle({fillColor:colour})}
 accuracy?.remove();accuracy=undefined
 if(gps&&finite(gps.accuracyM)&&gps.accuracyM>0)accuracy=L.circle(point,{radius:gps.accuracyM,color:'#397fd7',weight:1,fillOpacity:.1,interactive:false}).addTo(map)
 centre(point)
}
watch(current,update,{flush:'post'})
watch(()=>props.trail,()=>{if(map)drawTrail();else update()},{flush:'post'})
onMounted(()=>{mounted=true;timer=setInterval(()=>now.value=Date.now(),1000);update()})
onUnmounted(()=>{mounted=false;if(timer)clearInterval(timer);clear()})
</script>

<template>
 <div class="current-location radio-survey" :class="{hero}" :style="insetStyle" role="region" aria-label="現在地の地図">
  <div class="current-location-frame">
   <div ref="container" class="current-location-canvas radio-map-canvas" :class="{unavailable:!shown}" :style="canvasStyle" aria-label="現在地を示す地図"></div>
   <span v-if="current" class="current-location-label"><i></i>現在地</span>
   <div v-else-if="!shown" class="current-location-empty"><MapPin :size="28" stroke-width="1.5"/><strong>{{label}}</strong><p>測位すると、ここに現在地の地図を表示します。</p></div>
   <!-- On a hero map the readings sit at the bottom left and the map credit at the bottom right, on one line. -->
   <div class="current-location-foot">
   <div class="current-location-details">
    <div class="current-location-coordinates" aria-label="現在地の緯度経度"><span v-if="hero">{{current?`${current.lat!.toFixed(6)}, ${current.lon!.toFixed(6)}`:lastTrail?'最後の記録位置':'—'}}</span><template v-else><span>緯度 {{current?.lat?.toFixed(6)??'—'}}</span><span>経度 {{current?.lon?.toFixed(6)??'—'}}</span></template></div>
    <div class="current-location-quality"><span>衛星 {{finite(observation?.data?.satellites)?observation?.data?.satellites:'—'}}</span><span>精度 {{current && finite(current.accuracyM) && current.accuracyM>0?`${Math.round(current.accuracyM)} m`:'—'}}</span><span v-if="ageText" class="current-location-age">{{ageText}}の位置</span><span v-if="current&&!hero" class="current-location-time">{{new Date(current.at!).toLocaleTimeString('ja-JP',{hour:'2-digit',minute:'2-digit',second:'2-digit'})}}</span></div>
   </div>
   <span v-if="hero&&shown" class="current-location-credit" v-html="mapAttribution"></span>
   </div>
   <p v-if="shown&&tileFailed" class="current-location-note">背景地図を取得できません</p>
  </div>
 </div>
</template>

<style scoped>
.current-location.radio-survey{padding:0}.current-location-frame{height:276px;border-block:1px solid var(--line);position:relative;overflow:hidden}.current-location-canvas.radio-map-canvas{width:100%;height:100%;min-height:0}.current-location-canvas.unavailable{visibility:hidden}.current-location-label{position:absolute;top:12px;left:58px;z-index:450;display:flex;align-items:center;gap:7px;padding:7px 10px;border:1px solid var(--line);border-radius:5px;background:var(--paper);color:var(--text);font-size:12px;font-weight:650;box-shadow:0 1px 6px #00000018;pointer-events:none}.current-location-label i{width:7px;height:7px;border-radius:50%;background:var(--accent)}.current-location-empty{position:absolute;inset:0;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:10px;padding:18px 18px 80px;text-align:center;background-color:var(--bg);background-image:linear-gradient(var(--line) 1px,transparent 1px),linear-gradient(90deg,var(--line) 1px,transparent 1px);background-size:44px 44px;color:var(--muted)}.current-location-empty strong{color:var(--text);font-size:15px;font-weight:600}.current-location-empty p{font-size:12px;line-height:1.7;margin:0}.current-location-details{position:absolute;left:12px;right:12px;bottom:24px;z-index:450;display:flex;flex-wrap:wrap;justify-content:space-between;gap:6px 16px;padding:9px 11px;border:1px solid var(--line);border-radius:6px;background:color-mix(in srgb,var(--paper) 94%,transparent);color:var(--text);font-size:11px;line-height:1.7;font-variant-numeric:tabular-nums;box-shadow:0 1px 6px #00000018;pointer-events:none}.current-location-coordinates,.current-location-quality{display:flex;gap:6px 14px;flex-wrap:wrap}.current-location-coordinates span,.current-location-quality span{white-space:nowrap}.current-location-quality{color:var(--muted)}.current-location-time{margin-left:auto}.current-location-note{position:absolute;top:12px;right:12px;z-index:450;max-width:calc(100% - 170px);padding:6px 9px;border:1px solid var(--line);border-radius:5px;background:var(--paper);font-size:11px;line-height:1.7;color:var(--muted);margin:0;pointer-events:none}.current-location.hero{position:absolute;inset:0}.hero .current-location-frame{height:100%;border:0}.hero .current-location-label{display:none}.hero .current-location-empty{padding:calc(var(--hero-pad-top,0px) + 18px) calc(var(--hero-pad-right,0px) + 18px) calc(var(--hero-pad-bottom,0px) + 18px) calc(var(--hero-pad-left,0px) + 18px)}.hero .current-location-canvas.radio-map-canvas{position:absolute;inset:0;width:auto;height:auto}.current-location-foot{display:contents}.hero .current-location-foot{position:absolute;left:calc(16px - var(--hero-credit-lift,0)*8px);right:calc(16px - var(--hero-credit-lift,0)*8px);bottom:var(--dh-foot,calc(4px + var(--hero-credit-lift,0)*(var(--hero-pad-bottom,0px) + 2px)));z-index:450;display:flex;align-items:flex-end;justify-content:space-between;gap:8px;pointer-events:none}.hero .current-location-details,.hero .current-location-credit{border:0;border-radius:6px;padding:1px 8px;background:color-mix(in srgb,var(--paper) 62%,transparent);backdrop-filter:blur(6px) saturate(1.3);-webkit-backdrop-filter:blur(6px) saturate(1.3);box-shadow:none;font-size:11px;line-height:1.6}.hero .current-location-details{position:static;flex:0 1 auto;min-width:0;justify-content:flex-start;gap:0 12px}.hero .current-location-credit{flex:none;margin-left:auto;color:var(--muted);pointer-events:auto}.hero .current-location-credit :deep(a){color:var(--accent)}.hero .current-location-quality{width:auto;color:var(--text);opacity:.72}.current-location-age{color:#b45309;font-weight:650}.hero .current-location-note{top:calc(var(--hero-pad-top,0px) + 12px);right:calc(var(--hero-pad-right,0px) + 12px)}
@media(max-width:520px){.current-location-frame{height:260px}.current-location-empty p{max-width:220px}.current-location-details{gap:3px 12px}.current-location-quality{width:100%}}
</style>
