<script setup lang="ts">
import {computed} from 'vue'
import {ChevronRight,ExternalLink,Network,ShieldCheck,Wifi,WifiHigh} from '@lucide/vue'
import type {Summary} from '../services/api'
import {useTraffic,networkTotal,trafficVolume} from '../services/traffic'
import {apLabel,wifiLabel,vpnLabel} from '../services/network'
const props=defineProps<{summary:Summary}>(),emit=defineEmits<{navigate:[page:string]}>()
const {wifi:usage}=useTraffic(()=>props.summary)
const data=(key:string)=>['available','stale'].includes(props.summary[key]?.status)?props.summary[key]?.data:undefined
const rows=computed(()=>{
 const hotspot=data('hotspot'),wifi=data('wifi'),vpn=data('tailscale'),month=networkTotal(usage.value,'month')
 return [
  {id:'ap',page:'wifi',icon:WifiHigh,kind:'Wi-Fi AP',name:hotspot?.ssid||'接続情報待ち',detail:typeof hotspot?.clientCount==='number'?`接続端末 ${hotspot.clientCount} 台`:'接続端末 —',state:apLabel(hotspot),online:hotspot?.enabled===true},
  {id:'sta',page:'wifi',icon:Wifi,kind:'Wi-Fi STA',name:wifi?.ssid||'未接続',detail:`今月 ${trafficVolume(month)}`,state:wifiLabel(wifi),online:wifi?.wpa_state==='COMPLETED'},
  {id:'vpn',page:'vpn',icon:ShieldCheck,kind:'Tailscale',name:vpn?.deviceName||'接続情報待ち',detail:vpn?.ips?.find((ip:string)=>ip.includes('.'))||'アドレス —',state:vpnLabel(vpn),online:vpn?.online===true}
 ]
})
</script>
<template>
 <section class="panel network-summary" aria-label="ネットワーク">
  <div class="hero-head"><h2><Network :size="20"/>ネットワーク</h2><button class="icon-button section-link" aria-label="Wi-Fi / APを開く" @click="emit('navigate','wifi')"><ExternalLink :size="18"/></button></div>
  <ul>
   <li v-for="row in rows" :key="row.id"><button type="button" class="network-row" :data-network="row.id" @click="emit('navigate',row.page)">
    <span class="network-icon" :class="{online:row.online}"><component :is="row.icon" :size="19"/></span>
    <span class="network-text"><small>{{row.kind}}</small><strong :title="row.name">{{row.name}}</strong><small class="network-detail">{{row.detail}}</small></span>
    <span class="connection-badge" :class="{online:row.online}">{{row.state}}</span><ChevronRight :size="16"/>
   </button></li>
  </ul>
 </section>
</template>
<style scoped>
.network-summary{display:flex;flex-direction:column;min-width:0}
ul{list-style:none;margin:0;padding:6px;display:flex;flex-direction:column;flex:1;justify-content:space-evenly;gap:2px}
.network-row{display:flex;align-items:center;gap:12px;width:100%;min-height:58px;padding:7px 8px 7px 10px;border-radius:12px;background:transparent;text-align:left;transition:background-color .15s}
.network-row:hover{background:color-mix(in srgb,var(--text) 5%,transparent)}
.network-icon{display:flex;align-items:center;justify-content:center;width:38px;height:38px;border-radius:12px;background:var(--bg);color:var(--muted);flex-shrink:0}
.network-icon.online{background:var(--soft);color:var(--accent)}
.network-text{display:flex;flex-direction:column;flex:1;min-width:0;line-height:1.35}
.network-text small{font-size:11px;color:var(--muted)}
.network-text strong{font-size:14px;font-weight:650;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.network-detail{font-variant-numeric:tabular-nums;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.network-row>.connection-badge{flex-shrink:0;font-size:11px}
.network-row>svg{color:var(--muted);flex-shrink:0}
</style>
