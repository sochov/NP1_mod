import {test,expect} from '@playwright/test'
import {readFileSync} from 'node:fs'
import {runInNewContext} from 'node:vm'
const source=readFileSync(new URL('../src/workers/screen-audio-worklet.js',import.meta.url),'utf8')
function processor(rate:number){let Constructor:any;const chunks:ArrayBuffer[]=[];class Base{port={postMessage:(buffer:ArrayBuffer)=>chunks.push(buffer)}}
 runInNewContext(source,{AudioWorkletProcessor:Base,sampleRate:rate,ArrayBuffer,DataView,registerProcessor:(name:string,value:any)=>{expect(name).toBe('screen-audio-pcm');Constructor=value}})
 return {node:new Constructor(),chunks}
}
function push(node:any,channels:Float32Array[],block=128){for(let offset=0;offset<channels[0]!.length;offset+=block){const outputs=[[new Float32Array(Math.min(block,channels[0]!.length-offset)).fill(1)]];expect(node.process([channels.map(c=>c.subarray(offset,offset+block))],outputs)).toBe(true);expect(outputs[0]![0]!.every(v=>v===0)).toBe(true)}}
test('worklet downmixes stereo, resamples 48k to 24k and writes exact little-endian 200ms chunks',()=>{
 const {node,chunks}=processor(48000),left=new Float32Array(9600).fill(1),right=new Float32Array(9600).fill(0)
 push(node,[left,right]);expect(chunks).toHaveLength(1);expect(chunks[0]!.byteLength).toBe(9600);const bytes=new Uint8Array(chunks[0]!);expect(Array.from(bytes.slice(0,4))).toEqual([0,64,0,64])
})
test('fractional 44.1k resampling is continuous across process blocks',()=>{
 const input=Float32Array.from({length:17640},(_,i)=>Math.sin(i*.05)*.7),split=processor(44100),whole=processor(44100)
 push(split.node,[input],128);push(whole.node,[input],input.length)
 expect(split.chunks).toHaveLength(2);expect(split.chunks.map(b=>Buffer.from(b))).toEqual(whole.chunks.map(b=>Buffer.from(b)))
})
test('worklet clamps unsafe amplitudes and keeps opposite stereo channels silent',()=>{
 const clipped=processor(24000),input=Float32Array.from({length:4800},(_,i)=>i%3===0?-2:i%3===1?2:NaN);push(clipped.node,[input]);const view=new DataView(clipped.chunks[0]!)
 expect([view.getInt16(0,true),view.getInt16(2,true),view.getInt16(4,true)]).toEqual([-32768,32767,0])
 const silent=processor(24000);push(silent.node,[new Float32Array(4800).fill(.5),new Float32Array(4800).fill(-.5)]);expect(new Uint8Array(silent.chunks[0]!).every(v=>v===0)).toBe(true)
})
