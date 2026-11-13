import {api,type Observation} from './api'

type TrackPoint={at:string;lat:number;lon:number;segment:number}
type Track={date:string;points:TrackPoint[]}
const jstDate=(ms:number)=>new Intl.DateTimeFormat('sv-SE',{timeZone:'Asia/Tokyo',year:'numeric',month:'2-digit',day:'2-digit'}).format(new Date(ms))

// recentTrail returns the recorded driving track of the last `hours`, split into line segments
// wherever the recorder's segment changes or two samples are more than two minutes apart.
export async function recentTrail(hours=24,now=Date.now()):Promise<[number,number][][]>{
 const since=now-hours*3600000
 const dates=[...new Set([jstDate(since),jstDate(now)])]
 const points:TrackPoint[]=[]
 for(const date of dates){
  try{const value=await api<Observation<Track>>('/recordings/track?date='+date);if(value.status==='available'&&Array.isArray(value.data?.points))points.push(...value.data.points)}catch{/* a missing day leaves a gap */}
 }
 const sorted=points.filter(p=>Number.isFinite(p.lat)&&Number.isFinite(p.lon)&&Date.parse(p.at)>=since&&Date.parse(p.at)<=now+60000).sort((a,b)=>Date.parse(a.at)-Date.parse(b.at))
 const segments:[number,number][][]=[]
 let last:TrackPoint|undefined
 for(const p of sorted){
  if(!last||p.segment!==last.segment||Date.parse(p.at)-Date.parse(last.at)>120000)segments.push([])
  segments[segments.length-1]!.push([p.lat,p.lon])
  last=p
 }
 return segments.filter(s=>s.length)
}
