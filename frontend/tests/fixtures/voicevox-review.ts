// Isolated browser fixture; excluded from the production entry point.
import { createApp } from 'vue'
import VoicevoxSettings from '../../src/components/VoicevoxSettings.vue'
import '../../src/style.css'

document.title='Synthetic VOICEVOX settings review'
document.body.innerHTML='<div id="app" style="padding:16px;max-width:900px;margin:auto"></div>'
createApp(VoicevoxSettings).mount('#app')
