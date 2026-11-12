<script setup lang="ts">
import {ExternalLink,MapPin} from '@lucide/vue'
import type {Summary} from '../services/api'
import JourneyChart from './JourneyChart.vue'
import CurrentLocation from './CurrentLocation.vue'
import RecordingStatus from './RecordingStatus.vue'
import DashboardCameras from './DashboardCameras.vue'
import NetworkOverview from './NetworkOverview.vue'
import ResourceMonitor from './ResourceMonitor.vue'
import DeviceStatus from './DeviceStatus.vue'
import '../dashboard-overview.css'
defineProps<{summary:Summary}>()
const emit=defineEmits<{navigate:[page:string]}>()
</script>
<template>
 <div class="dashboard-overview">
  <JourneyChart :summary="summary" show-link @navigate="emit('navigate',$event)"/>
  <section class="panel gps-panel"><div class="hero-head"><h2><MapPin :size="20"/>GPS・録画</h2><button class="icon-button section-link" aria-label="走行履歴を開く" @click="emit('navigate','trips')"><ExternalLink :size="18"/></button></div><div class="dashboard-recording"><DashboardCameras @open="emit('navigate','camera')"><RecordingStatus compact :observation="summary.recording" @open="emit('navigate','recordings')"/></DashboardCameras></div><CurrentLocation :observation="summary.gps"/></section>
  <NetworkOverview :summary="summary" @navigate="emit('navigate',$event)"/>
  <ResourceMonitor :summary="summary" @open="emit('navigate','system')"/>
  <DeviceStatus :summary="summary" @open="emit('navigate','system')"/>
 </div>
</template>
