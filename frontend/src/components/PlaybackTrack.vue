<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch} from 'vue'
import L from 'leaflet'
import {LocateFixed,MapPin,Maximize,Play} from '@lucide/vue'
import 'leaflet/dist/leaflet.css'
import '../radio-map.css'
import {api,type Observation} from '../services/api'
import {hasRecording,nearestPoint,pointAt,type PlaybackTrack,type RecordedRange,type TrackPoint,type TrackSegment} from '../services/playbackTrack'

const props=defineProps<{date:string;cursor:number;ranges:RecordedRange[];refresh?:string}>(),emit=defineEmits<{seek:[at:number]}>()
const container=ref<HTMLElement>(),data=ref<PlaybackTrack>(),loading=ref(false),error=ref(''),notice=ref(''),follow=ref(true),background=ref(true),tileFailed=ref(false),shown=ref(8)
let map:L.Map|undefined,layers:L.LayerGroup|undefined,marker:L.CircleMarker|undefined,tiles:L.TileLayer|undefined,resize:ResizeObserver|undefined,request:AbortController|undefined,sequence=0,fittedDate=''
const current=computed(()=>pointAt(data.value?.points||[],props.cursor)),segments=computed(()=>data.value?.segments||[])
const clock=(at:string)=>new Date(at).toLocaleTimeString('ja-JP',{timeZone:'Asia/Tokyo',hour:'2-digit',minute:'2-digit',second:'2-digit'})
function elapsed(seconds:number){const minutes=Math.floor(seconds/60);return minutes>=60?`${Math.floor(minutes/60)}時間${minutes%60}分`:`${minutes}分${Math.floor(seconds%60)}秒`}
function distance(km:number){return km<1?Math.round(km*1000)+' m':km.toFixed(1)+' km'}
function playable(segment:TrackSegment){return data.value?.points.find(point=>point.segment===segment.id&&hasRecording(props.ranges,Date.parse(point.at)))}
function seek(point?:TrackPoint){if(!point)return;const at=Date.parse(point.at);if(hasRecording(props.ranges,at)){notice.value='';emit('seek',at)}else notice.value='この地点の時刻に再生できる録画はありません。'}
async function load(){const id=++sequence;request?.abort();request=new AbortController();loading.value=true;error.value='';notice.value=''
 if(data.value?.date!==props.date){data.value=undefined;layers?.clearLayers();marker?.remove();marker=undefined;shown.value=8}
 try{const result=await api<Observation<PlaybackTrack>>('/recordings/track?'+new URLSearchParams({date:props.date}),'GET',undefined,true,request.signal);if(id!==sequence)return;if(!result.data||!Array.isArray(result.data.points)||!Array.isArray(result.data.segments))throw new Error('位置記録の形式を確認できません');data.value=result.data;render()}
 catch(e){if(id===sequence&&!request?.signal.aborted){error.value=e instanceof Error?e.message:'位置記録を取得できません';data.value=undefined;layers?.clearLayers();marker?.remove();marker=undefined}}
 finally{if(id===sequence)loading.value=false}
}
function render(){if(!map||!layers)return;layers.clearLayers();const groups=new Map<number,TrackPoint[]>();for(const point of data.value?.points||[]){const group=groups.get(point.segment)||[];group.push(point);groups.set(point.segment,group)}
 for(const [segment,points] of groups){const path=points.length>1?L.polyline(points.map(point=>[point.lat,point.lon]),{color:'#4f91df',weight:5,opacity:.85,className:'playback-route'}):L.circleMarker([points[0].lat,points[0].lon],{color:'#4f91df',radius:4,className:'playback-route'})
  path.on('click',(event:L.LeafletMouseEvent)=>seek(nearestPoint(points,event.latlng.lat,event.latlng.lng,props.cursor,segment)));path.addTo(layers)
 }
 if(data.value?.points.length&&fittedDate!==props.date){fit();fittedDate=props.date}
 position()
}
function fit(){if(!map||!data.value?.points.length)return;map.fitBounds(L.latLngBounds(data.value.points.map(point=>[point.lat,point.lon])),{padding:[28,28],maxZoom:17,animate:false})}
function position(){if(!map)return;const point=current.value;if(!point){marker?.remove();marker=undefined;return}
 const location:L.LatLngTuple=[point.lat,point.lon]
 if(!marker)marker=L.circleMarker(location,{radius:8,color:'#fff',weight:3,fillColor:'#da755a',fillOpacity:1,className:'playback-location'}).addTo(map)
 else marker.setLatLng(location)
 const label=document.createElement('span');label.textContent=clock(point.at);marker.unbindTooltip().bindTooltip(label)
 if(follow.value)map.panInside(location,{padding:[35,35],animate:false})
}
function backgroundLayer(){if(!map)return;if(background.value&&!tiles){tileFailed.value=false;tiles=L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png',{maxZoom:19,attribution:'© <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener">OpenStreetMap</a>'}).on('tileerror',()=>tileFailed.value=true).addTo(map)}else if(!background.value){tiles?.remove();tiles=undefined;tileFailed.value=false}}
watch(()=>[props.date,props.refresh],load,{immediate:true});watch(()=>props.cursor,position);watch(follow,position);watch(background,backgroundLayer)
onMounted(()=>{if(!container.value)return;map=L.map(container.value,{scrollWheelZoom:false}).setView([36.2,138.2],5);map.attributionControl.setPrefix(false);map.on('dragstart',()=>follow.value=false);layers=L.layerGroup().addTo(map);backgroundLayer();resize=new ResizeObserver(()=>map?.invalidateSize());resize.observe(container.value);render()})
onUnmounted(()=>{sequence++;request?.abort();resize?.disconnect();map?.remove()})
</script>
<template>
 <section class="panel playback-track radio-survey" aria-label="録画と走行履歴の地図">
  <div class="panel-head"><h2><MapPin :size="18"/>走行履歴</h2><div class="playback-map-actions"><button class="icon-button" :class="{active:follow}" :aria-pressed="follow" aria-label="再生位置を追う" @click="follow=!follow"><LocateFixed :size="18"/></button><button class="icon-button" aria-label="走行経路の全体を表示" :disabled="!data?.points.length" @click="follow=false;fit()"><Maximize :size="18"/></button></div></div>
  <div class="playback-map-frame radio-map-frame"><div ref="container" class="playback-map radio-map-canvas" aria-label="録画時刻と連動する走行地図"></div><div v-if="!data?.points.length" class="playback-map-empty"><MapPin :size="27"/><strong>{{loading?'位置記録を読込み中…':error?'位置記録を取得できません':'この日の位置記録はありません'}}</strong><p>{{error||data?.reason||'測位できた録画では、再生位置を地図で確認できます。'}}</p><button v-if="error" class="secondary" @click="load">再試行</button></div></div>
  <div class="playback-position" aria-live="off"><strong v-if="current" data-testid="playback-position-time">{{clock(current.at)}}</strong><span v-else>{{data?.points.length?'この時刻の位置記録はありません':'位置記録なし'}}</span><span v-if="current?.speedMps!==undefined">{{Math.round(current.speedMps*3.6)}} km/h</span><label><input type="checkbox" v-model="background">背景地図</label></div>
  <p v-if="notice" class="playback-map-note" role="status">{{notice}}</p><p v-if="tileFailed" class="playback-map-note">背景地図を取得できません。記録された走行経路は表示できます。</p>
  <ol v-if="segments.length" class="playback-trip-list" aria-label="この日の走行区間"><li v-for="segment in segments.slice(0,shown)" :key="segment.id"><button @click="seek(playable(segment))" :disabled="!playable(segment)" :aria-label="clock(segment.start)+'の走行区間から再生'"><span><strong>{{clock(segment.start)}} — {{clock(segment.end)}}</strong><small>{{segment.distanceKm===null?'測位点のみ':distance(segment.distanceKm)+' · 走行 '+elapsed(segment.drivingSeconds||0)}}{{!playable(segment)?' · 録画なし':''}}</small></span><Play :size="15"/></button></li></ol>
  <button v-if="segments.length>shown" class="text-button playback-more" @click="shown+=8">残りの走行区間を表示</button>
  <p v-if="data?.sourceTruncated" class="playback-map-note">一部の位置記録は読み取り範囲に含まれていません。</p>
 </section>
</template>
<style scoped>
.playback-track.radio-survey{padding:0}
.playback-track{min-width:0;overflow:hidden}.playback-track .panel-head{padding:14px 18px;gap:12px}.playback-track h2{display:flex;gap:8px;align-items:center;font-size:16px}.playback-track h2 svg{color:var(--accent)}.playback-map-actions{display:flex;gap:5px}.playback-map-actions .active{color:var(--accent);background:var(--soft)}.playback-map-frame{height:350px;min-height:260px;border:0;border-radius:0;position:relative}.playback-map{height:100%;width:100%}.playback-map-empty{position:absolute;inset:0;z-index:450;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:12px;padding:25px;text-align:center;color:var(--muted);pointer-events:none;background:var(--paper)}.playback-map-empty strong{color:var(--text);font-size:16px}.playback-map-empty p{font-size:13px;line-height:1.8;max-width:340px}.playback-map-empty button{pointer-events:auto}.playback-position{padding:13px 18px;display:flex;gap:12px;align-items:center;flex-wrap:wrap;font-size:13px;min-height:50px;border-top:1px solid var(--line)}.playback-position strong{font-variant-numeric:tabular-nums}.playback-position>span{color:var(--muted)}.playback-position label{display:flex;gap:7px;align-items:center;margin-left:auto;min-height:32px;font-size:12px;color:var(--muted)}.playback-position input{accent-color:var(--accent)}.playback-trip-list{list-style:none;padding:0;margin:0;border-top:1px solid var(--line);max-height:240px;overflow:auto}.playback-trip-list button{display:flex;justify-content:space-between;align-items:center;gap:12px;padding:13px 18px;width:100%;background:transparent;text-align:left;border-radius:0;border-bottom:1px solid var(--line);min-height:52px;color:var(--text)}.playback-trip-list button:hover{background:var(--soft)}.playback-trip-list strong{display:block;font-size:13px;font-weight:600;font-variant-numeric:tabular-nums}.playback-trip-list small{display:block;font-size:12px;color:var(--muted);margin-top:5px}.playback-map-note{font-size:12px;color:var(--muted);line-height:1.8;padding:10px 18px;margin:0}.playback-more{margin:12px 18px;min-height:36px}@media(max-width:600px){.playback-map-frame{height:300px}.playback-position{padding:10px 14px}.playback-position label{width:100%;margin-left:0}.playback-trip-list button{padding:13px 14px}}
</style>
