<script setup lang="ts">
import { computed,onMounted,ref } from 'vue'
import { api,type Observation } from '../services/api'
type AlexaData={wakeWord:{value:number;enabled:boolean;fixed:boolean};writable:boolean;reason?:string;linkage:{status:string;source:string}}
const state=ref<Observation<AlexaData>>(),draft=ref(true),loading=ref(true),busy=ref(false),error=ref(''),message=ref('')
function valid(value?:Observation<AlexaData>){const word=value?.data?.wakeWord;return value?.status==='available'&&word&&[0,1,2].includes(word.value)&&typeof word.enabled==='boolean'&&typeof word.fixed==='boolean'&&word.enabled===(word.value!==1)&&word.fixed===(word.value===2)}
const writable=computed(()=>valid(state.value)&&state.value?.data.writable&&!state.value.data.wakeWord.fixed)
const changed=computed(()=>valid(state.value)&&draft.value!==state.value?.data.wakeWord.enabled)
const stateLabel=computed(()=>!valid(state.value)?'未取得':state.value!.data.wakeWord.fixed?'ON（連携中）':state.value!.data.wakeWord.enabled?'ON':'OFF')
function receive(value:Observation<AlexaData>){state.value=value;if(valid(value)){draft.value=value.data.wakeWord.enabled;return true}return false}
async function load(){if(busy.value)return;loading.value=true;error.value='';message.value='';try{const value=await api<Observation<AlexaData>>('/device/alexa');if(!receive(value))error.value=value.reason||'Alexaの設定を取得できません。'}catch{state.value=undefined;error.value='Alexaの設定を取得できません。状態を更新してください。'}finally{loading.value=false}}
async function apply(){if(busy.value||loading.value||!writable.value||!changed.value||!state.value)return;busy.value=true;error.value='';message.value='';const enabled=draft.value;try{const value=await api<Observation<AlexaData>>('/device/alexa','PUT',{expectedValue:state.value.data.wakeWord.value,enabled});if(!receive(value)||value.data.wakeWord.enabled!==enabled)throw new Error();message.value='ウェイクワード認識の設定を保存しました。'}catch{state.value=undefined;error.value='変更後の設定を確認できません。状態を更新してください。'}finally{busy.value=false}}
onMounted(load)
</script>
<template>
 <section class="panel alexa-settings" :aria-busy="loading||busy">
  <div class="panel-head"><h2>Alexa</h2><span class="tag">{{stateLabel}}</span></div>
  <div class="settings-body">
   <p>「アレクサ」の呼びかけを認識するかを設定します。</p>
   <p class="hint">AmazonへのログインとNP1との連携は、スマートフォンのAlexaアプリで行います。</p>
   <p v-if="loading" class="hint" role="status">純正サービスから設定を取得しています…</p>
   <template v-if="valid(state)">
    <p v-if="state?.data.wakeWord.fixed" class="alexa-note">Alexa連携中のため、認識はONに固定されています。現在の連携状態では変更できません。</p>
    <p v-else-if="!writable" class="hint">{{state?.data.reason||'現在は設定を変更できません。'}}</p>
    <fieldset :disabled="!writable||loading||busy" class="alexa-choice"><legend>呼びかけの認識</legend><label :class="{selected:draft}"><input type="radio" v-model="draft" :value="true" name="alexa-wake-word">認識する（ON）</label><label :class="{selected:!draft}"><input type="radio" v-model="draft" :value="false" name="alexa-wake-word">認識しない（OFF）</label></fieldset>
    <div class="alexa-actions"><button class="primary" @click="apply" :disabled="!writable||!changed||loading||busy">{{busy?'保存中…':'設定を保存'}}</button><button class="text-button" @click="load" :disabled="busy||loading">状態を更新</button></div>
   </template>
   <button v-else-if="!loading" class="secondary" @click="load">状態を更新</button>
   <p v-if="error" class="error" role="alert">{{error}}</p><p v-if="message" class="hint" role="status">{{message}}</p>
  </div>
 </section>
</template>
<style scoped>
.alexa-settings p{line-height:1.8;max-width:700px}.alexa-choice{border:0;padding:0;margin:24px 0 0;display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px;max-width:620px;min-width:0}.alexa-choice legend{font-weight:700;font-size:14px;margin-bottom:12px}.alexa-choice label{display:flex;align-items:center;gap:10px;min-height:48px;border:1px solid var(--line);border-radius:7px;padding:12px;font-size:14px;cursor:pointer}.alexa-choice label.selected{background:var(--soft);border-color:var(--accent)}.alexa-choice input{width:18px;height:18px;flex-shrink:0;accent-color:var(--accent)}.alexa-choice:disabled{opacity:.6}.alexa-actions{display:flex;align-items:center;gap:18px;margin-top:24px}.alexa-actions button{min-height:44px;font-size:14px}.alexa-note{border-left:3px solid var(--accent);padding:10px 14px;background:var(--soft)}@media(max-width:520px){.alexa-choice{grid-template-columns:1fr;gap:10px}.alexa-actions{align-items:stretch;flex-direction:column;gap:10px}.alexa-actions button{width:100%;justify-content:center}}
</style>
