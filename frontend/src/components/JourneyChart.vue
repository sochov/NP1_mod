<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,useId,watch} from 'vue'
import {ExternalLink,Route} from '@lucide/vue'
import {api,type Observation,type Summary} from '../services/api'
import {calendar,duration,finite,groupJourney,jstDate,niceMaximum,shortDate,valueText,type JourneyBucket,type JourneyDay,type JourneyGrouping,type JourneyHistory,type JourneyPeriod} from '../services/journey'
import '../journey-chart.css'
const props=defineProps<{summary?:Summary;details?:boolean;showLink?:boolean}>()
const emit=defineEmits<{loaded:[history:JourneyHistory|undefined];loading:[value:boolean];navigate:[page:string]}>()
const heading=useId(),period=ref('7'),grouping=ref<JourneyGrouping>('day'),customFrom=ref(''),customTo=ref(jstDate()),appliedFrom=ref(''),appliedTo=ref(''),selected=ref<string>(),history=ref<JourneyHistory>(),loading=ref(false),error=ref(''),refreshError=ref(''),validation=ref(''),chartScroll=ref<HTMLElement>(),viewportWidth=ref(300),calendarToday=ref(jstDate())
let sequence=0
let resize:ResizeObserver|undefined
let active=true,controller:AbortController|undefined,inFlightScope='',loadedScope='',poll:ReturnType<typeof setInterval>|undefined
const today=computed(()=>calendarToday.value)
const defaultFrom=computed(()=>new Date(Date.parse(today.value+'T00:00:00Z')-6*86400000).toISOString().slice(0,10))
customFrom.value=defaultFrom.value
const request=computed(()=>{const query=new URLSearchParams({period:period.value});if(period.value==='custom'){query.set('from',appliedFrom.value||customFrom.value);query.set('to',appliedTo.value||customTo.value)}return query.toString()})
const periodData=computed<JourneyPeriod|undefined>(()=>history.value?.period??props.summary?.tripStats?.data?.periods?.[period.value])
const range=computed(()=>{
 if(history.value)return history.value.range
 const count=Number(period.value)
 return {period:period.value,from:period.value==='custom'?(appliedFrom.value||customFrom.value):Number.isFinite(count)?new Date(Date.parse(today.value+'T00:00:00Z')-(count-1)*86400000).toISOString().slice(0,10):today.value,to:period.value==='custom'?(appliedTo.value||customTo.value):today.value}
})
const entries=computed<JourneyDay[]>(()=>Array.isArray(history.value?.days)?history.value!.days:Array.isArray(periodData.value?.days)?periodData.value!.days:[])
const days=computed(()=>calendar(range.value.from,range.value.to,entries.value))
const buckets=computed(()=>groupJourney(days.value,grouping.value))
const populated=computed(()=>buckets.value.some(bucket=>bucket.distance!==null||bucket.hours!==null))
const distanceMax=computed(()=>niceMaximum(buckets.value.map(bucket=>bucket.distance))),hoursMax=computed(()=>niceMaximum(buckets.value.map(bucket=>bucket.hours))),cumulativeDistanceMax=computed(()=>niceMaximum(buckets.value.map(bucket=>bucket.cumulativeDistance))),cumulativeHoursMax=computed(()=>niceMaximum(buckets.value.map(bucket=>bucket.cumulativeHours)))
const width=computed(()=>Math.max(viewportWidth.value,buckets.value.length*34)),step=computed(()=>width.value/Math.max(1,buckets.value.length))
const detail=computed(()=>buckets.value.find(bucket=>bucket.key===selected.value))
const groupLabel=computed(()=>({day:'日',week:'週',month:'月'}[grouping.value]))
const y=(value:number,max:number)=>190-value/max*180
const x=(index:number)=>(index+.5)*step.value
function label(bucket:JourneyBucket){return bucket.from===bucket.to?shortDate(bucket.from):shortDate(bucket.from)+'〜'+shortDate(bucket.to)}
function description(bucket:JourneyBucket){return `${bucket.from}${bucket.from===bucket.to?'':'〜'+bucket.to}、距離 ${valueText(bucket.distance)} km、走行時間 ${duration(bucket.hours===null?null:bucket.hours*3600)}、計測 ${bucket.observedDays}/${bucket.dayCount}日`}
function line(metric:'distance'|'hours'){
 const key=metric==='distance'?'cumulativeDistance':'cumulativeHours',count=metric==='distance'?'distanceDays':'timeDays',maximum=metric==='distance'?cumulativeDistanceMax.value:cumulativeHoursMax.value
 let previousComplete=false
 return buckets.value.map((bucket,index)=>{const value=bucket[key];if(value===null){previousComplete=false;return ''}const complete=bucket[count]===bucket.dayCount;const command=previousComplete&&complete?'L':'M';previousComplete=complete;return `${command}${x(index)} ${y(value,maximum)}`}).join(' ')
}
function focusBucket(event:KeyboardEvent,index:number){let next=index;if(event.key==='ArrowRight')next++;else if(event.key==='ArrowLeft')next--;else if(event.key==='Home')next=0;else if(event.key==='End')next=buckets.value.length-1;else return;event.preventDefault();next=Math.max(0,Math.min(buckets.value.length-1,next));const nodes=(event.currentTarget as Element).parentElement!.querySelectorAll<SVGElement>('.jc-bucket');nodes[next]?.focus();selected.value=buckets.value[next]?.key}
function choosePeriod(value:string){period.value=value;selected.value=undefined;validation.value='';grouping.value=value==='90'?'week':['365','all'].includes(value)?'month':'day';if(value==='custom'){appliedFrom.value=customFrom.value;appliedTo.value=customTo.value}}
function applyCustom(){if(!customFrom.value||!customTo.value||customFrom.value>customTo.value||customTo.value>today.value){validation.value='開始日と終了日を確認してください。未来の日付は指定できません。';return}validation.value='';const changed=appliedFrom.value!==customFrom.value||appliedTo.value!==customTo.value;appliedFrom.value=customFrom.value;appliedTo.value=customTo.value;selected.value=undefined;if(!changed)load()}
async function load(){
 if(!active||document.hidden)return
 calendarToday.value=jstDate()
 const scope=request.value
 if(controller&&inFlightScope===scope)return
 controller?.abort()
 const current=new AbortController(),id=++sequence,preserve=history.value!==undefined&&loadedScope===scope
 controller=current;inFlightScope=scope;error.value='';refreshError.value=''
 if(!preserve){history.value=undefined;emit('loaded',undefined)}
 loading.value=!preserve;emit('loading',!preserve)
 try{const response=await api<Observation<JourneyHistory>>('/trips?'+scope,'GET',undefined,true,current.signal);if(id!==sequence||!active||current.signal.aborted)return;if(!response.data||!Array.isArray(response.data.days)||!response.data.period||!Array.isArray(response.data.trips))throw new Error('走行履歴を取得できません');history.value=response.data;loadedScope=scope;emit('loaded',response.data)}catch(value){if(id===sequence&&active&&!current.signal.aborted){if(preserve)refreshError.value='更新できませんでした。前回の走行記録を表示しています。';else error.value=value instanceof Error?value.message:'走行履歴を取得できません'}}finally{if(id===sequence){controller=undefined;inFlightScope='';loading.value=false;emit('loading',false)}}
}
function visibilityChanged(){if(document.hidden){sequence++;controller?.abort();controller=undefined;inFlightScope='';loading.value=false;emit('loading',false)}else void load()}
watch(request,()=>{void load()},{immediate:true})
watch(chartScroll,element=>{resize?.disconnect();if(!element)return;viewportWidth.value=element.clientWidth;resize=new ResizeObserver(()=>viewportWidth.value=element.clientWidth);resize.observe(element)})
onMounted(()=>{document.addEventListener('visibilitychange',visibilityChanged);poll=setInterval(()=>{void load()},30000)})
onUnmounted(()=>{active=false;sequence++;controller?.abort();clearInterval(poll);document.removeEventListener('visibilitychange',visibilityChanged);resize?.disconnect()})
</script>
<template>
 <section class="panel journey-panel jc-panel" :aria-labelledby="heading">
  <div class="jc-head"><h2 :id="heading"><Route :size="20"/>走行の記録</h2><button v-if="showLink" class="icon-button section-link" aria-label="走行履歴を開く" @click="emit('navigate','trips')"><ExternalLink :size="18"/></button></div>
  <div class="jc-totals"><div><span>走行距離</span><strong data-testid="trip-distance">{{loading||error?'—':valueText(periodData?.distanceKm)}}<small>km</small></strong></div><div><span>走行時間</span><strong class="jc-duration" data-testid="trip-time">{{loading||error?'—':duration(periodData?.drivingSeconds)}}</strong></div></div>
  <div class="jc-controls"><div class="jc-periods" role="group" aria-label="走行記録の期間"><button v-for="item in ['7','30','90','365','all','custom']" :key="item" :class="{selected:period===item}" :aria-pressed="period===item" @click="choosePeriod(item)">{{item==='all'?'全期間':item==='custom'?'カスタム':item+'日'}}</button></div><label>集約<select v-model="grouping" aria-label="走行グラフの集約"><option value="day">日毎</option><option value="week">週毎</option><option value="month">月毎</option></select></label></div>
  <form v-if="period==='custom'" class="jc-custom" @submit.prevent="applyCustom"><label>開始日<input v-model="customFrom" type="date" :max="customTo||today" required></label><label>終了日<input v-model="customTo" type="date" :min="customFrom" :max="today" required></label><button class="secondary" type="submit">表示</button><p v-if="validation" role="alert">{{validation}}</p></form>
  <div v-if="refreshError" class="jc-error" role="status">{{refreshError}}<button class="secondary" @click="load">再試行</button></div>
  <div v-if="error" class="jc-error" role="alert">{{error}}<button class="secondary" @click="load">再試行</button></div>
  <div v-else-if="loading" class="jc-empty" role="status">走行履歴を読み込み中…</div>
  <template v-else-if="populated">
   <div class="jc-legend"><span class="jc-distance"><i></i>距離 · km</span><span class="jc-time"><i></i>時間 · h</span><span><b class="jc-bar-key"></b>{{groupLabel}}毎</span><span><b class="jc-line-key"></b>累計</span></div>
   <div class="jc-axis-labels"><span>各{{groupLabel}}の値</span><span>計測分の累計</span></div>
   <div class="jc-chart" role="group" :aria-label="`${range.from}から${range.to}の距離と走行時間`">
    <div class="jc-axis jc-left" aria-label="左軸: 各区間の距離kmと時間h"><div v-for="fraction in [1,.5,0]" :key="fraction"><span class="jc-distance">{{valueText(distanceMax*fraction)}} km</span><span class="jc-time">{{valueText(hoursMax*fraction,2)}} h</span></div></div>
    <div ref="chartScroll" class="jc-scroll" tabindex="0" aria-label="走行グラフ。左右にスクロールできます"><svg class="jc-svg" :width="width" height="220" :viewBox="`0 0 ${width} 220`">
     <path v-for="level in [10,100,190]" :key="level" :d="`M0 ${level} H${width}`" class="jc-grid" aria-hidden="true"/>
     <g v-for="(bucket,index) in buckets" :key="bucket.key" class="jc-bucket" :class="{selected:selected===bucket.key,missing:bucket.distance===null&&bucket.hours===null,zero:bucket.distance===0&&bucket.hours===0}" role="button" tabindex="0" :aria-label="description(bucket)" :aria-pressed="selected===bucket.key" @click="selected=bucket.key" @keydown.enter.prevent="selected=bucket.key" @keydown.space.prevent="selected=bucket.key" @keydown="focusBucket($event,index)">
      <title>{{description(bucket)}}</title><rect :x="index*step" y="0" :width="step" height="194" class="jc-hit"/>
      <rect v-if="bucket.distance!==null&&bucket.distance>0" :x="x(index)-12" :y="y(bucket.distance,distanceMax)" width="10" :height="Math.max(1,190-y(bucket.distance,distanceMax))" class="jc-distance-bar" :class="{partial:bucket.distanceDays<bucket.dayCount}"/>
      <circle v-else-if="bucket.distance===0" :cx="x(index)-7" cy="190" r="3" class="jc-distance-dot"/>
      <path v-else :d="`M${x(index)-11} 186 l8 8 m-8 0 l8 -8`" class="jc-missing"/>
      <rect v-if="bucket.hours!==null&&bucket.hours>0" :x="x(index)+2" :y="y(bucket.hours,hoursMax)" width="10" :height="Math.max(1,190-y(bucket.hours,hoursMax))" class="jc-time-bar" :class="{partial:bucket.timeDays<bucket.dayCount}"/>
      <circle v-else-if="bucket.hours===0" :cx="x(index)+7" cy="190" r="3" class="jc-time-dot"/>
      <path v-else :d="`M${x(index)+3} 186 l8 8 m-8 0 l8 -8`" class="jc-missing"/>
      <text v-if="buckets.length<16||index%Math.ceil(buckets.length/12)===0||index===buckets.length-1" :x="x(index)" y="213" text-anchor="middle" class="jc-date" aria-hidden="true">{{grouping==='month'?Number(bucket.from.slice(5,7))+'月':shortDate(bucket.from)}}</text>
     </g>
     <path :d="line('distance')" class="jc-distance-line" aria-hidden="true"/><path :d="line('hours')" class="jc-time-line" aria-hidden="true"/>
     <template v-for="(bucket,index) in buckets" :key="bucket.key"><circle v-if="bucket.cumulativeDistance!==null" :cx="x(index)" :cy="y(bucket.cumulativeDistance,cumulativeDistanceMax)" r="3" class="jc-distance-point" aria-hidden="true"/><circle v-if="bucket.cumulativeHours!==null" :cx="x(index)" :cy="y(bucket.cumulativeHours,cumulativeHoursMax)" r="3" class="jc-time-point" aria-hidden="true"/></template>
    </svg></div>
    <div class="jc-axis jc-right" aria-label="右軸: 計測分累計の距離kmと時間h"><div v-for="fraction in [1,.5,0]" :key="fraction"><span class="jc-distance">{{valueText(cumulativeDistanceMax*fraction)}} km</span><span class="jc-time">{{valueText(cumulativeHoursMax*fraction,2)}} h</span></div></div>
   </div>
   <p v-if="width>viewportWidth+1" class="jc-scroll-hint">← 横にスクロールして全区間を確認 →</p>
   <div class="jc-detail chart-detail" role="status"><template v-if="detail"><strong>{{label(detail)}}</strong><span>距離 {{valueText(detail.distance)}} km / 時間 {{duration(detail.hours===null?null:detail.hours*3600)}}</span><span>累計 {{valueText(detail.cumulativeDistance)}} km / {{duration(detail.cumulativeHours===null?null:detail.cumulativeHours*3600)}}</span><small>計測 {{detail.observedDays}} / {{detail.dayCount}}日</small></template><template v-else>棒をタップすると、その{{groupLabel}}の値と累計を確認できます。</template></div>
  </template>
  <div v-else class="jc-empty"><Route :size="25"/><strong>この期間の走行記録はありません</strong><span>{{history?.reason||'距離と時間は、連続して測位できた記録から集計します。'}}</span></div>
  <div class="jc-foot"><span>{{range.from}}〜{{range.to}} · JST</span><span>0 は計測済み · × は未計測</span></div>
  <p v-if="history?.sourceTruncated||summary?.tripStats?.data?.sourceTruncated" class="jc-truncated">一部区間は集計に含まれていません。取得・保存の状態を確認してください。</p>
 </section>
</template>
