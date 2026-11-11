<script setup lang="ts">
import { computed,onMounted,ref } from 'vue'
import { api,type Observation } from '../services/api'

type LEDSettings={revision:number;mode:'off'|'solid'|'rainbow';brightness:number;speed:number;fps:number;rgb:number[];autostart:boolean}
const state=ref<Observation<any>>(),draft=ref<LEDSettings>({revision:1,mode:'off',brightness:180,speed:240,fps:15,rgb:[255,255,255],autostart:false}),color=ref('#ffffff'),busy=ref(false),loading=ref(true),error=ref(''),message=ref('')
const ready=computed(()=>state.value?.status==='available'&&valid(state.value.data?.settings))
const modeName=computed(()=>({off:'消灯',solid:'単色',rainbow:'レインボー'})[state.value?.data?.settings?.mode as LEDSettings['mode']]||'未取得')
const serviceLabel=computed(()=>({active:'動作中',inactive:'停止中',failed:'エラー',activating:'起動中',deactivating:'停止処理中'})[state.value?.data?.serviceState as string]||'未取得')
function valid(value:any):value is LEDSettings{return value&&Number.isInteger(value.revision)&&['off','solid','rainbow'].includes(value.mode)&&Array.isArray(value.rgb)&&value.rgb.length===3&&value.rgb.every((n:any)=>Number.isInteger(n)&&n>=0&&n<=255)&&Number.isFinite(value.brightness)&&Number.isFinite(value.speed)&&Number.isFinite(value.fps)}
function receive(value:Observation<any>){state.value=value;if(value.status==='available'&&valid(value.data?.settings)){draft.value={...value.data.settings,rgb:[...value.data.settings.rgb]};color.value='#'+draft.value.rgb.map(n=>n.toString(16).padStart(2,'0')).join('');return true}return false}
function reason(e:unknown){const value=e instanceof Error?e.message:String(e);if(value.includes('REVISION_CONFLICT'))return '別の操作で設定が変わりました。状態を更新して、もう一度設定してください。';return value}
async function load(){if(busy.value)return;loading.value=true;error.value='';message.value='';try{if(!receive(await api<Observation<any>>('/device/led')))error.value='LEDの設定を取得できません。状態を更新してください。'}catch(e){state.value=undefined;error.value='設定の取得に失敗しました。'+reason(e)}finally{loading.value=false}}
async function apply(mode?:'off'){if(busy.value||loading.value||!ready.value)return;busy.value=true;error.value='';message.value='';try{const next={...draft.value,mode:mode||draft.value.mode,rgb:[0,2,4].map(n=>parseInt(color.value.slice(n+1,n+3),16))};if(!receive(await api<Observation<any>>('/device/led','PUT',{expectedRevision:draft.value.revision,settings:next})))throw new Error('結果を確認できません。状態を更新してください。');message.value=next.mode==='off'?'消灯の設定を送信しました。':'点灯の設定を送信しました。'}catch(e){state.value=undefined;error.value='適用結果を確認できません。'+reason(e)}finally{busy.value=false}}
onMounted(load)
</script>
<template>
 <section class="panel led-panel" :aria-busy="loading||busy">
  <div class="panel-head"><h2>前面LED</h2><span class="tag">5か所のライト</span></div>
  <div class="settings-body">
   <p class="hint led-intro">本体前面の色と明るさを設定します。消灯すると、前面のライトがすべて消えます。純正表示への復帰には対応していません。</p>
   <p v-if="loading" class="hint" role="status">LEDの設定を読み込んでいます…</p>
   <fieldset :disabled="!ready||loading||busy" class="led-fields">
    <label>表示モード<select v-model="draft.mode"><option value="off">消灯</option><option value="solid">単色</option><option value="rainbow">レインボー</option></select></label>
    <template v-if="draft.mode!=='off'"><label>明るさ <output>{{Math.round(draft.brightness/255*100)}}%</output><input v-model.number="draft.brightness" type="range" min="1" max="255" aria-label="明るさ"></label>
    <label v-if="draft.mode==='solid'">色<input type="color" v-model="color" aria-label="LEDの色"></label>
    <template v-if="draft.mode==='rainbow'"><label>色の変化速度 <output>1周 {{(360/draft.speed).toFixed(1)}} 秒</output><input v-model.number="draft.speed" type="range" min="15" max="360" step="15" aria-label="色の変化速度"></label><label>滑らかさ <output>{{draft.fps}} 段階 / 秒</output><input v-model.number="draft.fps" type="range" min="1" max="18" aria-label="滑らかさ"></label></template>
    <label class="checkbox"><input type="checkbox" v-model="draft.autostart">再起動後もこの設定で点灯</label></template>
   </fieldset>
   <div class="led-actions"><button class="primary" @click="apply()" :disabled="!ready||loading||busy">{{busy?'適用中…':'設定を適用'}}</button><button class="secondary" @click="apply('off')" :disabled="!ready||loading||busy">消灯</button><button class="text-button" @click="load" :disabled="loading||busy">状態を更新</button></div>
   <p v-if="error" class="error led-feedback" role="alert">{{error}}</p><p v-if="message" class="hint led-feedback" role="status">{{message}}</p>
   <dl class="details full led-state"><div><dt>現在の設定</dt><dd>{{modeName}}</dd></div><div><dt>LED制御</dt><dd>{{serviceLabel}}</dd></div></dl><p class="hint">表示しているのは端末が受け付けた設定です。実際の点灯状態は取得できないため、本体でも確認してください。</p>
  </div>
 </section>
 <section class="panel space-top"><div class="panel-head"><h2>音声</h2></div><div class="settings-body"><p>SDカードの録画は音声付きです。「ドラレコ再生」で映像と一緒に再生できます。</p><p class="hint">ライブ映像の音声には、まだ対応していません。</p></div></section>
</template>
<style scoped>
.led-fields{border:0;margin:0;padding:0;min-width:0;max-width:560px}.led-fields:disabled{opacity:.55}.led-fields label{font-size:14px}.led-fields input[type=range]{padding:0;min-height:44px;width:100%;accent-color:var(--accent);cursor:pointer}.led-fields input[type=color]{height:46px;width:100%;max-width:180px;padding:5px}.led-fields output{color:var(--muted);font-size:12px}.led-fields .checkbox{min-height:44px;display:flex;align-items:center}.led-fields .checkbox input{width:20px;height:20px;flex-shrink:0;accent-color:var(--accent)}.led-actions{display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin:20px 0}.led-actions button{min-height:44px;min-width:88px;font-size:13px}.led-feedback{font-size:13px;line-height:1.8;margin:14px 0}.led-state{margin-top:22px}.led-intro{margin-top:0;margin-bottom:20px;max-width:650px}@media(max-width:520px){.led-actions{gap:10px}.led-actions .primary,.led-actions .secondary{flex:1}.led-actions .text-button{width:100%;justify-content:center}.led-state{font-size:12px}}
</style>
