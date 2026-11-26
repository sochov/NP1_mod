import {Map as GLMap,Marker,Popup,NavigationControl,AttributionControl,LngLatBounds,setWorkerUrl,setWorkerCount,type MapOptions,type LayerSpecification,type GeoJSONSource,type MapMouseEvent} from 'maplibre-gl'
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url'
import type {FeatureCollection,Feature,Geometry,Position} from 'geojson'
import {vectorStyle,mapAttribution,type MapPalette,type MapView} from './vector-style'
import 'maplibre-gl/dist/maplibre-gl.css'
import '../vector-map.css'
export {Marker,Popup,mapAttribution}
export type {FeatureCollection,Feature,Geometry,Position,MapMouseEvent}
export type LatLon=[number,number]
setWorkerUrl(workerUrl);setWorkerCount(2)
// Development-only inspection for rendering/interaction regression tests. No
// map objects or private coordinates are attached to window or production DOM.
const hotData=import.meta.hot?.data
const inspected:WeakMap<HTMLElement,GLMap>|undefined=import.meta.env.DEV?(hotData?(hotData.inspected??=new WeakMap<HTMLElement,GLMap>()):new WeakMap<HTMLElement,GLMap>()):undefined
export const inspectVectorMap=(element:HTMLElement)=>inspected?.get(element)
export const collection=(features:Feature[]=[]) :FeatureCollection=>({type:'FeatureCollection',features})
export const feature=(geometry:Geometry,properties:Record<string,unknown>={}):Feature=>({type:'Feature',geometry,properties})
export function circleGeometry(lat:number,lon:number,metres:number):Geometry{
 const coordinates:Position[]=[]
 for(let i=0;i<=48;i++){const a=i/48*Math.PI*2;coordinates.push([lon+Math.cos(a)*metres/(111320*Math.max(.01,Math.cos(lat*Math.PI/180))),lat+Math.sin(a)*metres/111320])}
 return {type:'Polygon',coordinates:[coordinates]}
}
export function routeFeatures(points:{lat:number;lon:number;segment:number;at?:string}[]):FeatureCollection{
 const groups=new Map<number,typeof points>()
 for(const point of points){const group=groups.get(point.segment)||[];group.push(point);groups.set(point.segment,group)}
 return collection([...groups].map(([segment,group])=>feature(group.length>1?{type:'LineString',coordinates:group.map(p=>[p.lon,p.lat])}:{type:'Point',coordinates:[group[0]!.lon,group[0]!.lat]},{segment,count:group.length,from:group[0]!.at,to:group.at(-1)!.at})))
}
export function routeLayers(id:string,color='#397fd7',width=4):LayerSpecification[]{return [
 {id:id+'-halo',type:'line',source:id,filter:['==','$type','LineString'],layout:{'line-cap':'round','line-join':'round'},paint:{'line-color':'#f5f8ff','line-width':width+2,'line-opacity':.7}},
 {id:id+'-line',type:'line',source:id,filter:['==','$type','LineString'],layout:{'line-cap':'round','line-join':'round'},paint:{'line-color':color,'line-width':width,'line-opacity':.9}},
 {id:id+'-points',type:'circle',source:id,filter:['==','$type','Point'],paint:{'circle-color':color,'circle-radius':3,'circle-stroke-color':'#fff','circle-stroke-width':1}}
]}
export function locationElement(className:string,live=true){const el=document.createElement('span');el.className='vector-location '+className;el.style.setProperty('--marker-color',live?'#397fd7':'#8a94a3');return el}
type Overlay={data:FeatureCollection;layers:LayerSpecification[]}
export class VectorMap{
 readonly map:GLMap
 private overlays=new Map<string,Overlay>()
 private ready=false
 private openedPopup:Popup|undefined
 private online:boolean
 private palette:MapPalette='mist'
 private view:MapView='dashboard'
 private mutation:MutationObserver
 private media=matchMedia('(prefers-color-scheme:dark)')
 private resize:ResizeObserver
 private resolveTheme():MapPalette{const saved=document.documentElement.getAttribute('data-map-'+this.view+'-theme');if(saved==='mist'||saved==='night'||saved==='sage'||saved==='colorful')return saved;return document.documentElement.dataset.theme==='dark'||((!document.documentElement.dataset.theme||document.documentElement.dataset.theme==='system')&&this.media.matches)?'night':'mist'}
 private theme=()=>{const next=this.resolveTheme();if(next!==this.palette){this.palette=next;this.resetStyle()}}
 constructor(options:Omit<MapOptions,'style'|'center'> & {view:MapView;center?:[number,number];background?:boolean;hero?:boolean;failed?:()=>void}){
  const {view,background=false,hero=false,failed,...rest}=options;this.online=background;this.view=view
  this.palette=this.resolveTheme()
  this.map=new GLMap({...rest,center:rest.center??[138.2,36.2],zoom:rest.zoom??5,minZoom:3,maxZoom:20,style:vectorStyle(this.palette,background),attributionControl:false,dragRotate:false,pitchWithRotate:false,renderWorldCopies:false,localIdeographFontFamily:'"Noto Sans JP", "Yu Gothic UI", sans-serif',canvasContextAttributes:{antialias:true},transformRequest:url=>({url,referrerPolicy:'strict-origin-when-cross-origin'})})
  const el=this.map.getContainer();inspected?.set(el,this.map);el.dataset.mapEngine='maplibre';el.dataset.mapBackground=background?'vector':'off';el.dataset.mapTheme=this.palette
  if(!hero){this.map.addControl(new NavigationControl({showCompass:false}),'top-left');this.map.addControl(new AttributionControl({compact:false}),'bottom-right')}
  else{this.map.scrollZoom.disable();this.map.scrollZoom.enable({around:'center'});this.map.touchZoomRotate.disable();this.map.touchZoomRotate.enable({around:'center'})}
  this.map.on('style.load',()=>{this.ready=true;this.apply();el.dataset.mapReady='true'})
  this.map.on('error',event=>{if(('sourceId'in event&&event.sourceId==='basemap')||event.error?.message?.includes('tiles.openfreemap.org')){failed?.();el.dataset.mapBackground='failed'}})
  this.map.on('move',()=>this.state());this.map.on('zoom',()=>this.state())
  this.resize=new ResizeObserver(()=>this.map.resize());this.resize.observe(el)
  this.mutation=new MutationObserver(this.theme);this.mutation.observe(document.documentElement,{attributes:true,attributeFilter:['data-theme','data-map-'+view+'-theme']});this.media.addEventListener('change',this.theme);this.state()
 }
 private state(){const el=this.map.getContainer(),center=this.map.getCenter();el.dataset.mapCenter=center.lng.toFixed(7)+','+center.lat.toFixed(7);el.dataset.mapZoom=this.map.getZoom().toFixed(3)}
 private resetStyle(){this.ready=false;this.map.getContainer().dataset.mapReady='false';const style=vectorStyle(this.palette,this.online);for(const [id,overlay]of this.overlays){style.sources[id]={type:'geojson',data:overlay.data,tolerance:0};style.layers.push(...overlay.layers)}this.map.setStyle(style,{diff:false});this.map.getContainer().dataset.mapBackground=this.online?'vector':'off';this.map.getContainer().dataset.mapTheme=this.palette}
 private apply(){for(const [id,overlay]of this.overlays){const source=this.map.getSource(id) as GeoJSONSource|undefined;if(source)source.setData(overlay.data);else{this.map.addSource(id,{type:'geojson',data:overlay.data,tolerance:0});for(const layer of overlay.layers)this.map.addLayer(layer)}}}
 setOverlay(id:string,data:FeatureCollection,layers:LayerSpecification[]){this.overlays.set(id,{data,layers});this.map.getContainer().dataset['features'+id.replace(/(^|-)(\w)/g,(_,a:string,b:string)=>b.toUpperCase())]=String(data.features.length);if(this.ready){const source=this.map.getSource(id) as GeoJSONSource|undefined;if(source)source.setData(data);else this.apply()}}
 background(value:boolean){if(value!==this.online){this.online=value;this.resetStyle()}}
 fit(points:LatLon[],maxZoom=17){if(!points.length)return;const bounds=new LngLatBounds();for(const [lat,lon]of points)bounds.extend([lon,lat]);this.map.fitBounds(bounds,{padding:28,maxZoom,duration:0})}
 centre(point:LatLon){this.map.jumpTo({center:[point[1],point[0]]})}
 popup(point:LatLon,node:HTMLElement){this.openedPopup?.remove();this.openedPopup=new Popup({maxWidth:'300px'}).setLngLat([point[1],point[0]]).setDOMContent(node).addTo(this.map)}
 closePopup(){this.openedPopup?.remove();this.openedPopup=undefined}
 features(event:MapMouseEvent,layers:string[]){const visible=layers.filter(id=>this.map.getLayer(id));return visible.length?this.map.queryRenderedFeatures(event.point,{layers:visible}):[]}
 destroy(){inspected?.delete(this.map.getContainer());this.mutation.disconnect();this.media.removeEventListener('change',this.theme);this.resize.disconnect();this.map.remove()}
}
