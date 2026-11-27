<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref} from 'vue'
import {api,bytes,date} from '../services/api'

// Clip caches synthesized with the VOICEVOX API: plate rule messages and
// vocabulary, and the fixed PICS messages. Missing lists only fixed words and
// the owner's messages, never a detected plate.
type Cache={style:number;total:number;ready:number;bytes:number;missing:string[];state:string;error?:string;updatedAt?:string}
type Status={keyConfigured:boolean;plate:Cache;pics:Cache}
type Target='plate'|'pics'
const status=ref<Status>(),busy=ref<Target|''>(''),confirming=ref<Target|''>(''),error=ref(''),message=ref('')
const errorText=(e:unknown)=>e instanceof Error?e.message:String(e)
const targets:{id:Target;title:string;hint:string}[]=[
 {id:'plate',title:'ナンバーの読み上げ',hint:'条件のメッセージと、全地名・数字・英字・ひらがなの部品'},
 {id:'pics',title:'高度化PICS',hint:'信号の定型文と秒数'},
]
const states:Record<string,string>={ready:'準備済み',generating:'生成中',waiting:'待機中',error:'エラー','no-key':'APIキー未設定',idle:'待機中'}
const valid=(c:unknown):c is Cache=>!!c&&typeof (c as Cache).total==='number'&&typeof (c as Cache).ready==='number'&&Array.isArray((c as Cache).missing)
function receive(s:Status){if(!s||!valid(s.plate)||!valid(s.pics))throw new Error('音声キャッシュの状況を取得できませんでした');status.value=s}
const active=computed(()=>!!status.value&&[status.value.plate,status.value.pics].some(c=>c.ready<c.total&&['generating','waiting','idle'].includes(c.state)))
let timer:number|undefined
function schedule(){window.clearTimeout(timer);timer=window.setTimeout(load,active.value?3000:30000)}
async function load(){try{receive(await api<Status>('/voicevox/cache'));if(!busy.value)error.value=''}catch(e){error.value=errorText(e)}finally{schedule()}}
async function run(target:Target,action:'refresh'|'regenerate'){
 busy.value=target;confirming.value='';error.value='';message.value=''
 try{receive(await api<Status>(`/voicevox/cache/${target}/${action}`,'POST'));message.value=action==='regenerate'?'削除しました。VOICEVOX APIで作り直しています':'不足分の取得を始めました'}
 catch(e){error.value=errorText(e)}finally{busy.value='';schedule()}
}
const cache=(id:Target)=>status.value?.[id]
const percent=(c:Cache)=>c.total?Math.round(c.ready/c.total*100):0
onMounted(load)
onUnmounted(()=>window.clearTimeout(timer))
</script>

<template>
 <section id="voice-cache" class="panel voice-cache" :aria-busy="!!busy">
  <div class="panel-head"><h2>音声キャッシュ</h2></div>
  <div class="settings-body">
   <p class="hint">VOICEVOX APIで生成し、本体に保存した音声です。API設定の保存後や起動時に、不足分を自動で生成します。読み上げは保存済みの音声だけを使います。</p>
   <p v-if="status&&!status.keyConfigured" class="hint warn" role="status">APIキーが未設定のため生成できません。</p>
   <div class="voice-cache-list">
    <article v-for="t in targets" :key="t.id" class="voice-cache-item" :aria-label="t.title+'の音声キャッシュ'">
     <template v-if="cache(t.id)">
      <header><h3>{{t.title}}</h3><span class="voice-cache-state" :class="cache(t.id)!.state">{{states[cache(t.id)!.state]||cache(t.id)!.state}}</span></header>
      <p class="hint">{{t.hint}}</p>
      <progress max="100" :value="percent(cache(t.id)!)" :aria-label="t.title+'の準備状況'"></progress>
      <p class="voice-cache-count"><strong>{{cache(t.id)!.ready}} / {{cache(t.id)!.total}}</strong> 件<span>{{bytes(cache(t.id)!.bytes)}}</span><span v-if="cache(t.id)!.updatedAt">更新 {{date(cache(t.id)!.updatedAt)}}</span></p>
      <p v-if="cache(t.id)!.error" class="error">{{cache(t.id)!.error}}</p>
      <details v-if="cache(t.id)!.missing.length"><summary>未取得（{{cache(t.id)!.total-cache(t.id)!.ready}}件）</summary><p class="voice-cache-missing">{{cache(t.id)!.missing.join('、')}}<template v-if="cache(t.id)!.total-cache(t.id)!.ready>cache(t.id)!.missing.length"> ほか</template></p></details>
      <div v-if="confirming===t.id" class="voice-cache-confirm" role="group" :aria-label="t.title+'の削除の確認'">
       <p>{{cache(t.id)!.total}}件を削除して作り直します。APIのポイントを消費し、揃うまで読み上げられない案内があります。</p>
       <button type="button" class="secondary" @click="confirming=''">やめる</button>
       <button type="button" class="danger" :disabled="!!busy" @click="run(t.id,'regenerate')">削除して作り直す</button>
      </div>
      <div v-else class="voice-cache-actions">
       <button type="button" class="secondary" :disabled="!!busy||!status?.keyConfigured" @click="run(t.id,'refresh')">{{busy===t.id?'実行中…':'不足分を今すぐ取得'}}</button>
       <button type="button" class="secondary" :disabled="!!busy||!status?.keyConfigured" @click="confirming=t.id">削除して作り直す…</button>
      </div>
     </template>
    </article>
   </div>
   <p v-if="error" class="error" role="alert">{{error}}</p><p v-if="message" class="hint" role="status">{{message}}</p>
  </div>
 </section>
</template>

<style scoped>
.voice-cache-list{width:100%;max-width:760px;display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,300px),1fr));gap:12px}
.voice-cache-item{display:grid;gap:8px;align-content:start;min-width:0;padding:12px;border:1px solid var(--line);border-radius:8px}
.voice-cache-item header{display:flex;align-items:center;justify-content:space-between;gap:8px}
.voice-cache-item h3{margin:0;font-size:15px}
.voice-cache-item .hint{margin:0}
.voice-cache-item progress{width:100%}
.voice-cache-state{font-size:13px;font-weight:600;color:#9a6a1f}
.voice-cache-state.ready{color:var(--accent)}
.voice-cache-state.error,.voice-cache-state.no-key{color:var(--danger)}
.voice-cache-count{display:flex;flex-wrap:wrap;gap:4px 12px;margin:0;font-size:13px}
.voice-cache-count span{color:var(--muted)}
.voice-cache-missing{margin:6px 0 0;font-size:13px;overflow-wrap:anywhere}
.voice-cache-actions,.voice-cache-confirm{display:flex;flex-wrap:wrap;align-items:center;gap:8px}
.voice-cache-confirm p{flex-basis:100%;margin:0;font-size:13px}
.warn{color:#9a6a1f}
.voice-cache-confirm .danger{background:var(--danger);border-color:var(--danger);color:#fff}
:global([data-theme=dark]) .voice-cache-state:not(.ready):not(.error):not(.no-key),:global([data-theme=dark]) .voice-cache .warn{color:#e2b56c}
@media (prefers-color-scheme:dark){:global(:root:not([data-theme=light]):not([data-theme=dark])) .voice-cache-state:not(.ready):not(.error):not(.no-key),:global(:root:not([data-theme=light]):not([data-theme=dark])) .voice-cache .warn{color:#e2b56c}}
</style>
