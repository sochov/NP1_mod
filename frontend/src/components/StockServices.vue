<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref} from 'vue'
import {Check,Info,LoaderCircle,Play,RefreshCw,RotateCw,Square} from '@lucide/vue'
import {api,type Observation} from '../services/api'

type Action='start'|'stop'|'restart'
type StockService={id:string;name:string;label:string;description:string;impact:string;state:string;subState:string;mainPID:number;invocationID:string;loadState:string;unitFileState:string;canStart:boolean;canStop:boolean;canRestart:boolean;controlReason?:string}
type Result={status:string;action:Action;service:StockService;observedAt:string}
const emit=defineEmits<{refresh:[]}>()
const items=ref<StockService[]>([]),loaded=ref(false),loading=ref(false),loadError=ref(''),observedAt=ref(''),pending=ref<{id:string;action:Action}>(),feedback=ref<{id:string;error?:string;success?:string}>()
const verbs:Record<Action,string>={start:'起動',stop:'停止',restart:'再起動'}
const actions:Action[]=['start','stop','restart']
const icons={start:Play,stop:Square,restart:RotateCw}
let active=true,inFlight=false,generation=0,controller:AbortController|undefined,poll:ReturnType<typeof setInterval>|undefined
const activeCount=computed(()=>items.value.filter(item=>item.state==='active').length)
const timeLabel=computed(()=>{const time=Date.parse(observedAt.value);return Number.isFinite(time)?new Date(time).toLocaleTimeString('ja-JP',{hour:'2-digit',minute:'2-digit',second:'2-digit'}):''})
function validItem(item:StockService){return !!item&&typeof item.id==='string'&&item.id.length>0&&typeof item.name==='string'&&typeof item.state==='string'&&typeof item.invocationID==='string'&&Number.isInteger(item.mainPID)&&item.mainPID>=0}
function stateLabel(item:StockService){if(item.loadState==='not-found')return '未導入';return ({active:'稼働中',inactive:'停止',failed:'異常停止',activating:'起動中',deactivating:'停止中',reloading:'再読込中'})[item.state]||'状態未取得'}
function can(item:StockService,action:Action){if(pending.value||loadError.value||item.loadState!=='loaded'||!['active','inactive','failed'].includes(item.state))return false;if(action==='start'&&item.state==='active'||action==='stop'&&item.state==='inactive')return false;return action==='start'?item.canStart===true:action==='stop'?item.canStop===true:item.canRestart===true}
function processLabel(item:StockService){if(item.mainPID>0)return 'PID '+item.mainPID;if(item.loadState==='not-found')return 'この本体では未導入';return ['inactive','failed'].includes(item.state)?'プロセス停止':'プロセス情報未取得'}
function reason(e:unknown){return e instanceof Error?e.message.replace(/^\d+:\s*/,''):String(e)}
async function load(){
 if(inFlight||pending.value||!active)return
 inFlight=true;loading.value=true;const version=generation;controller=new AbortController()
 try{const value=await api<Observation<StockService[]>>('/system/services','GET',undefined,true,controller.signal);if(!active||version!==generation||pending.value)return;if(value.status!=='available'||!Array.isArray(value.data)||!value.data.every(validItem))throw new Error(value.reason||'サービスの状態を取得できません。');items.value=value.data;observedAt.value=value.observedAt;loaded.value=true;loadError.value=''}
 catch(e){if(active&&version===generation&&!pending.value)loadError.value=reason(e)}finally{inFlight=false;if(active)loading.value=false}
}
function confirmed(value:Result,before:StockService,action:Action){
 if(value.status!=='applied'||value.action!==action||!validItem(value.service)||value.service.id!==before.id||value.service.name!==before.name||!Number.isFinite(Date.parse(value.observedAt)))return false
 const after=value.service
 if(action==='stop')return after.state==='inactive'&&after.mainPID===0
 if(after.state!=='active'||after.mainPID<=0)return false
 if(action==='restart')return after.mainPID!==before.mainPID||!!after.invocationID&&after.invocationID!==before.invocationID
 return true
}
async function apply(item:StockService,action:Action){
 if(!can(item,action))return
 generation++;controller?.abort();pending.value={id:item.id,action};feedback.value=undefined
 try{
  const value=await api<Result>('/system/services/'+encodeURIComponent(item.id)+'/actions','POST',{action,expectedInvocationID:item.invocationID,expectedState:item.state})
  if(!confirmed(value,item,action))throw new Error('変更後のサービス状態を確認できません。状態を更新してください。')
  if(active){items.value=items.value.map(current=>current.id===item.id?value.service:current);observedAt.value=value.observedAt;feedback.value={id:item.id,success:`${item.label||item.name}を${verbs[action]}しました。`};emit('refresh')}
 }catch(e){if(active)feedback.value={id:item.id,error:reason(e)}}finally{if(active){pending.value=undefined;await load()}}
}
onMounted(()=>{load();poll=setInterval(load,5000)})
onUnmounted(()=>{active=false;clearInterval(poll);controller?.abort()})
</script>

<template>
 <section class="panel stock-services space-top" :aria-busy="!!pending">
  <header class="panel-head"><div><h2>純正サービス</h2><p v-if="loaded">{{items.length}} 件中 {{activeCount}} 件が稼働中<span v-if="timeLabel"> · {{timeLabel}} 確認</span></p></div><button class="icon-button" :disabled="!!pending||loading" aria-label="純正サービスの状態を更新" @click="load"><RefreshCw :size="17" :class="{spinning:loading}"/></button></header>
  <p v-if="loadError" class="stock-service-load-error error" role="alert">{{loadError}}</p>
  <p v-if="!loaded&&!loadError" class="stock-service-empty" role="status">サービスの状態を読み込んでいます…</p>
  <p v-else-if="loaded&&!items.length" class="stock-service-empty">操作対象のサービスがありません。</p>
  <div v-if="items.length" class="stock-services-grid">
  <article v-for="item in items" :key="item.id" class="stock-service-item" :aria-busy="pending?.id===item.id">
   <div class="stock-service-heading"><div><h3>{{item.label||item.name}}</h3><span class="stock-service-unit mono">{{item.name}}</span></div><span class="stock-service-state" :class="{online:item.state==='active',failed:item.state==='failed'}">{{pending?.id===item.id?verbs[pending.action]+'を確認中':stateLabel(item)}}</span></div>
   <p v-if="item.description" class="stock-service-description">{{item.description}}</p>
   <p v-if="item.impact" class="stock-service-impact"><Info :size="14"/><span>{{item.impact}}</span></p>
   <aside v-if="item.id==='qmmf'" class="stock-camera-order" role="note" aria-label="カメラ系サービスの操作順">
    <strong>カメラ系サービスの操作順</strong>
    <p><b>停止</b> カメラ利用サービス → 映像・音声イベント → カメラ基盤</p>
    <p><b>起動</b> カメラ基盤 → 映像・音声イベント → カメラ利用サービス</p>
    <details><summary>カメラを利用するサービス</summary><p>録画、映像認識、リモート監視、対話（バーチャルパッセンジャー）、純正システム。</p></details>
   </aside>
   <p v-if="item.controlReason" class="stock-service-progress">{{item.controlReason}}</p>
   <div class="stock-service-bottom"><span class="stock-service-pid">{{processLabel(item)}}<span v-if="item.subState&&item.subState!==item.state&&!['running','dead'].includes(item.subState)"> · {{item.subState}}</span></span><div class="stock-service-actions"><button v-for="action in actions" :key="action" type="button" :class="['secondary',{'stock-stop':action==='stop'}]" :disabled="!can(item,action)" :aria-label="(item.label||item.name)+'を'+verbs[action]" @click="apply(item,action)"><LoaderCircle v-if="pending?.id===item.id&&pending.action===action" :size="14" class="spinning"/><component v-else :is="icons[action]" :size="14"/>{{verbs[action]}}</button></div></div>
   <p v-if="pending?.id===item.id" class="stock-service-progress" role="status">実際のサービス状態を確認しています…</p>
   <p v-if="feedback?.id===item.id&&feedback.error" class="stock-service-result error" role="alert">{{feedback.error}}</p><p v-if="feedback?.id===item.id&&feedback.success" class="stock-service-result stock-service-success" role="status"><Check :size="15"/>{{feedback.success}}</p>
  </article>
  </div>
 </section>
</template>

<style scoped>
.stock-camera-order{margin-top:14px;padding:12px 14px;border:1px solid var(--line);border-radius:6px;background:var(--bg);font-size:11px;line-height:1.8;overflow-wrap:anywhere}.stock-camera-order>strong{font-size:12px;font-weight:650}.stock-camera-order p{margin:8px 0 0}.stock-camera-order p b{font-weight:650;margin-right:7px}.stock-camera-order summary{cursor:pointer;color:var(--muted);padding-top:10px;min-height:28px;box-sizing:border-box}
.stock-service-impact span{min-width:0;overflow-wrap:anywhere}.stock-service-description{overflow-wrap:anywhere}
.stock-services .panel-head{align-items:center}.stock-services h2{font-size:15px}.stock-services .panel-head p{font-size:11px;color:var(--muted);margin:7px 0 0;line-height:1.6}.stock-service-item{padding:22px 24px;border-top:1px solid var(--line)}.stock-service-item:first-of-type{border-top:0}.stock-service-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:15px}.stock-service-heading>div{min-width:0}.stock-service-heading h3{font-size:15px;font-weight:650;margin:0 0 7px}.stock-service-unit{font-size:11px;color:var(--muted);overflow-wrap:anywhere;line-height:1.7}.stock-service-state{font-size:11px;white-space:nowrap;padding:4px 8px;border-radius:4px;background:var(--bg);color:var(--muted);line-height:1.6}.stock-service-state.online{background:color-mix(in srgb,var(--accent) 10%,var(--paper));color:var(--accent)}.stock-service-state.failed{color:var(--danger);background:color-mix(in srgb,var(--danger) 10%,var(--paper))}.stock-service-description{font-size:12px;line-height:1.8;margin:14px 0 6px;color:var(--text)}.stock-service-impact{display:flex;align-items:flex-start;gap:7px;font-size:11px;line-height:1.8;color:var(--muted);margin:0}.stock-service-impact>svg{flex-shrink:0;margin-top:3px}.stock-service-bottom{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-top:17px}.stock-service-pid{font-size:11px;color:var(--muted);overflow-wrap:anywhere}.stock-service-actions{display:flex;gap:8px;flex-shrink:0}.stock-service-actions button{min-height:42px;font-size:12px;padding:9px 13px;gap:6px}.stock-service-actions .stock-stop:not(:disabled){color:var(--danger)}.stock-service-actions button:disabled{color:var(--muted);background:var(--bg);opacity:.65}.stock-service-result,.stock-service-progress{font-size:12px;line-height:1.8;margin:13px 0 0;overflow-wrap:anywhere}.stock-service-success{display:flex;align-items:center;gap:7px;color:var(--accent)}.stock-service-empty,.stock-service-load-error{padding:20px 24px;font-size:12px;line-height:1.8;margin:0}.stock-service-progress{color:var(--muted)}.spinning{animation:stock-service-spin 1.1s linear infinite}@keyframes stock-service-spin{to{transform:rotate(360deg)}}@media(prefers-reduced-motion:reduce){.spinning{animation:none}}@media(max-width:620px){.stock-service-item{padding:20px 18px}.stock-service-bottom{align-items:stretch;flex-direction:column;gap:13px}.stock-service-actions{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:8px}.stock-service-actions button{justify-content:center;padding:10px 7px;min-height:44px}.stock-services .panel-head{padding:19px 18px}.stock-service-heading{gap:10px}.stock-service-heading h3{line-height:1.6}.stock-service-load-error,.stock-service-empty{padding:20px 18px}}
</style>
<style scoped>
.stock-services-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,300px),1fr));gap:12px;padding:16px}.stock-services-grid>.stock-service-item{display:flex;flex-direction:column;border:1px solid var(--line);border-radius:7px;padding:16px;min-width:0}.stock-services-grid .stock-service-heading{gap:8px}.stock-services-grid .stock-service-bottom{margin-top:auto;padding-top:16px;flex-direction:column;align-items:stretch;gap:10px}.stock-services-grid .stock-service-actions{display:flex;width:100%;gap:6px}.stock-services-grid .stock-service-actions>button{flex:1;min-width:0;padding:9px 8px;font-size:12px;gap:5px}@media(max-width:600px){.stock-services-grid{grid-template-columns:1fr;padding:12px}.stock-services-grid>.stock-service-item{padding:16px}}
</style>
