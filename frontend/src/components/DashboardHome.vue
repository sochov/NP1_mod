<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch} from 'vue'
import type {Summary} from '../services/api'
import JourneyChart from './JourneyChart.vue'
import CurrentLocation from './CurrentLocation.vue'
import RecordingStatus from './RecordingStatus.vue'
import DashboardCameras from './DashboardCameras.vue'
import TrafficUsage from './TrafficUsage.vue'
import NetworkSummary from './NetworkSummary.vue'
import ResourceMonitor from './ResourceMonitor.vue'
import DeviceStatus from './DeviceStatus.vue'
import '../dashboard-home.css'
const props=defineProps<{summary:Summary;phone?:boolean}>()
const emit=defineEmits<{navigate:[page:string]}>()
type Inset={top:number;right:number;bottom:number;left:number}
const stage=ref<HTMLElement>(),chartHeight=ref(props.phone?170:220),inset=ref<Inset>()
const frontOnly=['front'] as const
let observer:ResizeObserver|undefined,frame=0
const clamp=(low:number,value:number,high:number)=>Math.max(low,Math.min(high,value))

// Phone: the panels live in a sheet pulled up from the bottom. It rests at three heights — a peek, about
// half (the default) and full; only the full sheet scrolls its content, a shorter one is dragged as a whole.
const level=ref(1),dragged=ref<number|null>(null),stageHeight=ref(0),stageTop=ref(0),body=ref<HTMLElement>()
const stops=computed(()=>[Math.min(150,Math.round(stageHeight.value*.3)),Math.round(stageHeight.value*.46),Math.max(0,stageHeight.value-stageTop.value+4)])
const sheetHeight=computed(()=>dragged.value??stops.value[level.value]!)
// The map keeps the position in view above the sheet at rest; a full sheet hides the map anyway.
const sheetStyle=computed(()=>({'--dh-sheet':sheetHeight.value+'px','--dh-rest':stops.value[Math.min(level.value,1)]!+'px'}))
let grabbed:{y:number;height:number;id:number;moved:boolean;grip:boolean}|undefined,swallowUntil=0,rolledAt=0
function grab(event:PointerEvent){
 if(event.pointerType==='mouse'&&event.button!==0)return
 const grip=!!(event.target as Element).closest('.dh-grip')
 if(level.value===2&&!grip)return
 grabbed={y:event.clientY,height:sheetHeight.value,id:event.pointerId,moved:false,grip}
 // A mouse leaves the handle as soon as it moves, so the handle keeps the pointer from the start.
 // Elsewhere the pointer is taken only once a drag is certain, so that buttons in the sheet still get their clicks.
 if(grip)(event.currentTarget as Element).setPointerCapture(event.pointerId)
}
function drag(event:PointerEvent){
 if(!grabbed||event.pointerId!==grabbed.id)return
 const travel=grabbed.y-event.clientY
 if(!grabbed.moved){if(Math.abs(travel)<8)return;grabbed.moved=true;if(!grabbed.grip)(event.currentTarget as Element).setPointerCapture(event.pointerId)}
 dragged.value=clamp(stops.value[0]!,grabbed.height+travel,stops.value[2]!)
}
function release(event:PointerEvent){
 if(!grabbed||event.pointerId!==grabbed.id)return
 const {moved,height:from,grip}=grabbed;grabbed=undefined
 if(!moved){if(grip&&event.type==='pointerup')cycle();return}
 const height=dragged.value??from,travel=height-from
 let next=stops.value.reduce((best,stop,index)=>Math.abs(stop-height)<Math.abs(stops.value[best]!-height)?index:best,0)
 // A clear pull moves at least one step even when it ends nearer to where it began.
 if(next===level.value&&Math.abs(travel)>40)next=clamp(0,level.value+Math.sign(travel),2)
 level.value=next;dragged.value=null;swallowUntil=performance.now()+350
}
// The click that ends a drag must not press whatever was under the finger.
function swallow(event:MouseEvent){if(performance.now()<swallowUntil){event.stopPropagation();event.preventDefault()}}
function cycle(){level.value=level.value===2?0:level.value+1}
// A tap on the handle is answered when the pointer is released; this is for the keyboard and assistive tools.
function press(event:MouseEvent){if(event.detail===0)cycle()}
function roll(event:WheelEvent){
 const now=performance.now();if(now-rolledAt<350)return
 if(level.value<2&&event.deltaY>0){level.value++;rolledAt=now}
 else if(level.value===2&&event.deltaY<0&&(body.value?.scrollTop??0)<=0){level.value=1;rolledAt=now}
}
watch(level,value=>{if(value<2&&body.value)body.value.scrollTop=0})

// The stylesheet chooses the arrangement (--dh-layout). This only measures: where the map is left
// uncovered, and — when the panels frame the window — how tall the chart and the camera pictures may be
// so that the trip record and the cameras end above the row of tiles without scrolling.
function fit(){
 const root=stage.value;if(!root)return
 const style=getComputedStyle(root),layout=style.getPropertyValue('--dh-layout').trim(),open=root.querySelector<HTMLElement>('.dh-window')
 if(layout==='sheet'){stageHeight.value=root.clientHeight;stageTop.value=root.querySelector<HTMLElement>('.dh-chips')?.offsetTop??0}
 if(open){
  const outer=root.getBoundingClientRect(),inner=open.getBoundingClientRect()
  const next={top:Math.round(inner.top-outer.top),right:Math.round(outer.right-inner.right),bottom:Math.round(outer.bottom-inner.bottom),left:Math.round(inner.left-outer.left)},now=inset.value
  if(!now||now.top!==next.top||now.right!==next.right||now.bottom!==next.bottom||now.left!==next.left)inset.value=next
 }
 // The line with the position readings and the map credit goes just above the row of tiles when the panels
 // frame the window, and just above the trip record and the cameras when they are stacked under the map.
 const tiles=root.querySelector<HTMLElement>('.dh-tiles'),below=layout==='frame'?tiles:layout==='stack'?root.querySelector<HTMLElement>('.dh-journey'):null
 if(below)root.style.setProperty('--dh-foot',Math.round(root.getBoundingClientRect().bottom-below.getBoundingClientRect().top+4)+'px');else root.style.removeProperty('--dh-foot')
 if(layout!=='frame'||!tiles){chartHeight.value=layout==='sheet'?170:160;root.style.removeProperty('--dh-cam-h');return}
 const room=innerHeight-parseFloat(style.paddingTop)-parseFloat(style.paddingBottom)-(parseFloat(style.rowGap)||16)-tiles.offsetHeight-(parseFloat(getComputedStyle(tiles).marginTop)||0)
 const journey=root.querySelector<HTMLElement>('.dh-journey'),plot=journey?.querySelector<SVGElement>('.jc-chart .jc-svg')
 if(journey&&plot){const drawn=plot.getBoundingClientRect().height;chartHeight.value=Math.round(clamp(110,room-(journey.offsetHeight-drawn),250))}
 const cameras=root.querySelector<HTMLElement>('.dh-cams'),pictures=[...root.querySelectorAll<HTMLElement>('.dh-cams figure')]
 if(cameras&&pictures.length){
  const shown=pictures.reduce((sum,picture)=>sum+picture.offsetHeight,0),free=room-(cameras.offsetHeight-shown)
  root.style.setProperty('--dh-cam-h',Math.floor(clamp(96,free/pictures.length,pictures[0]!.offsetWidth*9/16))+'px')
 }
}
// Measuring is deferred to the next frame so that a resize it causes is observed in a fresh one.
function schedule(){if(!frame)frame=requestAnimationFrame(()=>{frame=0;fit()})}
onMounted(()=>{
 const root=stage.value!
 observer=new ResizeObserver(schedule)
 if(props.phone)observer.observe(root)
 for(const element of root.querySelectorAll('.dh-window,.dh-journey,.dh-cams,.dh-tiles'))observer.observe(element)
 window.addEventListener('resize',schedule);fit()
})
onUnmounted(()=>{observer?.disconnect();window.removeEventListener('resize',schedule);cancelAnimationFrame(frame)})
</script>
<template>
 <div class="dash-stage">
  <div v-if="phone" ref="stage" class="dash-home dh-phone" :class="{dragging:dragged!==null}" :style="sheetStyle">
   <div class="dh-map"><CurrentLocation hero :inset="inset" :observation="summary.gps"/></div>
   <div class="dh-window" aria-hidden="true"></div>
   <div class="dh-chips"><RecordingStatus compact :observation="summary.recording" @open="emit('navigate','recordings')"/></div>
   <DashboardCameras class="dh-thumb" :sources="frontOnly" bare @open="emit('navigate','camera')"/>
   <section class="dh-sheet" :class="{full:level===2}" aria-label="走行・通信・本体の情報" @pointerdown="grab" @pointermove="drag" @pointerup="release" @pointercancel="release" @click.capture="swallow" @wheel.passive="roll">
    <button type="button" class="dh-grip" :aria-label="level===2?'パネルをたたむ':'パネルを広げる'" :aria-expanded="level>0" @click="press"></button>
    <div ref="body" class="dh-sheet-body">
     <JourneyChart class="dh-journey" :summary="summary" :chart-height="chartHeight" show-link @navigate="emit('navigate',$event)"/>
     <TrafficUsage class="dh-tile" compact :summary="summary" @navigate="emit('navigate',$event)"/>
     <NetworkSummary class="dh-tile" :summary="summary" @navigate="emit('navigate',$event)"/>
     <ResourceMonitor class="dh-tile" compact :summary="summary" @open="emit('navigate','system')"/>
     <DeviceStatus class="dh-tile" :summary="summary" @open="emit('navigate','system')"/>
    </div>
   </section>
  </div>
  <div v-else ref="stage" class="dash-home">
   <div class="dh-map"><CurrentLocation hero :inset="inset" :observation="summary.gps"/></div>
   <JourneyChart class="dh-journey" :summary="summary" :chart-height="chartHeight" show-link @navigate="emit('navigate',$event)"/>
   <div class="dh-window" aria-hidden="true"></div>
   <section class="panel dh-cams" aria-label="ライブ映像と録画"><DashboardCameras @open="emit('navigate','camera')"><RecordingStatus compact :observation="summary.recording" @open="emit('navigate','recordings')"/></DashboardCameras></section>
   <div class="dh-tiles">
    <TrafficUsage class="dh-tile" compact :summary="summary" @navigate="emit('navigate',$event)"/>
    <NetworkSummary class="dh-tile" :summary="summary" @navigate="emit('navigate',$event)"/>
    <ResourceMonitor class="dh-tile" compact :summary="summary" @open="emit('navigate','system')"/>
    <DeviceStatus class="dh-tile" :summary="summary" @open="emit('navigate','system')"/>
   </div>
  </div>
 </div>
</template>
