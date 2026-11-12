<script setup lang="ts">
import {computed} from 'vue'
import {ExternalLink,ShieldCheck,Wifi,WifiHigh} from '@lucide/vue'
import type {Summary} from '../services/api'
import TrafficUsage from './TrafficUsage.vue'
import WifiTraffic from './WifiTraffic.vue'
import {apLabel as apState,wifiLabel as wifiState,vpnLabel as vpnState} from '../services/network'
const props=defineProps<{summary:Summary}>(),emit=defineEmits<{navigate:[page:string]}>()
const data=(key:string)=>['available','stale'].includes(props.summary[key]?.status)?props.summary[key]?.data:undefined
const wifi=computed(()=>data('wifi')),vpn=computed(()=>data('tailscale')),hotspot=computed(()=>data('hotspot'))
const apLabel=computed(()=>apState(hotspot.value))
const wifiLabel=computed(()=>wifiState(wifi.value))
const wifiChannel=computed(()=>{const f=wifi.value?.freq;if(typeof f!=='number'||!Number.isInteger(f)||f<=0)return '';const channel=f===2484?14:f>=2412&&f<=2472?(f-2407)/5:f>=4910&&f<=4980?(f-4000)/5:f>=5005&&f<=5895?(f-5000)/5:f===5935?2:f>=5955&&f<=7115?(f-5950)/5:NaN;return Number.isInteger(channel)?`${channel}ch（${f} MHz）`:`${f} MHz`})
const vpnLabel=computed(()=>vpnState(vpn.value))
const vpnPaths=computed(()=>{const paths=vpn.value?.activePeerConnections;return Array.isArray(paths)&&paths.length?[...new Set(paths.map((p:string)=>p==='Direct'?'直接':p==='DERP'?'リレー':'確認中'))].join(' / '):vpn.value?.online===true?'通信待ち':'—'})
</script>
<template>
 <section class="panel network-overview" aria-label="ネットワーク接続">
  <TrafficUsage compact :summary="summary" @navigate="emit('navigate',$event)"/>
  <section class="connection-card network-ap"><div class="connection-title"><WifiHigh :size="17"/><h3>Wi-Fi AP</h3><span class="connection-badge" :class="{online:hotspot?.enabled===true}">{{apLabel}}</span><button class="icon-button section-link" aria-label="ホットスポットを開く" @click="emit('navigate','wifi')"><ExternalLink :size="17"/></button></div><strong class="connection-primary" :title="hotspot?.ssid">{{hotspot?.ssid||'接続情報待ち'}}</strong><dl><div><dt>接続端末</dt><dd>{{typeof hotspot?.clientCount==='number'?`${hotspot.clientCount} 台`:'—'}}</dd></div><div><dt>動作</dt><dd>{{['always-on','hotspot-auto-uplink'].includes(hotspot?.mode)?'常時テザリング':hotspot?.mode==='LAN-always-available'?'LANを維持':'—'}}</dd></div></dl></section>
  <section class="connection-card network-sta"><div class="connection-title"><Wifi :size="17"/><h3>Wi-Fi STA</h3><span class="connection-badge" :class="{online:wifi?.wpa_state==='COMPLETED'}">{{wifiLabel}}</span><button class="icon-button section-link" aria-label="Wi-Fiの詳細を開く" @click="emit('navigate','wifi')"><ExternalLink :size="17"/></button></div><div class="sta-identity"><strong class="connection-primary" :title="wifi?.ssid">{{wifi?.ssid||'未接続'}}</strong><span v-if="wifiChannel" class="sta-channel">{{wifiChannel}}</span></div><WifiTraffic volume-only :summary="summary"/></section>
  <section class="connection-card network-vpn"><div class="connection-title"><ShieldCheck :size="17"/><h3>Tailscale</h3><span class="connection-badge" :class="{online:vpn?.online===true}">{{vpnLabel}}</span><button class="icon-button section-link" aria-label="Tailscaleを開く" @click="emit('navigate','vpn')"><ExternalLink :size="17"/></button></div><strong class="connection-primary" :title="vpn?.deviceName">{{vpn?.deviceName||'接続情報待ち'}}</strong><dl><div class="tailscale-dashboard-account"><dt>アカウント</dt><dd :title="vpn?.accountLogin||vpn?.accountName">{{vpn?.accountLogin||vpn?.accountName||'—'}}</dd></div><div><dt>アドレス</dt><dd class="mono">{{vpn?.ips?.find((ip:string)=>ip.includes('.'))||'—'}}</dd></div><div><dt>通信経路</dt><dd>{{vpnPaths}}</dd></div></dl></section>
 </section>
</template>
<style scoped>
.network-overview .sta-identity{display:flex;align-items:baseline;gap:10px;margin:12px 0}.network-overview .sta-identity .connection-primary{margin:0;min-width:0}.sta-channel{flex-shrink:0;font-size:11px;line-height:1.6;color:var(--muted);white-space:nowrap}
</style>
