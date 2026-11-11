<script setup lang="ts">
import {computed,ref,watch} from 'vue'
import {Clock3,MapPin,Route,Play} from '@lucide/vue'
import JourneyChart from './JourneyChart.vue'
import RadioMap from './RadioMap.vue'
import {duration,valueText,type JourneyHistory} from '../services/journey'
import '../drive-history.css'
const history=ref<JourneyHistory>(),loading=ref(false),selectedDate=ref(''),shown=ref(40)
const date=(value:string)=>new Intl.DateTimeFormat('sv-SE',{timeZone:'Asia/Tokyo'}).format(new Date(value))
const time=(value:string)=>new Date(value).toLocaleTimeString('ja-JP',{timeZone:'Asia/Tokyo',hour:'2-digit',minute:'2-digit',second:'2-digit'})
const dateTitle=(value:string)=>new Date(value+'T00:00:00+09:00').toLocaleDateString('ja-JP',{timeZone:'Asia/Tokyo',year:'numeric',month:'long',day:'numeric',weekday:'short'})
const recordedDays=computed(()=>[...(history.value?.days??[])].filter(day=>day.observed).reverse())
const selectedDay=computed(()=>history.value?.days.find(day=>day.date===selectedDate.value))
const trips=computed(()=>(history.value?.trips??[]).filter(trip=>date(trip.start)<=selectedDate.value&&date(trip.end)>=selectedDate.value))
watch(history,(value,previous)=>{if(!value)return;if(!previous)shown.value=40;if(!selectedDate.value||selectedDate.value<value.range.from||selectedDate.value>value.range.to)selectedDate.value=[...value.days].reverse().find(day=>day.observed)?.date||value.range.to})
</script>
<template>
 <div class="drive-history">
  <JourneyChart details @loaded="history=$event" @loading="loading=$event"/>
  <section class="panel dh-details" aria-labelledby="drive-day-heading">
   <div class="dh-heading"><div><h2 id="drive-day-heading"><Route :size="20"/>日別の詳細</h2><p>連続して測位できた区間ごとに確認できます。</p></div><label v-if="history">日付<input v-model="selectedDate" type="date" :min="history.range.from" :max="history.range.to" aria-label="詳細を表示する日付"></label></div>
   <div v-if="loading" class="dh-empty" role="status">走行区間を読み込み中…</div>
   <div v-else-if="!history" class="dh-empty">走行履歴の取得を確認してください。</div>
   <div v-else-if="!history.period.hasData" class="dh-empty"><MapPin :size="25"/><strong>距離・走行時間は未計測です</strong><p>{{history.reason||'有効な連続測位データがありません。'}}</p><span v-if="history.sourceFiles">純正の測位ファイル {{history.sourceFiles.toLocaleString()}} 件を確認<span v-if="history.noFixSentences"> · 未測位 {{history.noFixSentences.toLocaleString()}} 件</span></span></div>
   <div v-else class="dh-body">
    <aside class="dh-days" aria-label="記録のある日"><button v-for="day in recordedDays.slice(0,shown)" :key="day.date" :class="{selected:selectedDate===day.date}" :aria-pressed="selectedDate===day.date" @click="selectedDate=day.date"><strong>{{day.date}}</strong><span>{{valueText(day.distanceKm)}} km</span><small>{{duration(day.drivingSeconds)}}</small></button><button v-if="recordedDays.length>shown" class="dh-more" @click="shown+=40">さらに表示</button></aside>
    <div class="dh-day-detail">
     <h3>{{selectedDate?dateTitle(selectedDate):'日付を選択'}}</h3>
     <div class="dh-day-values"><div><span>この日の距離</span><strong>{{valueText(selectedDay?.distanceKm)}}<small> km</small></strong></div><div><span>走行時間</span><strong>{{duration(selectedDay?.drivingSeconds)}}</strong></div></div>
     <p v-if="!selectedDay?.observed" class="dh-no-trips">この日に有効な連続測位の記録はありません。</p>
     <p v-else-if="!trips.length" class="dh-no-trips">この日の集計はありますが、区間詳細は表示上限の範囲外です。</p>
     <ol v-else class="dh-trip-list"><li v-for="trip in trips" :key="trip.id"><div class="dh-trip-time"><Clock3 :size="16"/><strong>{{time(trip.start)}}<span> → </span>{{time(trip.end)}}</strong><small v-if="date(trip.start)!==date(trip.end)">{{date(trip.start)}}〜{{date(trip.end)}}</small><a class="dh-recording-link" :href="'#recordings?at='+encodeURIComponent(trip.start)"><Play :size="14"/>録画と地図</a></div><dl><div><dt>距離</dt><dd>{{valueText(trip.distanceKm)}} km</dd></div><div><dt>走行時間</dt><dd>{{duration(trip.drivingSeconds)}}</dd></div><div><dt>測位できた時間</dt><dd>{{duration(trip.observedSeconds)}}</dd></div></dl><span v-if="trip.drivingSeconds===0" class="dh-stationary">停止中の測位</span><small v-if="date(trip.start)!==date(trip.end)" class="dh-crossday">日付をまたぐ区間全体の距離・時間です。</small></li></ol>
    </div>
   </div>
   <details v-if="history" class="dh-source"><summary>取得元・測位の状態</summary><dl><div><dt>取得元</dt><dd>{{history.source==='ncc-retained-measured-intervals'?'保存した走行記録（純正NMEAから集計）':'純正録画のNMEA測位情報'}}</dd></div><div><dt>読取り済みファイル</dt><dd>{{history.sourceFiles.toLocaleString()}} 件</dd></div><div><dt>有効な連続測位のある日</dt><dd>{{history.period.validObservedDays}} 日</dd></div><div><dt>未測位の記録</dt><dd>{{history.noFixSentences.toLocaleString()}} 件</dd></div><div v-if="history.unreadableFiles||history.badChecksums"><dt>読取り不可 / チェックサム不正</dt><dd>{{history.unreadableFiles}} / {{history.badChecksums}}</dd></div><div v-if="history.range.sourceFrom"><dt>有効な測位の範囲</dt><dd>{{history.range.sourceFrom}}〜{{history.range.sourceTo}}</dd></div></dl><p>測位の欠落や5秒を超える間隔で区切ります。走行時間は測位中に移動した時間です。</p><p v-if="history.detailsTruncated">区間詳細は新しい {{history.trips.length.toLocaleString()}} 件を表示しています。グラフは読み取った全区間の集計です。</p><p v-if="history.sourceTruncated">一部区間は集計に含まれていません。取得・保存の状態を確認してください。</p></details>
  </section>
  <section class="panel radio-panel dh-radio"><div class="dh-heading"><div><h2><MapPin :size="20"/>電波マップ</h2><p>走行地点ごとの4Gの受信状態</p></div></div><RadioMap/></section>
 </div>
</template>
