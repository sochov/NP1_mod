<script setup lang="ts">
import { computed } from 'vue'
import { ArrowUpRight, CircleHelp } from '@lucide/vue'
import { date, type Observation } from '../services/api'
const props=defineProps<{title:string;observation?:Observation;featured?:boolean}>()
const labels:Record<string,string>={available:'取得済み',unknown:'未確認',unsupported:'非対応','permission-denied':'権限不足',busy:'使用中',error:'エラー',stale:'古いデータ'}
const label=computed(()=>labels[props.observation?.status||'unknown']||'未確認')
</script>
<template><article class="observation" :class="{featured}"><div class="card-head"><h3>{{title}}</h3><span class="status" :class="observation?.status||'unknown'"><i></i>{{label}}</span></div><div class="observation-body"><slot v-if="observation?.status==='available'"></slot><div v-else class="unavailable"><CircleHelp :size="22"/><p>{{observation?.reason||'まだ実測データがありません。'}}</p></div></div><footer><span :title="observation?.source">{{observation?.source||'実機アダプター'}}</span><span>{{date(observation?.observedAt)}} · {{observation?.refreshSeconds||10}}s</span></footer></article></template>
