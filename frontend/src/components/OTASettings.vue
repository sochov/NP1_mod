<script setup lang="ts">
import { computed,onMounted,onUnmounted,ref } from 'vue'
import { AlertTriangle,CheckCircle2,CircleDashed,ShieldCheck,X } from '@lucide/vue'
import { api,type Observation } from '../services/api'
type Guard={checkedAt:string;flag:{present:boolean;managed:boolean;protected:boolean;heals:number;lastHeal?:string;error?:string};network:{wanted:boolean;active:boolean;uid:number;rejectedPackets:number;error?:string;ipv6Exit:boolean};staging:{workFiles:number;workBytes:number;offeredItems:number;listReadable:boolean;sdTrigger:boolean;firmwareKnown:boolean;updateRunning:boolean;error?:string};alerts:string[]}
type Policy={disabled:boolean;managed:boolean;writable:boolean;token:string;scope:string;protected?:boolean;heals?:number;lastHeal?:string;guard?:Guard|null}
const state=ref<Observation<Policy>>(),loading=ref(true),busy=ref(false),error=ref(''),message=ref(''),confirming=ref(false)
let timer:ReturnType<typeof setInterval>|undefined
function valid(value:Observation<Policy>){return value.status==='available'&&typeof value.data?.disabled==='boolean'&&typeof value.data?.writable==='boolean'&&typeof value.data?.token==='string'&&value.data.token.length===64}
const data=computed(()=>state.value&&valid(state.value)?state.value.data:undefined),guard=computed(()=>data.value?.guard||undefined)
const time=(v?:string)=>v?new Date(v).toLocaleString('ja-JP',{timeZone:'Asia/Tokyo',month:'numeric',day:'numeric',hour:'2-digit',minute:'2-digit'}):'—'
// Each layer is shown as on, off or unknown; nothing is claimed without the guard's latest round.
type Layer={name:string;state:'on'|'off'|'unknown';detail:string}
const layers=computed<Layer[]>(()=>{
 const d=data.value,g=guard.value
 if(!d)return []
 const stopped=d.disabled
 return [
  {name:'更新データ取得の停止',state:stopped?'on':'off',detail:stopped?'純正の更新処理が更新リストを取りに行かないようにしています。':'更新リストの取得を許可しています。'},
  {name:'停止設定の削除防止',state:!stopped?'off':d.protected?'on':'off',detail:!stopped?'—':d.protected?'停止設定を変更不可にしており、純正の処理からも削除できません。':'停止設定を保護できていません。'},
  {name:'停止設定の自動復旧',state:!g?'unknown':g.network.wanted?'on':'off',detail:!g?'見張りの結果を取得できません。':g.network.wanted?(g.flag.heals?`消された停止設定を ${g.flag.heals} 回作り直しました（最終 ${time(g.flag.lastHeal)}）。`:'消された場合は30秒以内に作り直します。'):'—'},
  {name:'更新処理の通信遮断',state:!g?'unknown':!g.network.wanted?'off':g.network.active?'on':'off',detail:!g?'見張りの結果を取得できません。':!g.network.wanted?'更新処理の通信を許可しています。':g.network.active?`純正の更新処理（専用アカウント）の外部通信だけを遮断しています。遮断 ${g.network.rejectedPackets.toLocaleString()} 件。`:'遮断を設定できていません。'},
  {name:'更新データ・ファームウェアの監視',state:!g?'unknown':'on',detail:!g?'見張りの結果を取得できません。':`取得済みの更新データ ${g.staging.workFiles?g.staging.workFiles+' 件':'なし'} · 提供中の更新 ${g.staging.listReadable?(g.staging.offeredItems?g.staging.offeredItems+' 件':'なし'):'不明'} · SDカードの更新ファイル ${g.staging.sdTrigger?'あり':'なし'} · ファームウェア ${g.staging.firmwareKnown?'変化なし':'変化あり'}`},
 ]
})
const alertText:Record<string,string>={
 'flag-missing':'更新データ取得の停止設定がありません。見張りが作り直せない状態です。',
 'flag-unprotected':'停止設定を変更不可にできていません。',
 'network-inactive':'更新処理の通信遮断を設定できていません。',
 'ipv6-unfiltered':'IPv6の外部経路があります。この本体ではIPv6の通信は遮断できません。',
 'staged-package':'本体に取得済みの更新データがあります。電源を切らずに状況を確認してください。',
 'update-offered':'更新サーバーから新しい更新が提供されています。',
 'sd-trigger':'SDカードに更新用のファイル（update.Auth）があります。本体を再起動する前にSDカードから取り除いてください。',
 'firmware-changed':'本体の更新処理が変わっています。ファームウェアが更新された可能性があります。',
 'update-account-changed':'純正の更新処理が想定と違うアカウントで動いています。通信遮断が効いていない可能性があります。',
}
const alerts=computed(()=>(guard.value?.alerts||[]).map(code=>alertText[code]||code))
const guardStale=computed(()=>!!data.value&&!guard.value)
async function load(){if(busy.value)return;loading.value=true;error.value='';try{const value=await api<Observation<Policy>>('/device/ota-policy');state.value=value;if(!valid(value))error.value=value.reason||'本体更新の設定を取得できません。'}catch{state.value=undefined;error.value='本体更新の設定を取得できません。もう一度更新してください。'}finally{loading.value=false}}
async function change(disabled:boolean){if(busy.value||loading.value||!data.value?.writable)return;confirming.value=false;busy.value=true;error.value='';message.value='';try{const value=await api<Observation<Policy>>('/device/ota-policy','PUT',{expectedToken:data.value.token,disabled});state.value=value;if(!valid(value)||value.data?.disabled!==disabled)throw new Error();message.value=disabled?'更新データの取得を停止しました。通信遮断は30秒以内に戻ります。':'更新データの取得を許可しました。通信遮断は30秒以内に解除されます。'}catch{state.value=undefined;error.value='変更後の設定を確認できません。状態を更新してください。'}finally{busy.value=false}}
function key(event:KeyboardEvent){if(event.key==='Escape')confirming.value=false}
onMounted(()=>{load();timer=setInterval(()=>{if(!document.hidden&&!busy.value&&!confirming.value)load()},30000);document.addEventListener('keydown',key)})
onUnmounted(()=>{if(timer)clearInterval(timer);document.removeEventListener('keydown',key)})
</script>
<template>
 <section class="panel space-top ota-settings" :aria-busy="loading||busy">
  <div class="panel-head"><h2><ShieldCheck :size="18"/>本体の自動更新</h2><span v-if="data" class="tag" :class="{warn:!data.disabled||alerts.length}">{{data.disabled?(alerts.length?'要確認':'更新を防止中'):'更新を許可'}}</span></div>
  <div class="settings-body">
   <p>NP1modは、本体のファームウェア更新で動かなくなる可能性があります。導入時から、次の仕組みを重ねて更新を防いでいます。</p>
   <p v-if="loading&&!data" class="hint" role="status">設定を読み込んでいます…</p>
   <div v-if="alerts.length" class="ota-alerts" role="alert"><p v-for="text in alerts" :key="text"><AlertTriangle :size="16"/>{{text}}</p></div>
   <ul v-if="data" class="ota-layers">
    <li v-for="layer in layers" :key="layer.name" :class="layer.state"><CheckCircle2 v-if="layer.state==='on'" :size="17"/><CircleDashed v-else :size="17"/><div><strong>{{layer.name}}<span>{{layer.state==='on'?'有効':layer.state==='off'?'無効':'不明'}}</span></strong><small>{{layer.detail}}</small></div></li>
   </ul>
   <p v-if="guardStale" class="hint">見張り（np1-control-ota-guard）の結果が2分以上更新されていません。停止設定は有効ですが、通信遮断と監視の状態は確認できません。</p>
   <p v-else-if="guard" class="hint">最終確認 {{time(guard.checkedAt)}}。開始済みの更新処理は中断しません。音声・ナビなど、更新以外の通信は遮断しません。</p>
   <template v-if="data">
    <p v-if="!data.writable" class="hint">管理記録を確認できないため、ここからは変更できません。</p>
    <div class="ota-actions"><button v-if="data.disabled" class="secondary" @click="confirming=true" :disabled="busy||loading||!data.writable">{{busy?'変更中…':'更新を許可する（非推奨）'}}</button><button v-else class="primary" @click="change(true)" :disabled="busy||loading||!data.writable">{{busy?'変更中…':'更新を防止する'}}</button><button class="text-button" @click="load" :disabled="busy||loading">状態を更新</button></div>
   </template>
   <button v-else-if="!loading" class="secondary" @click="load">状態を更新</button>
   <p v-if="error" class="error" role="alert">{{error}}</p><p v-if="message" class="hint" role="status">{{message}}</p>
  </div>
  <div v-if="confirming" class="modal-backdrop" @click.self="confirming=false">
   <section class="modal" role="dialog" aria-modal="true" aria-label="本体の更新を許可">
    <div class="modal-title"><h2>本体の更新を許可しますか</h2><button class="icon-button" aria-label="閉じる" @click="confirming=false"><X :size="18"/></button></div>
    <p class="ota-confirm">更新データの取得停止と、更新処理の通信遮断を解除します。本体が自動でファームウェアを更新し、NP1modが動かなくなる可能性があります。</p>
    <div class="ota-actions"><button class="secondary" @click="confirming=false">やめる</button><button class="danger-button" @click="change(false)">許可する</button></div>
   </section>
  </div>
 </section>
</template>
<style scoped>
.ota-settings{margin-bottom:24px}.panel-head h2{display:flex;align-items:center;gap:9px}.panel-head h2>svg{color:var(--accent)}
.tag.warn{color:var(--danger);border-color:color-mix(in srgb,var(--danger) 30%,var(--line))}
.ota-settings p{line-height:1.8;max-width:760px}
.ota-layers{list-style:none;margin:14px 0 6px;padding:0;display:flex;flex-direction:column;border:1px solid var(--line);border-radius:12px;overflow:hidden}
.ota-layers li{display:flex;align-items:flex-start;gap:11px;padding:11px 14px;border-top:1px solid var(--line)}
.ota-layers li:first-child{border-top:0}
.ota-layers li>svg{flex-shrink:0;margin-top:2px;color:var(--muted)}
.ota-layers li.on>svg{color:var(--ok)}
.ota-layers li.off>svg{color:var(--danger)}
.ota-layers li>div{display:flex;flex-direction:column;gap:2px;min-width:0}
.ota-layers strong{display:flex;align-items:center;gap:8px;font-size:13px;font-weight:650}
.ota-layers strong span{font-size:11px;font-weight:600;color:var(--muted)}
.ota-layers li.on strong span{color:var(--ok)}
.ota-layers li.off strong span{color:var(--danger)}
.ota-layers small{font-size:12px;color:var(--muted);line-height:1.6;overflow-wrap:anywhere}
.ota-alerts{display:flex;flex-direction:column;gap:6px;margin-top:12px;padding:12px 14px;border-radius:12px;background:var(--danger-soft);border:1px solid color-mix(in srgb,var(--danger) 26%,var(--line))}
.ota-alerts p{display:flex;align-items:flex-start;gap:8px;margin:0;font-size:13px;color:var(--danger)}
.ota-alerts svg{flex-shrink:0;margin-top:3px}
.ota-actions{display:flex;align-items:center;flex-wrap:wrap;gap:14px;margin-top:18px}.ota-actions button{min-height:44px;font-size:13px}
.ota-confirm{line-height:1.8;margin:0}
.danger-button{min-height:44px;padding:0 18px;border-radius:var(--radius-md);background:var(--danger);color:#fff;font-weight:650}
@media(max-width:520px){.ota-actions{align-items:stretch;flex-direction:column;gap:10px}.ota-actions button{width:100%;justify-content:center}}
</style>
