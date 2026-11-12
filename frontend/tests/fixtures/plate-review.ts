// Isolated browser fixture; excluded from the production entry point.
import { createApp } from 'vue'
import PlateRecognition from '../../src/components/PlateRecognition.vue'
import '../../src/style.css'
import '../../src/neutral.css'

document.title='Synthetic plate recognition review'
document.body.innerHTML='<p style="padding:12px 16px;margin:0;color:var(--muted)">合成データ・ナンバー認識レビュー</p><div id="app" style="padding:16px;max-width:1500px;margin:auto"></div>'
createApp(PlateRecognition).mount('#app')
