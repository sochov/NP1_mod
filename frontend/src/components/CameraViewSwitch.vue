<script setup lang="ts">
defineProps<{modelValue:'front'|'in'|'both';disabled?:boolean}>()
const emit=defineEmits<{'update:modelValue':[value:'front'|'in'|'both'];change:[]}>()
function choose(value:'front'|'in'|'both'){emit('update:modelValue',value);emit('change')}
</script>
<template><div class="camera-view-switch drive-view-switch" role="group" aria-label="カメラの表示"><button v-for="item in ([{id:'front',label:'前方'},{id:'in',label:'車内'},{id:'both',label:'並べて表示'}] as const)" :key="item.id" :class="{selected:modelValue===item.id}" :aria-pressed="modelValue===item.id" :disabled="disabled" @click="choose(item.id)">{{item.label}}</button></div></template>
<style scoped>.camera-view-switch{display:inline-flex;gap:3px;padding:3px;background:var(--bg);border:1px solid var(--line);border-radius:6px}.camera-view-switch button{min-height:38px;background:transparent;font-size:14px;font-weight:650;color:var(--muted);padding:7px 14px;border-radius:4px;white-space:nowrap}.camera-view-switch button.selected{color:var(--accent);background:var(--paper);box-shadow:0 1px 4px #0001}@media(max-width:600px){.camera-view-switch{width:100%}.camera-view-switch button{flex:1;padding:7px 10px;min-height:40px}}</style>
