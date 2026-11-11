export type JourneyDay={date:string;distanceKm:number|null;drivingSeconds:number|null;observed:boolean;sampleCount:number;observedSeconds?:number}
export type JourneyPeriod={distanceKm:number|null;drivingSeconds:number|null;hasData:boolean;validObservedDays:number;sampleCount:number;days:JourneyDay[]}
export type JourneyTrip={id:string;start:string;end:string;distanceKm:number;drivingSeconds:number;observedSeconds:number;sampleCount:number}
export type JourneyHistory={range:{period:string;from:string;to:string;sourceFrom?:string;sourceTo?:string};days:JourneyDay[];period:JourneyPeriod;trips:JourneyTrip[];totalTrips:number;detailsTruncated:boolean;sourceTruncated:boolean;sourceFiles:number;unreadableFiles:number;noFixSentences:number;badChecksums:number;indexedAt:string;source:string;status:string;reason?:string}
export type JourneyBucket={key:string;from:string;to:string;distance:number|null;hours:number|null;observedDays:number;dayCount:number;distanceDays:number;timeDays:number;cumulativeDistance:number|null;cumulativeHours:number|null}
export type JourneyGrouping='day'|'week'|'month'
export const finite=(value:unknown):value is number=>typeof value==='number'&&Number.isFinite(value)&&value>=0
export const jstDate=()=>new Intl.DateTimeFormat('sv-SE',{timeZone:'Asia/Tokyo'}).format(new Date())
export const shortDate=(value:string)=>Number(value.slice(5,7))+'/'+Number(value.slice(8,10))
export function duration(seconds:unknown){if(!finite(seconds))return '—';const minutes=Math.floor(seconds/60);return seconds<60?Math.floor(seconds)+'秒':Math.floor(minutes/60)+'時間 '+minutes%60+'分'}
export function valueText(value:unknown,digits=1){return finite(value)?value.toLocaleString('ja-JP',{maximumFractionDigits:digits}):'—'}
export function calendar(from:string,to:string,entries:JourneyDay[]):JourneyDay[]{
 const start=Date.parse(from+'T00:00:00Z'),finish=Date.parse(to+'T00:00:00Z')
 if(!Number.isFinite(start)||!Number.isFinite(finish)||start>finish||(finish-start)/86400000>=36600)return []
 const known=new Map(entries.map(day=>[day.date,day]))
 const result:JourneyDay[]=[]
 for(let at=start;at<=finish;at+=86400000){const date=new Date(at).toISOString().slice(0,10);result.push(known.get(date)??{date,distanceKm:null,drivingSeconds:null,observed:false,sampleCount:0})}
 return result
}
export function groupJourney(days:JourneyDay[],grouping:JourneyGrouping):JourneyBucket[]{
 const groups=new Map<string,JourneyBucket>()
 for(const day of days){
  const date=new Date(day.date+'T00:00:00Z');date.setUTCDate(date.getUTCDate()-(date.getUTCDay()+6)%7)
  const key=grouping==='month'?day.date.slice(0,7):grouping==='week'?date.toISOString().slice(0,10):day.date
  let bucket=groups.get(key)
  if(!bucket){bucket={key,from:day.date,to:day.date,distance:null,hours:null,observedDays:0,dayCount:0,distanceDays:0,timeDays:0,cumulativeDistance:null,cumulativeHours:null};groups.set(key,bucket)}
  bucket.to=day.date;bucket.dayCount++
  const distance=day.observed!==false&&finite(day.distanceKm),time=day.observed!==false&&finite(day.drivingSeconds)
  if(distance){bucket.distance=(bucket.distance??0)+day.distanceKm!;bucket.distanceDays++}
  if(time){bucket.hours=(bucket.hours??0)+day.drivingSeconds!/3600;bucket.timeDays++}
  if(distance||time)bucket.observedDays++
 }
 let distance=0,hours=0
 return [...groups.values()].map(bucket=>{if(bucket.distance!==null){distance+=bucket.distance;bucket.cumulativeDistance=distance}if(bucket.hours!==null){hours+=bucket.hours;bucket.cumulativeHours=hours}return bucket})
}
export function niceMaximum(values:(number|null)[]){const value=Math.max(0,...values.map(value=>value??0));if(value===0)return 1;const step=10**Math.floor(Math.log10(value));return Math.ceil(value/step)*step}
