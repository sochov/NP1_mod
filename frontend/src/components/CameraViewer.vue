<script setup lang="ts">
import {computed,nextTick,ref} from 'vue'
import {Camera,X} from '@lucide/vue'
import CameraViewSwitch from './CameraViewSwitch.vue'
import LiveCamera from './LiveCamera.vue'
type CameraID='front'|'in'
const view=ref<CameraID|'both'>('front'),format=ref<'h264'|'jpeg'>('h264'),color=ref('auto'),playing=ref(false),busy=ref<Partial<Record<CameraID,boolean>>>({})
const cameras=computed<CameraID[]>(()=>view.value==='both'?['front','in']:[view.value]),loading=computed(()=>cameras.value.some(camera=>busy.value[camera]))
const children:Partial<Record<CameraID,InstanceType<typeof LiveCamera>>>={}
function keep(camera:CameraID,value:unknown){if(value)children[camera]=value as InstanceType<typeof LiveCamera>;else{delete children[camera];delete busy.value[camera]}}
async function stop(){playing.value=false;await Promise.all(Object.values(children).map(child=>child?.stop()));await nextTick()}
defineExpose({stop})
</script>
<template>
 <div class="panel live-camera-toolbar"><CameraViewSwitch v-model="view" :disabled="loading"/><div class="live-camera-options"><select v-model="format" aria-label="映像形式" :disabled="loading"><option value="h264">標準（H.264）</option><option value="jpeg">軽量（JPEG）</option></select><select v-if="format==='jpeg'" v-model="color" aria-label="映像の色調" :disabled="loading"><option value="auto">色調: 自動補正</option><option value="source">色調: 元映像</option></select><button v-if="!playing" class="primary" @click="playing=true" :disabled="loading"><Camera :size="16"/>映像を表示</button><button v-else class="secondary" @click="stop"><X :size="16"/>表示を停止</button></div></div>
 <div class="live-camera-grid" :class="{dual:view==='both'}"><LiveCamera v-for="camera in cameras" :key="camera" :ref="value=>keep(camera,value)" :source="camera" :format="format" :color="color" :playing="playing" @busy="value=>busy[camera]=value" @fallback="format='jpeg'"/></div>
 <p v-if="playing&&format==='h264'" class="hint live-camera-note">純正録画の映像を表示しています。ライブ音声は未対応です。</p>
</template>
<style scoped>.live-camera-toolbar{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:16px 20px;margin-bottom:20px;flex-wrap:wrap}.live-camera-options{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.live-camera-options select,.live-camera-options button{min-height:44px}.live-camera-grid{display:grid;grid-template-columns:minmax(0,1fr);gap:20px}.live-camera-grid.dual{grid-template-columns:repeat(2,minmax(0,1fr))}.live-camera-grid :deep(.panel-head){gap:10px;flex-wrap:wrap}.live-camera-grid :deep(.panel-head h2){font-size:16px}.live-camera-grid :deep(.tag){font-size:11px}.live-camera-note{padding:16px 2px 0;margin:0}@media(max-width:850px){.live-camera-grid.dual{grid-template-columns:minmax(0,1fr)}}@media(max-width:600px){.live-camera-toolbar{padding:12px;gap:12px}.live-camera-options{width:100%}.live-camera-options select,.live-camera-options button{flex:1;min-width:120px}.live-camera-grid{gap:16px}}</style>
