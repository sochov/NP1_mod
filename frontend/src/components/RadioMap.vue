<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import { api } from '../services/api'
const container=ref<HTMLElement>();const metric=ref('rsrp');const mode=ref('route');const legend=ref<{unit:string;note:string;thresholds:{minimum:number|null;label:string;color:string}[]}>();let map:L.Map|undefined
async function loadLegend(){legend.value=await api('/radio/heatmap/legend?metric='+metric.value)}
onMounted(()=>{if(container.value){map=L.map(container.value,{zoomControl:true,attributionControl:false}).setView([36.2,138.2],5);L.control.attribution({prefix:false}).addTo(map).addAttribution('地図タイル未接続 · 位置情報の外部送信なし')}loadLegend()})
watch(metric,loadLegend);onUnmounted(()=>map?.remove())
</script>
<template><div class="map-controls"><label>測定項目<select v-model="metric"><option value="rsrp">RSRP · dBm</option><option value="sinr">SINR · dB</option><option value="rsrq">RSRQ · dB</option></select></label><label>表示方法<select v-model="mode"><option value="route">経路色分け</option><option value="points">測定点</option><option value="grid">グリッド集計</option></select></label><label>対象期間<select disabled><option>収集データなし</option></select></label></div><div class="map-wrap"><div ref="container" class="map-canvas" aria-label="電波品質地図"></div><div class="map-empty"><span class="eyebrow">RADIO SURVEY</span><h3>まだ、走行の記録がありません</h3><p>GNSSとLTEの取得経路を検証後、記録を有効にできます。<br>欠測値や推定位置は描画しません。</p><span class="tag">位置の記録 OFF</span></div><div class="map-scale">グリッド 20 / 50 / 100 m · 既定は中央値</div></div><div class="map-legend"><span>{{metric.toUpperCase()}} / {{legend?.unit}}</span><span v-for="t in legend?.thresholds" :key="t.label"><i :style="{'background-color':t.color}"></i>{{t.minimum===null?'下限未満':`≥ ${t.minimum}`}} {{t.label}}</span><span>--- 圏外</span><span>▧ 未取得</span></div><p class="hint">{{legend?.note}} · 観測数 0 / 走行数 0。地図タイルは未設定で、外部サービスには接続していません。</p></template>
