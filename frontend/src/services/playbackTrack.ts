export type TrackPoint={at:string;lat:number;lon:number;segment:number;speedMps?:number;heading?:number}
export type TrackSegment={id:number;start:string;end:string;pointCount:number;distanceKm:number|null;drivingSeconds:number|null}
export type PlaybackTrack={date:string;points:TrackPoint[];segments:TrackSegment[];source:string;sourceTruncated:boolean;indexedAt:string;reason?:string}
export type RecordedRange={start:number;end:number}
export function hasRecording(ranges:RecordedRange[],at:number){return ranges.some(range=>at>=range.start&&at<range.end)}
export function pointAt(points:TrackPoint[],at:number){
 let left=0,right=points.length
 while(left<right){const middle=(left+right)>>>1;if(Date.parse(points[middle].at)<at)left=middle+1;else right=middle}
 const candidates=[points[left-1],points[left]].filter((point):point is TrackPoint=>!!point)
 const point=candidates.sort((a,b)=>Math.abs(Date.parse(a.at)-at)-Math.abs(Date.parse(b.at)-at))[0]
 return point&&Math.abs(Date.parse(point.at)-at)<=2500?point:undefined
}
export function nearestPoint(points:TrackPoint[],lat:number,lon:number,at:number,segment:number){
 let best:TrackPoint|undefined,distance=Infinity
 const scale=Math.cos(lat*Math.PI/180)
 for(const point of points){if(point.segment!==segment)continue;const next=(point.lat-lat)**2+((point.lon-lon)*scale)**2
  if(next<distance-1e-14||Math.abs(next-distance)<1e-14&&best&&Math.abs(Date.parse(point.at)-at)<Math.abs(Date.parse(best.at)-at)){best=point;distance=next}
 }
 return best
}
