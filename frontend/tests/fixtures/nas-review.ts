import {createApp} from 'vue'
import NASBackup from '../../src/components/NASBackup.vue'
import '../../src/style.css'
document.body.innerHTML='<div id="app" style="padding:16px;max-width:1300px;margin:auto"></div>'
createApp(NASBackup).mount('#app')
