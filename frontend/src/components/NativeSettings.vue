<script setup lang="ts">
import { computed,onMounted,ref } from 'vue'
import { Check,RefreshCw,Settings2 } from '@lucide/vue'
import { api,type Observation } from '../services/api'
import { nativePages } from '../services/native'
type Item={id:string;label:string;description?:string;kind:'boolean'|'select'|'range'|'info';value:unknown;valueLabel?:string;options?:{value:unknown;label:string}[];min?:number;max?:number;step?:number;writable:boolean;reason?:string;group:string;status?:string}
type NativeData={items:Item[];revision?:number;scope?:string}
const props=defineProps<{section?:string}>()
const state=ref<Observation<NativeData>>(),drafts=ref<Record<string,unknown>>({}),loading=ref(false),saving=ref(''),error=ref(''),notice=ref('')
const items=computed(()=>{
 const all=state.value?.data?.items||[],page=nativePages.find(p=>p.id===props.section)
 if(!page)return all
 if(page.id==='installation')return page.items.map((id,i)=>all.find(item=>item.id===id)||({id,label:['車種','取付高さ','取付位置','取付方向'][i]!,group:'車両・取付',kind:'info',value:null,writable:false,status:'unknown',reason:'純正アプリの取得・変更経路を解析中です。'} as Item))
 return all.filter(item=>page.items.includes(item.id))
})
const groups=computed(()=>[...new Set(items.value.map(i=>i.group))])
const count=computed(()=>items.value.filter(i=>i.value!==null&&i.value!==undefined).length)
function display(item:Item){if(item.value===null||item.value===undefined)return '未取得';if(item.valueLabel)return item.valueLabel;if(typeof item.value==='boolean')return item.value?'ON':'OFF';const option=item.options?.find(o=>o.value===item.value);return option?.label||String(item.value)}
function editable(item:Item){return state.value?.status==='available'&&item.status==='available'&&item.writable&&item.value!==null&&item.value!==undefined&&!loading.value&&!saving.value}
function changed(item:Item){return JSON.stringify(drafts.value[item.id])!==JSON.stringify(item.value)}
function receive(value:Observation<NativeData>){state.value=value;drafts.value=Object.fromEntries((value.data?.items||[]).map(i=>[i.id,i.value]))}
async function load(){if(loading.value||saving.value)return;loading.value=true;error.value='';notice.value='';try{receive(await api<Observation<NativeData>>('/device/native-settings'))}catch(e){state.value=undefined;error.value='純正設定を取得できませんでした。'+String(e)}finally{loading.value=false}}
async function apply(item:Item){if(!editable(item)||!changed(item))return;const wanted=drafts.value[item.id];saving.value=item.id;error.value='';notice.value='';try{receive(await api<Observation<NativeData>>('/device/native-settings/'+encodeURIComponent(item.id),'PUT',{expectedValue:item.value,value:wanted}));const current=state.value?.data?.items.find(i=>i.id===item.id);if(state.value?.status!=='available'||current?.status!=='available'||JSON.stringify(current.value)!==JSON.stringify(wanted))throw new Error('変更後の値を確認できません。状態を更新してください。');notice.value=item.label+'を変更し、端末から再取得しました。'}catch(e){state.value=undefined;error.value='変更を確認できませんでした。状態を更新してください。'+String(e)}finally{saving.value=''}}
onMounted(load)
</script>
<template>
 <div class="native-toolbar"><p>純正アプリの設定を確認します。取得した値と、変更できる項目を区別して表示します。</p><button class="secondary" @click="load" :disabled="loading||!!saving"><RefreshCw :size="16"/>{{loading?'読込み中…':'状態を更新'}}</button></div>
 <p v-if="error" class="banner error" role="alert">{{error}}</p><p v-if="notice" class="banner success" role="status"><Check :size="17"/>{{notice}}</p>
 <div v-if="loading&&!state" class="panel native-empty" role="status">純正設定を読み込んでいます…</div>
 <template v-else-if="items.length">
  <p v-if="state?.reason" class="hint">{{state.reason}}</p><div class="native-summary"><span><strong>{{count}}</strong> / {{items.length}} 項目を取得</span><span>変更と再取得を確認できた項目だけ操作できます</span></div>
  <section v-for="group in groups" :key="group" class="panel native-group"><div class="panel-head"><h2><Settings2 :size="18"/>{{group}}</h2><span class="muted">純正NP1の設定</span></div>
   <div v-for="item in items.filter(i=>i.group===group)" :key="item.id" class="native-row"><div class="native-description"><h3>{{item.label}}</h3><p v-if="item.description">{{item.description}}</p></div>
    <div class="native-value"><template v-if="item.writable&&item.kind!=='info'">
     <label v-if="item.kind==='boolean'" class="native-toggle"><input type="checkbox" v-model="drafts[item.id]" :disabled="!editable(item)" :aria-label="item.label">{{drafts[item.id]?'ON':'OFF'}}</label>
     <select v-else-if="item.kind==='select'" v-model="drafts[item.id]" :disabled="!editable(item)" :aria-label="item.label"><option v-for="option in item.options" :key="String(option.value)" :value="option.value">{{option.label}}</option></select>
     <label v-else class="native-range"><input type="range" v-model.number="drafts[item.id]" :min="item.min" :max="item.max" :step="item.step||1" :disabled="!editable(item)" :aria-label="item.label"><output>{{drafts[item.id]}}</output></label>
     <button class="primary" @click="apply(item)" :disabled="!editable(item)||!changed(item)">{{saving===item.id?'確認中…':'変更を適用'}}</button>
    </template><template v-else><strong>{{display(item)}}</strong><span v-if="item.value!==null&&item.value!==undefined" class="native-badge">読取り</span></template></div>
    <p v-if="item.reason" class="native-reason">{{item.reason}}</p>
   </div>
  </section>
 </template>
 <section v-else-if="!loading" class="panel native-empty"><h2>純正設定はまだ取得できません</h2><p>{{state?.reason||'対応する設定取得経路を確認しています。'}}</p><button class="secondary" @click="load">もう一度取得</button></section>
</template>
<style scoped>
.native-toolbar{display:flex;justify-content:space-between;align-items:center;gap:20px;margin-bottom:20px}.native-toolbar p{font-size:13px;color:var(--muted)}.native-toolbar button{white-space:nowrap;min-height:44px}.native-summary{display:flex;justify-content:space-between;gap:16px;align-items:center;margin:20px 0;font-size:12px;color:var(--muted)}.native-summary strong{font-size:20px;color:var(--text);font-weight:650}.native-group{margin-bottom:20px}.native-group .panel-head h2{display:flex;align-items:center;gap:10px}.native-group .panel-head h2 svg{color:var(--accent)}.native-group .panel-head .muted{font-size:11px}.native-row{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px 22px;padding:20px 24px;border-top:1px solid var(--line)}.native-description h3{font-size:14px;font-weight:650}.native-description p{font-size:12px;color:var(--muted);margin-top:6px}.native-value{display:flex;align-items:center;gap:12px;flex-wrap:wrap;justify-content:flex-end}.native-value>strong{font-size:16px;font-weight:550}.native-badge{font-size:10px;background:var(--bg);border:1px solid var(--line);padding:4px 7px;border-radius:4px;color:var(--muted)}.native-reason{grid-column:1/-1;font-size:11px;color:var(--muted);line-height:1.7}.native-value .primary{font-size:12px;min-height:44px}.native-value select{max-width:220px;min-height:44px}.native-toggle{display:flex;align-items:center;gap:10px;min-height:44px}.native-toggle input{width:20px;height:20px;accent-color:var(--accent)}.native-range{display:flex;align-items:center;gap:10px}.native-range input{min-height:44px;padding:0;max-width:160px;accent-color:var(--accent)}.native-empty{padding:35px;text-align:center}.native-empty h2{font-size:17px}.native-empty p{margin:14px auto;color:var(--muted);font-size:13px;max-width:560px}@media(max-width:600px){.native-toolbar{align-items:flex-start;flex-direction:column;gap:12px}.native-toolbar button{width:100%}.native-summary{align-items:flex-start;flex-direction:column;gap:5px}.native-row{grid-template-columns:1fr;padding:18px 20px;gap:12px}.native-value{justify-content:flex-start}.native-reason{font-size:11px}.native-value .primary{margin-left:auto}.native-group .panel-head .muted{display:none}.native-empty{padding:25px 20px}}
</style>
