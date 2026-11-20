// Generates the Android app's basemap styles from the Web UI's style definition so that both
// clients draw the same OpenFreeMap vector map. Run after changing frontend/src/services/vector-style.ts:
//   node tools/android-map-styles.mjs
import {mkdirSync, writeFileSync} from 'node:fs'
import {dirname, join} from 'node:path'
import {fileURLToPath} from 'node:url'
import {vectorStyle} from '../frontend/src/services/vector-style.ts'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const out = join(root, 'android/app/src/main/assets/map')
mkdirSync(out, {recursive: true})
for (const theme of ['mist', 'night', 'sage', 'colorful']) {
  const style = vectorStyle(theme, true)
  // MapLibre Native shows attribution through its own control; keep the plain-text credit.
  for (const source of Object.values(style.sources)) {
    if (source.attribution) source.attribution = '© OpenStreetMap · OpenFreeMap · OpenMapTiles'
  }
  writeFileSync(join(out, `${theme}.json`), JSON.stringify(style))
  console.log('wrote', `map/${theme}.json`)
}
