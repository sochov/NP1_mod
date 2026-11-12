import L from 'leaflet'

export const mapAttribution='© <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener">OpenStreetMap contributors</a>'

export function mapTiles(failed?:()=>void){
 return L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png',{
  maxZoom:19,
  referrerPolicy:'strict-origin-when-cross-origin',
  attribution:mapAttribution
 }).on('tileerror',event=>{(event as L.TileErrorEvent).tile.style.visibility='hidden';failed?.()})
}
