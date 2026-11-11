// Isolated browser fixture; excluded from the production entry point.
import { createApp } from 'vue'
import NativeAudioSettings from '../../src/components/NativeAudioSettings.vue'
import '../../src/style.css'
import '../../src/neutral.css'

document.title='Synthetic native audio review'
document.body.innerHTML='<p style="padding:12px 16px;margin:0;color:var(--muted)">合成データ・音声設定レビュー</p><div id="app" style="padding:16px;max-width:1100px;margin:auto"></div>'
createApp(NativeAudioSettings).mount('#app')
