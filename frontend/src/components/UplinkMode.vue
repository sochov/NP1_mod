<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref} from 'vue'
import {Globe2,RefreshCw,Wifi,Zap} from '@lucide/vue'
import {api} from '../services/api'

// Which uplink NP1 uses: automatic (4G when the saved Wi-Fi is out of range, back near it),
// or held on Wi-Fi or on 4G. The switch itself is the stock Wi-Fi STA mode, so stock apps,
// NCC and hotspot clients all follow it.
type Mode='auto'|'wifi'|'cellular'
type Status={at:string;mode:Mode;staOn:boolean|null;wpaState?:string;uplink:'wifi'|'cellular'|'none'|'other';helperAvailable:boolean;error?:string;state:{autoOffAt?:string;switches:number;lastAction?:string;lastReason?:string}}
type View={mode:Mode;status:Status|null}
const view=ref<View>(),loading=ref(true),busy=ref(false),error=ref(''),draft=ref<Mode>('auto')
let timer:ReturnType<typeof setInterval>|undefined
const options:{value:Mode;label:string;detail:string;icon:any}[]=[
 {value:'auto',label:'自動',detail:'Wi-Fiが圏外になったら4Gへ。保存済みWi-Fiの近くに戻ったらWi-Fiへ。',icon:Zap},
 {value:'wifi',label:'Wi-Fi',detail:'常にWi-Fiを使います。圏外の間はインターネットにつながりません。',icon:Wifi},
 {value:'cellular',label:'4G',detail:'常に4Gを使います。Wi-Fi接続は止まります。',icon:Globe2},
]
const status=computed(()=>view.value?.status||undefined)
const uplinkLabel=computed(()=>({wifi:'Wi-Fi',cellular:'4G',none:'未接続',other:'その他'} as Record<string,string>)[status.value?.uplink||'']||'—')
const reasonText:Record<string,string>={'wifi-unavailable':'Wi-Fiが圏外になったため4Gへ','saved-network-seen':'保存済みWi-Fiが見えたためWi-Fiへ','near-last-connected-place':'Wi-Fiの近くに戻ったためWi-Fiへ','owner-manual':'手動の設定に合わせて','auto-disabled':'自動切替を止めたためWi-Fiへ'}
const lastSwitch=computed(()=>{const s=status.value?.state;if(!s?.lastReason)return '';return reasonText[s.lastReason]||s.lastReason})
const pendingMode=computed(()=>!!view.value&&!!status.value&&status.value.mode!==view.value.mode)
async function load(){try{view.value=await api<View>('/network/uplink');if(!busy.value)draft.value=view.value.mode;error.value=''}catch{error.value='回線の設定を取得できません。'}finally{loading.value=false}}
async function choose(mode:Mode){if(busy.value||mode===view.value?.mode)return;busy.value=true;error.value='';draft.value=mode;try{view.value=await api<View>('/network/uplink','PUT',{mode});if(view.value.mode!==mode)throw new Error()}catch{error.value='設定を保存できません。もう一度お試しください。';draft.value=view.value?.mode||'auto'}finally{busy.value=false}}
onMounted(()=>{load();timer=setInterval(()=>{if(!document.hidden)load()},5000)})
onUnmounted(()=>{if(timer)clearInterval(timer)})
</script>

<template>
 <section class="panel uplink-mode" aria-labelledby="uplink-heading" :aria-busy="loading||busy">
  <header class="wifi-section-head"><div><h2 id="uplink-heading"><Globe2 :size="21"/>インターネット回線</h2><p>本体・純正アプリ・ホットスポットにつないだ端末が使う回線です。</p></div><button class="icon-button" aria-label="回線の状態を更新" @click="load"><RefreshCw :size="17"/></button></header>
  <div class="uplink-body">
   <div class="uplink-options" role="radiogroup" aria-label="回線の切替方法">
    <button v-for="option in options" :key="option.value" type="button" role="radio" :aria-checked="draft===option.value" :class="{selected:draft===option.value}" :disabled="busy||loading||!view" @click="choose(option.value)"><component :is="option.icon" :size="18"/><span><strong>{{option.label}}</strong><small>{{option.detail}}</small></span></button>
   </div>
   <dl class="uplink-state">
    <div><dt>使用中の回線</dt><dd :class="'uplink-'+(status?.uplink||'unknown')">{{status?uplinkLabel:'確認中'}}</dd></div>
    <div><dt>Wi-Fi接続（純正のSTA）</dt><dd>{{status?.staOn==null?'—':status.staOn?'オン':'オフ（4G）'}}</dd></div>
    <div v-if="lastSwitch"><dt>直近の切替</dt><dd>{{lastSwitch}}</dd></div>
   </dl>
   <p v-if="pendingMode" class="uplink-note" role="status">設定を反映しています…（10秒ほどかかります）</p>
   <p v-if="status&&!status.helperAvailable" class="uplink-note error">切替に必要な部品が本体にないため、回線を切り替えられません。</p>
   <p v-else-if="status?.error" class="uplink-note error">直前の切替を確認できませんでした。しばらくすると再試行します。</p>
   <p v-if="!status&&!loading" class="uplink-note">切替の状態を確認できません（自動切替サービスの応答待ち）。</p>
   <p v-if="error" class="uplink-note error" role="alert">{{error}}</p>
  </div>
 </section>
</template>

<style scoped>
.uplink-body{padding:22px 28px 24px}
.uplink-options{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:10px}
.uplink-options button{display:flex;align-items:flex-start;gap:11px;padding:14px;border:1px solid var(--line);border-radius:12px;background:var(--paper);color:var(--muted);text-align:left;min-height:44px}
.uplink-options button>svg{flex-shrink:0;margin-top:2px}
.uplink-options button span{display:flex;flex-direction:column;gap:4px;min-width:0}
.uplink-options strong{font-size:14px;color:var(--text)}
.uplink-options small{font-size:11px;line-height:1.6}
.uplink-options button.selected{border-color:var(--accent);background:var(--soft);color:var(--accent)}
.uplink-options button.selected strong{color:var(--accent)}
.uplink-state{display:flex;flex-wrap:wrap;gap:10px 28px;margin:18px 0 0}
.uplink-state>div{display:flex;flex-direction:column;gap:4px}
.uplink-state dt{font-size:11px;color:var(--muted)}
.uplink-state dd{margin:0;font-size:14px;font-weight:650}
.uplink-state dd.uplink-cellular{color:#b45309}
.uplink-state dd.uplink-wifi{color:var(--accent)}
.uplink-note{font-size:12px;color:var(--muted);margin:12px 0 0;line-height:1.7}
.uplink-note.error{color:var(--danger)}
@media(max-width:700px){.uplink-body{padding:18px 20px 20px}.uplink-options{grid-template-columns:1fr}}
</style>
