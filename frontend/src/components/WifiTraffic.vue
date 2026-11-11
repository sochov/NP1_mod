<script setup lang="ts">
import type { Summary } from '../services/api'
import { useTraffic,networkTotal,trafficVolume,trafficSpeed } from '../services/traffic'
const props=defineProps<{summary:Summary}>()
const {wifi,stale}=useTraffic(()=>props.summary)
</script>
<template>
 <div class="wifi-traffic">
  <div class="wifi-volume"><span>本日 <strong data-testid="wifi-today">{{trafficVolume(networkTotal(wifi,'today'))}}</strong></span><span>今月 <strong data-testid="wifi-month">{{trafficVolume(networkTotal(wifi,'month'))}}</strong></span></div>
  <div class="wifi-speed"><span>↓ <strong data-testid="wifi-download">{{trafficSpeed(wifi,'rxBytesPerSecond',stale)}}</strong></span><span>↑ <strong data-testid="wifi-upload">{{trafficSpeed(wifi,'txBytesPerSecond',stale)}}</strong></span></div>
 </div>
</template>
