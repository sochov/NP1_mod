<script setup lang="ts">
import {computed,ref,watch} from 'vue'
import {Check,Save,Wifi,Signal} from '@lucide/vue'
import {api,type Observation} from '../services/api'
type Preference='cellular'|'wifi'
type PreferenceData={settings:{revision:number;preference?:Preference};enabled?:boolean|null;sharing?:{preference?:Preference;ready?:boolean;state?:string;upstream?:{kind:string}}}
const props=defineProps<{observation?:Observation<PreferenceData>;disabled?:boolean}>(),emit=defineEmits<{updated:[value:Observation<PreferenceData>];busy:[value:boolean]}>()
const draft=ref<Preference|''>(''),baseline=ref<Preference|''>(''),busy=ref(false),error=ref(''),message=ref('')
const data=computed(()=>props.observation?.data),preference=computed(()=>data.value?.settings?.preference)
const dirty=computed(()=>draft.value!==baseline.value)
const ready=computed(()=>props.observation?.status==='available'&&['cellular','wifi'].includes(preference.value||'')&&Number.isInteger(data.value?.settings?.revision))
const actualKind=computed(()=>data.value?.sharing?.ready?data.value.sharing.upstream?.kind:undefined)
const actualLabel=computed(()=>actualKind.value==='cellular'?'4G経由':actualKind.value==='wifi'?'Wi-Fi経由':data.value?.enabled===false?'停止中':data.value?.enabled===true?'上流接続待ち':'状態確認中')
const fallback=computed(()=>!!actualKind.value&&!!baseline.value&&actualKind.value!==baseline.value)
watch(preference,value=>{if(value==='cellular'||value==='wifi'){if(!dirty.value||draft.value==='')draft.value=value;baseline.value=value}},{immediate:true})
async function save(){
 if(busy.value||props.disabled||!ready.value||!dirty.value||draft.value==='')return
 const target=draft.value,expectedRevision=data.value!.settings.revision
 busy.value=true;emit('busy',true);error.value='';message.value=''
 try{const value=await api<Observation<PreferenceData>>('/network/hotspot','PUT',{expectedRevision,settings:{preference:target}});if(value.status!=='available'||value.data?.settings?.preference!==target||value.data?.sharing?.preference!==target)throw new Error('優先回線の反映を確認できません。最新の状態を取得してください。');baseline.value=target;draft.value=target;emit('updated',value);message.value='優先回線を保存しました。'}catch(e){error.value=e instanceof Error?e.message.replace(/^\d+:\s*/,''):String(e)}finally{busy.value=false;emit('busy',false)}
}
</script>
<template>
 <section class="hotspot-upstream" aria-label="ホットスポットの優先回線" :aria-busy="busy">
  <div class="hotspot-upstream-head"><h3>インターネット回線</h3><span>現在の共有 <b>{{actualLabel}}</b></span></div>
  <fieldset :disabled="disabled||busy||!ready"><legend>優先する回線</legend><div class="hotspot-upstream-options"><label :class="{selected:draft==='cellular'}"><input v-model="draft" type="radio" value="cellular" name="hotspot-upstream" aria-label="4Gを優先"><Signal :size="19"/><span><strong>4Gを優先</strong><small>4G回線を先に使用します</small></span></label><label :class="{selected:draft==='wifi'}"><input v-model="draft" type="radio" value="wifi" name="hotspot-upstream" aria-label="Wi-Fi STAを優先"><Wifi :size="19"/><span><strong>Wi-Fiを優先</strong><small>接続中のWi-Fi（STA）を先に使用します</small></span></label></div></fieldset>
  <p>優先側を利用できない場合は、もう一方の回線を使用します。</p>
  <p v-if="fallback" class="hotspot-upstream-fallback">{{baseline==='cellular'?'4G':'Wi-Fi'}}優先の設定です。現在は{{actualKind==='cellular'?'4G':'Wi-Fi'}}経由で共有しています。</p>
  <p v-if="!ready" class="hotspot-upstream-fallback">優先回線の設定を取得できていません。</p>
  <div class="hotspot-upstream-actions"><button class="secondary" type="button" :disabled="disabled||busy||!ready||!dirty" @click="save"><Save :size="15"/>{{busy?'反映を確認中…':'優先回線を保存'}}</button><p v-if="message" class="hotspot-upstream-success" role="status"><Check :size="15"/>{{message}}</p></div>
  <p v-if="error" class="error" role="alert">{{error}}</p>
 </section>
</template>
<style scoped>
.hotspot-upstream{padding:23px 25px;border-bottom:1px solid var(--line)}.hotspot-upstream-head{display:flex;align-items:center;justify-content:space-between;gap:15px;margin-bottom:19px}.hotspot-upstream h3{font-size:14px;margin:0;font-weight:650}.hotspot-upstream-head>span{font-size:11px;color:var(--muted);line-height:1.6}.hotspot-upstream-head b{font-size:12px;font-weight:600;color:var(--text);margin-left:7px}.hotspot-upstream fieldset{border:0;padding:0;margin:0;min-width:0}.hotspot-upstream legend{font-size:12px;margin-bottom:11px;color:var(--muted)}.hotspot-upstream-options{display:grid;grid-template-columns:1fr 1fr;gap:12px}.hotspot-upstream-options label{display:flex;align-items:center;gap:11px;border:1px solid var(--line);border-radius:6px;padding:15px;cursor:pointer;min-width:0}.hotspot-upstream-options label.selected{border-color:var(--accent);background:color-mix(in srgb,var(--accent) 5%,var(--paper))}.hotspot-upstream-options input{width:16px;height:16px;margin:0;accent-color:var(--accent);flex-shrink:0}.hotspot-upstream-options svg{flex-shrink:0;color:var(--muted)}.hotspot-upstream-options span{min-width:0}.hotspot-upstream-options strong{font-size:13px;font-weight:650;display:block}.hotspot-upstream-options small{font-size:11px;color:var(--muted);line-height:1.7;display:block;margin-top:5px}.hotspot-upstream p{font-size:11px;color:var(--muted);line-height:1.8;margin:12px 0 0;overflow-wrap:anywhere}.hotspot-upstream fieldset:disabled label{cursor:default;opacity:.65}.hotspot-upstream-fallback{background:var(--bg);padding:10px 13px;border-radius:5px}.hotspot-upstream-actions{display:flex;align-items:center;flex-wrap:wrap;gap:13px;margin-top:16px}.hotspot-upstream-actions button{min-height:44px;font-size:12px}.hotspot-upstream-actions .hotspot-upstream-success{display:flex;gap:7px;align-items:center;color:var(--accent);margin:0}.hotspot-upstream .error{color:var(--danger)}@media(max-width:600px){.hotspot-upstream{padding:21px 18px}.hotspot-upstream-head{align-items:flex-start;flex-direction:column;gap:9px}.hotspot-upstream-options{grid-template-columns:minmax(0,1fr);gap:9px}.hotspot-upstream-options label{padding:14px}.hotspot-upstream-actions{align-items:stretch;flex-direction:column}.hotspot-upstream-actions button{justify-content:center}}
</style>
