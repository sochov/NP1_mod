<script setup lang="ts">
import { computed } from 'vue'
import { ExternalLink, Database, Gauge, HardDrive, MapPin, ShieldCheck, Video, Wifi, WifiHigh } from '@lucide/vue'
import { bytes, type Summary } from '../services/api'
import TrafficUsage from './TrafficUsage.vue'
import WifiTraffic from './WifiTraffic.vue'
import JourneyChart from './JourneyChart.vue'

const props = defineProps<{summary:Summary}>()
const emit = defineEmits<{navigate:[page:string]}>()
function data(key:string) { const item=props.summary[key]; return item && ['available','stale'].includes(item.status) ? item.data : undefined }
function number(value:unknown):value is number { return typeof value==='number' && Number.isFinite(value) }
function decimal(value:unknown):string|null { return typeof value==='string' && /^\d+$/.test(value) ? value : null }
function byteText(value:unknown) { const n=decimal(value);return n===null?'—':bytes(n) }
function percent(value:unknown) { return number(value)&&value>=0&&value<=100?`${value.toFixed(0)}%`:'—' }
const wifi=computed(()=>data('wifi'))
const vpn=computed(()=>data('tailscale'))
const sd=computed(()=>data('sd'))
const ram=computed(()=>data('ram'))
const cpu=computed(()=>data('cpu'))
const storage=computed(()=>data('internalStorage'))
const hotspot=computed(()=>data('hotspot'))
const gps=computed(()=>data('gps')||(props.summary.gps?.data?.fix===false?props.summary.gps.data:undefined))
const temperature=computed(()=>{const values=data('temperatures');if(!Array.isArray(values))return null;const cpus=values.filter(t=>typeof t.name==='string'&&t.name.toLowerCase().includes('cpu')&&number(t.celsius)&&t.celsius>=-40&&t.celsius<=150);return cpus.length?Math.max(...cpus.map(t=>t.celsius)):null})
const recording=computed(()=>{const value=data('recording');if(value?.writing===true)return '録画ファイルを更新中';if(value?.writing===false)return '直近の録画更新なし';if(number(value?.finalizedClips))return `保存済みの録画 ${value.finalizedClips.toLocaleString('ja-JP')} 件`;const service=(data('services') as {name:string;state:string}[]|undefined)?.find(s=>s.name==='app-@DriveRec.service');return service?.state==='failed'?'録画処理を確認してください':'録画状況は確認中'})
const gpsLabel=computed(()=>gps.value?.fix===true?'測位済み':gps.value?.fix===false?'測位待ち':'測位データ未取得')
const apLabel=computed(()=>hotspot.value?.enabled===true?'提供中':hotspot.value?.enabled===false?'停止中':'取得待ち')
const wifiLabel=computed(()=>{const state=wifi.value?.wpa_state;if(state==='COMPLETED')return '接続済み';if(['DISCONNECTED','INACTIVE','INTERFACE_DISABLED'].includes(state))return '未接続';return state?'接続中':'取得待ち'})
const vpnLabel=computed(()=>{if(vpn.value?.online===true)return 'オンライン';if(vpn.value?.backendState==='NeedsLogin')return '認証待ち';if(vpn.value?.backendState==='Stopped')return '停止中';return vpn.value?.online===false?'オフライン':'取得待ち'})
const vpnTitle=computed(()=>vpn.value?.online===true?'管理ネットワークに接続':vpn.value?.backendState==='NeedsLogin'?'Tailscaleで認証が必要':'接続状態を確認中')
const vpnPaths=computed(()=>{const paths=vpn.value?.activePeerConnections;if(!Array.isArray(paths)||!paths.length)return vpn.value?.online===true?'通信待ち':'—';return [...new Set(paths.map((p:string)=>p==='Direct'?'直接':p==='DERP'?'リレー':'確認中'))].join(' / ')})
</script>

<template>
 <div class="dashboard-hero">
  <JourneyChart :summary="summary" show-link @navigate="emit('navigate',$event)"/>
  <TrafficUsage :summary="summary" @navigate="emit('navigate',$event)"/>
 </div>

 <div class="connection-grid">
  <section class="panel connection-card"><div class="connection-title"><WifiHigh :size="20"/><h3>Wi-Fi AP</h3><span class="connection-badge" :class="{online:hotspot?.enabled===true}">{{apLabel}}</span><button class="icon-button section-link" aria-label="ホットスポットを開く" @click="emit('navigate','wifi')"><ExternalLink :size="18"/></button></div><strong class="connection-primary" :title="hotspot?.ssid">{{hotspot?.ssid||'ホットスポット情報待ち'}}</strong><dl><div><dt>接続端末</dt><dd>{{number(hotspot?.clientCount)?`${hotspot.clientCount} 台`:'—'}}</dd></div><div><dt>動作モード</dt><dd>{{hotspot?.mode==='always-on'?'常時ON':hotspot?.mode==='LAN-always-available'?'LANを維持':hotspot?.mode||'—'}}</dd></div></dl></section>
  <section class="panel connection-card"><div class="connection-title"><Wifi :size="20"/><h3>Wi-Fi STA</h3><span class="connection-badge" :class="{online:wifi?.wpa_state==='COMPLETED'}">{{wifiLabel}}</span><button class="icon-button section-link" aria-label="Wi-Fiの詳細を開く" @click="emit('navigate','wifi')"><ExternalLink :size="18"/></button></div><strong class="connection-primary" :title="wifi?.ssid">{{wifi?.ssid||'接続先情報待ち'}}</strong><dl><div><dt>認証</dt><dd>{{wifi?.key_mgmt||'—'}}</dd></div><div><dt>周波数</dt><dd>{{wifi?.freq?`${wifi.freq} MHz`:'—'}}</dd></div></dl><WifiTraffic :summary="summary"/></section>
  <section class="panel connection-card"><div class="connection-title"><ShieldCheck :size="20"/><h3>Tailscale</h3><span class="connection-badge" :class="{online:vpn?.online===true}">{{vpnLabel}}</span><button class="icon-button section-link" aria-label="Tailscaleを開く" @click="emit('navigate','vpn')"><ExternalLink :size="18"/></button></div><strong class="connection-primary" :title="vpn?.deviceName">{{vpn?.deviceName||vpnTitle}}</strong><dl><div class="tailscale-dashboard-account"><dt>アカウント</dt><dd :title="vpn?.accountLogin||vpn?.accountName">{{vpn?.accountLogin||vpn?.accountName||'—'}}</dd></div><div><dt>アドレス</dt><dd class="mono">{{vpn?.ips?.find((ip:string)=>ip.includes('.'))||'—'}}</dd></div><div><dt>通信経路</dt><dd>{{vpnPaths}}</dd></div></dl></section>
 </div>

 <div class="dashboard-lower">
  <section class="panel gps-panel"><div class="hero-head"><h2><MapPin :size="20"/>GPS・録画</h2><button class="icon-button section-link" aria-label="電波マップを開く" @click="emit('navigate','trips')"><ExternalLink :size="18"/></button></div><div class="gps-status"><div class="gps-orbit"><MapPin :size="27" stroke-width="1.4"/></div><div><strong>{{gpsLabel}}</strong><p>衛星 {{number(gps?.satellites)?gps.satellites:'—'}} / 精度 {{number(gps?.accuracyM)&&gps.accuracyM>0?`${gps.accuracyM} m`:'—'}}</p></div></div><div class="recording-health"><Video :size="21"/><div><strong>{{recording}}</strong><small>最新の映像と録画区間を確認できます</small></div><button class="icon-button section-link" aria-label="ドラレコ再生を開く" @click="emit('navigate','recordings')"><ExternalLink :size="18"/></button></div></section>
  <section class="panel body-panel"><div class="hero-head"><h2><Gauge :size="20"/>本体の状態</h2><button class="icon-button section-link" aria-label="詳細を開く" @click="emit('navigate','system')"><ExternalLink :size="18"/></button></div><div class="device-metrics"><div class="device-meter"><span>CPU</span><strong>{{percent(cpu?.usedPercent)}}</strong><progress v-if="number(cpu?.usedPercent)" max="100" :value="cpu.usedPercent" aria-label="CPU使用率"></progress><span v-else class="unknown-meter" aria-label="CPU使用率は未取得"></span></div><div class="device-meter"><span>RAM</span><strong>{{percent(ram?.usedPercent)}}</strong><progress v-if="number(ram?.usedPercent)" max="100" :value="ram.usedPercent" aria-label="RAM使用率"></progress><span v-else class="unknown-meter" aria-label="RAM使用率は未取得"></span><small>{{ram?`${byteText(ram.usedBytes)} / ${byteText(ram.totalBytes)}`:'計測待ち'}}</small></div><div class="device-meter"><span>本体温度</span><strong>{{temperature===null?'—':`${temperature.toFixed(1)} °C`}}</strong><small>CPU周辺センサー</small></div></div><div class="storage-row"><HardDrive :size="21"/><div><strong>SDカード <span class="connection-badge" :class="{online:summary.sd?.status==='available'}">{{summary.sd?.status==='available'?'利用可能':'状態未取得'}}</span></strong><small>残り {{byteText(sd?.freeBytes)}} / {{byteText(sd?.totalBytes)}}</small></div><span>{{percent(sd?.usedPercent)}} 使用</span></div><div class="storage-row"><Database :size="20"/><div><strong>本体空き容量</strong><small>アプリ・設定の保存領域</small></div><span>{{byteText(storage?.freeBytes)}}</span></div></section>
 </div>
</template>
