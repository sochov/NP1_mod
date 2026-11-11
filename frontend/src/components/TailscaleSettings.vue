<script setup lang="ts">
import {computed,ref,watch} from 'vue'
import {ArrowUpRight,Check,Network,RefreshCw,Save,ShieldCheck,UserRound} from '@lucide/vue'
import {api,type Observation} from '../services/api'

type TailscaleData={online?:boolean;backendState?:string;ips?:string[];accountName?:string;accountLogin?:string;deviceName?:string;hostName?:string;dnsName?:string;nameWritable?:boolean;activePeerConnections?:string[]}
const props=defineProps<{observation?:Observation<TailscaleData>}>(),emit=defineEmits<{refresh:[]}>()
const current=ref(props.observation),draft=ref(props.observation?.data?.hostName||''),expected=ref(props.observation?.data?.hostName||''),busy=ref(false),loginBusy=ref(false),error=ref(''),message=ref(''),loginURL=ref('')
const data=computed(()=>current.value?.data),dirty=computed(()=>draft.value!==expected.value)
const validName=computed(()=>/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(draft.value))
const writable=computed(()=>current.value?.status==='available'&&data.value?.nameWritable===true&&!!expected.value)
const changedElsewhere=computed(()=>typeof data.value?.hostName==='string'&&data.value.hostName!==expected.value)
const account=computed(()=>data.value?.accountLogin||data.value?.accountName||'アカウント未取得')
const backend=computed(()=>({Running:'稼働中',NeedsLogin:'認証待ち',NeedsMachineAuth:'端末の承認待ち',Stopped:'停止中',Starting:'起動中',NoState:'状態未取得'})[data.value?.backendState||'']||'状態未取得')
const needsLogin=computed(()=>['NeedsLogin','Stopped'].includes(data.value?.backendState||''))
const nameDiffers=computed(()=>!!data.value?.deviceName&&!!data.value?.hostName&&data.value.deviceName!==data.value.hostName)
watch(()=>props.observation,value=>{current.value=value;if(!dirty.value&&!busy.value){draft.value=value?.data?.hostName||'';expected.value=draft.value}})
function reset(){draft.value=data.value?.hostName||'';expected.value=draft.value;error.value='';message.value=''}
function reason(e:unknown){return e instanceof Error?e.message.replace(/^\d+:\s*/,''):String(e)}
async function rename(){
 if(busy.value||!writable.value||!validName.value||!dirty.value||changedElsewhere.value)return
 busy.value=true;error.value='';message.value=''
 const hostName=draft.value,expectedHostName=expected.value,differentRegistration=nameDiffers.value
 try{const value=await api<Observation<TailscaleData>>('/vpn/tailscale/name','PUT',{expectedHostName,hostName});if(value.status!=='available'||value.data?.hostName!==hostName)throw new Error('名前の反映をまだ確認できません。状態を更新してください。');current.value=value;expected.value=hostName;draft.value=hostName;message.value=differentRegistration?'ホスト名の変更を確認しました。':'機器名の変更を確認しました。';emit('refresh')}
 catch(e){error.value=reason(e)}finally{busy.value=false}
}
async function login(){loginBusy.value=true;error.value='';try{const value=await api<{authURL:string}>('/vpn/tailscale/login','POST');if(value.authURL){const url=new URL(value.authURL);if(url.protocol!=='https:'||url.hostname!=='login.tailscale.com')throw new Error('認証先を確認できません。');loginURL.value=url.href}else emit('refresh')}catch(e){error.value=reason(e)}finally{loginBusy.value=false}}
</script>

<template>
 <div class="tailscale-page">
  <section class="panel tailscale-overview">
   <header class="panel-head"><h2>Tailscale / 遠隔アクセス</h2><span class="connection-badge" :class="{online:data?.online===true}">{{data?.online===true?'接続済み':data?.online===false?'未接続':'状態未取得'}}</span></header>
   <div class="tailscale-identities">
    <div class="tailscale-identity"><ShieldCheck :size="23"/><div><span>登録機器名</span><strong>{{data?.deviceName||'機器名未取得'}}</strong><small v-if="data?.dnsName">{{data.dnsName}}</small></div></div>
    <div class="tailscale-identity tailscale-account"><UserRound :size="23"/><div><span>アカウント</span><strong>{{account}}</strong><small v-if="data?.accountName&&data.accountLogin&&data.accountName!==data.accountLogin">{{data.accountName}}</small></div></div>
   </div>
   <dl class="tailscale-connection"><div><dt>接続状態</dt><dd>{{backend}}</dd></div><div><dt>管理アドレス</dt><dd><span v-for="ip in data?.ips||[]" :key="ip" class="tailscale-address mono">{{ip}}</span><span v-if="!data?.ips?.length">未割当</span></dd></div></dl>
   <div class="tailscale-access-note"><Network :size="17"/><p>別の端末から開く場合は、同じtailnetへ接続してください。</p></div>
   <div v-if="needsLogin||loginURL" class="tailscale-login"><button v-if="needsLogin" class="secondary" :disabled="loginBusy||busy||current?.status!=='available'" @click="login">{{loginBusy?'接続を準備しています…':data?.backendState==='Stopped'?'Tailscaleに接続':'Tailscaleの認証を開始'}}<ArrowUpRight :size="15"/></button><a v-if="loginURL" :href="loginURL" target="_blank" rel="noreferrer" class="primary">Tailscaleでログインする<ArrowUpRight :size="15"/></a></div>
  </section>
  <section class="panel tailscale-name" :aria-busy="busy">
   <header class="panel-head"><h2>機器名の設定</h2><button class="icon-button" :disabled="busy" aria-label="Tailscaleの状態を更新" @click="emit('refresh')"><RefreshCw :size="17"/></button></header>
   <form @submit.prevent="rename">
    <label for="tailscale-host-name">{{nameDiffers?'ホスト名':'機器名'}}</label>
    <p v-if="nameDiffers">この機器から通知する名前です。管理コンソールの登録機器名と異なる場合があります。</p>
    <input id="tailscale-host-name" v-model="draft" autocomplete="off" autocapitalize="none" spellcheck="false" maxlength="63" placeholder="例: np1-car" :disabled="busy||!writable" :aria-invalid="dirty&&!validName" aria-describedby="tailscale-name-format">
    <small id="tailscale-name-format">英小文字・数字・ハイフン、1〜63文字。先頭と末尾のハイフンは使えません。</small>
    <p v-if="nameDiffers" class="tailscale-name-note">登録機器名は <b>{{data?.deviceName}}</b>、現在のホスト名は <b>{{data?.hostName}}</b> です。</p>
    <p v-if="!writable" class="tailscale-name-note">{{current?.reason||'現在は名前を変更できません。Tailscaleの接続状態を確認してください。'}}</p>
    <p v-if="changedElsewhere" class="tailscale-name-note">別の操作で名前が変わりました。最新の名前を読み込んでから変更してください。</p>
    <div class="tailscale-actions"><button type="submit" class="primary" :disabled="busy||!writable||!validName||!dirty||changedElsewhere"><Save :size="16"/>{{busy?'反映を確認中…':'機器名を変更'}}</button><button v-if="dirty||changedElsewhere" type="button" class="text-button" :disabled="busy" @click="reset">{{changedElsewhere?'最新の名前を読み込む':'入力を戻す'}}</button></div>
   </form>
  </section>
  <p v-if="error" class="banner error" role="alert">{{error}}</p><p v-if="message" class="banner success" role="status"><Check :size="17"/>{{message}}</p>
 </div>
</template>

<style scoped>
.tailscale-identities .tailscale-account strong{font-size:16px;font-weight:600}.tailscale-actions .primary:disabled{color:var(--muted);background:var(--bg);border:1px solid var(--line);opacity:1}
.tailscale-page{max-width:1040px;display:flex;flex-direction:column;gap:20px}.tailscale-page h2{font-size:15px}.tailscale-identities{display:grid;grid-template-columns:1fr 1fr;gap:26px;padding:27px 25px}.tailscale-identity{display:flex;align-items:flex-start;gap:13px;min-width:0}.tailscale-identity>svg{color:var(--muted);flex-shrink:0;margin-top:3px}.tailscale-identity>div{min-width:0}.tailscale-identity span{font-size:12px;color:var(--muted)}.tailscale-identity strong{display:block;font-size:19px;font-weight:650;margin:8px 0;overflow-wrap:anywhere;line-height:1.5}.tailscale-identity small{display:block;color:var(--muted);font-size:12px;overflow-wrap:anywhere;line-height:1.6}.tailscale-connection{display:grid;grid-template-columns:1fr 2fr;gap:24px;padding:22px 25px;margin:0;border-top:1px solid var(--line)}.tailscale-connection dt{font-size:12px;color:var(--muted);margin-bottom:10px}.tailscale-connection dd{margin:0;font-size:14px;line-height:1.6;overflow-wrap:anywhere}.tailscale-address{display:block;font-size:13px}.tailscale-access-note{display:flex;gap:9px;align-items:flex-start;padding:0 25px 22px;color:var(--muted)}.tailscale-access-note>svg{flex-shrink:0;margin-top:2px}.tailscale-access-note p{font-size:12px;line-height:1.7;margin:0}.tailscale-login{padding:0 25px 24px;display:flex;flex-wrap:wrap;gap:12px}.tailscale-name form{padding:25px;display:flex;flex-direction:column;align-items:flex-start;gap:12px}.tailscale-name label{font-size:14px;font-weight:650}.tailscale-name p{font-size:12px;color:var(--muted);line-height:1.8;margin:0;overflow-wrap:anywhere}.tailscale-name input{width:100%;max-width:490px;min-height:46px;font-size:16px;box-sizing:border-box}.tailscale-name input[aria-invalid=true]{border-color:var(--danger)}.tailscale-name small{font-size:11px;color:var(--muted);line-height:1.8}.tailscale-name-note{background:var(--bg);border-radius:5px;padding:10px 13px}.tailscale-name-note b{font-weight:550;color:var(--text)}.tailscale-actions{display:flex;flex-wrap:wrap;align-items:center;gap:15px;margin-top:7px}.tailscale-actions button,.tailscale-login button,.tailscale-login a{min-height:44px}.tailscale-page .banner{margin:0;overflow-wrap:anywhere}
@media(max-width:600px){.tailscale-identities,.tailscale-connection{grid-template-columns:minmax(0,1fr);gap:23px;padding:22px 18px}.tailscale-identity strong{font-size:18px}.tailscale-name form{padding:22px 18px}.tailscale-access-note,.tailscale-login{padding-left:18px;padding-right:18px}.tailscale-actions{width:100%;align-items:stretch;flex-direction:column;gap:9px}.tailscale-actions button{justify-content:center;width:100%}.tailscale-page .panel-head{padding:19px 18px}}
</style>
