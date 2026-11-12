<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref} from 'vue'
import {ExternalLink,Video} from '@lucide/vue'
import type {Observation} from '../services/api'

type Recording={writing?:boolean|null;cameras?:Record<string,boolean|null>;writingObservedAt?:string;writingReason?:string;finalizedClips?:number}
const props=defineProps<{observation?:Observation<Recording>}>(),emit=defineEmits<{open:[]}>()
const now=ref(Date.now());let timer:ReturnType<typeof setInterval>|undefined
const state=computed(()=>{
 const observation=props.observation,at=Date.parse(observation?.data?.writingObservedAt||observation?.observedAt||'')
 if(observation?.status!=='available'||!Number.isFinite(at)||now.value-at>30000||at-now.value>2000||observation.data?.writingReason)return 'unknown'
 return observation.data?.writing===true?'recording':observation.data?.writing===false?'stopped':'checking'
})
const label=computed(()=>({recording:'録画中',stopped:'録画停止中',checking:'録画を確認中',unknown:'録画状態を取得できません'})[state.value])
const cameras=computed(()=>state.value==='recording'||state.value==='stopped'?(['front','in'] as const).map(id=>({id,label:id==='front'?'前方':'車内',writing:props.observation?.data?.cameras?.[id]})):[])
onMounted(()=>timer=setInterval(()=>now.value=Date.now(),1000))
onUnmounted(()=>{if(timer)clearInterval(timer)})
</script>
<template>
 <div class="recording-health" :class="state" role="status" aria-label="録画状態"><Video :size="21"/><div class="recording-copy"><strong><i class="recording-dot"></i>{{label}}</strong><small v-if="cameras.some(camera=>camera.writing!==undefined)"><span v-for="camera in cameras" :key="camera.id">{{camera.label}} {{camera.writing===true?'録画中':camera.writing===false?'更新なし':'確認中'}}</span></small><small v-else-if="state==='stopped'">直近の録画ファイル更新がありません</small><small v-else-if="state==='checking'">録画ファイルの更新を確認しています</small><small v-if="Number.isFinite(observation?.data?.finalizedClips)">保存済み {{observation?.data?.finalizedClips?.toLocaleString('ja-JP')}} 件</small></div><button class="icon-button section-link" aria-label="ドラレコ再生を開く" @click="emit('open')"><ExternalLink :size="18"/></button></div>
</template>
<style scoped>
.recording-copy{min-width:0}.recording-health strong{display:flex;align-items:center;gap:7px;font-size:14px;font-weight:650}.recording-dot{display:block;width:8px;height:8px;flex-shrink:0;border-radius:50%;background:var(--muted)}.recording .recording-dot{background:#df6262;box-shadow:0 0 0 3px #df626218}.recording small{color:var(--muted)}.recording-copy small>span+span{margin-left:12px}.recording-health>.section-link{align-self:flex-start}.recording-health{flex-wrap:nowrap;align-items:flex-start}.recording-health>svg{margin-top:3px}.recording-health>.section-link{margin-top:-10px}@media(max-width:520px){.recording-health>.section-link{margin-left:auto}.recording-health{gap:10px}.recording-health strong{font-size:13px}.recording-copy small>span{display:inline-block}}
</style>
