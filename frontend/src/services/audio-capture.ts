import {api} from './api'
import workletURL from '../workers/screen-audio-worklet.js?url&no-inline'
export type ScreenAudioHandle={stop:()=>Promise<void>}
type AudioSession={id:string;sampleRate:number;channels:number;chunkSamples:number;idleTimeoutMs:number}
export const screenAudioSupported=()=>typeof AudioContext!=='undefined'&&'audioWorklet' in AudioContext.prototype
/** Call synchronously from the start button, before awaiting the native display chooser. */
export function prepareScreenAudio():AudioContext{
 const context=new AudioContext({sampleRate:24000,latencyHint:'interactive'});void context.resume().catch(()=>{});return context
}
async function deleteAudio(id:string){const controller=new AbortController(),timeout=setTimeout(()=>controller.abort(),1200);try{await api(`/device/audio/sessions/${encodeURIComponent(id)}`,'DELETE',undefined,true,controller.signal,true)}catch{/* The server's audio watchdog independently stops stale playback. */}finally{clearTimeout(timeout)}}
export async function startScreenAudio(stream:MediaStream,context:AudioContext,onFailure:(reason:string)=>void):Promise<ScreenAudioHandle>{
 let session:AudioSession|undefined,node:AudioWorkletNode|undefined,source:MediaStreamAudioSourceNode|undefined,stopped=false,pending:ArrayBuffer|undefined,sending=false,sequence=0,request:AbortController|undefined,nextSendAt=0,sendTimer:ReturnType<typeof setTimeout>|undefined
 const tracks=stream.getAudioTracks(),stop=async()=>{if(stopped)return;stopped=true;pending=undefined;clearTimeout(sendTimer);request?.abort();if(node){node.port.onmessage=null;node.port.close();node.disconnect()};source?.disconnect();tracks.forEach(track=>track.stop());await context.close().catch(()=>{});if(session)await deleteAudio(session.id)}
 async function send(){
  if(stopped||sending||!pending||!session)return
  const delay=nextSendAt-performance.now();if(delay>0){if(!sendTimer)sendTimer=setTimeout(()=>{sendTimer=undefined;void send()},delay);return}
  const data=pending;pending=undefined;sending=true;const current=++sequence;const controller=new AbortController();request=controller;const timeout=setTimeout(()=>controller.abort(),1000)
  try{const pcm=btoa(String.fromCharCode(...new Uint8Array(data))),result=await api<{accepted:boolean;sequence:number}>(`/device/audio/sessions/${encodeURIComponent(session.id)}`,'PUT',{sequence:current,pcm},true,controller.signal);if(!stopped&&(!result.accepted||result.sequence!==current))throw new Error('音声の更新を確認できません')}
  catch(e){if(!stopped){const reason=e instanceof Error?e.message:String(e);void stop();onFailure('音声の接続が途切れたため、NP1での再生を停止しました。'+reason)}}
  finally{clearTimeout(timeout);if(!stopped){sending=false;request=undefined;nextSendAt=performance.now()+50;if(pending)void send()}}
 }
 try{
  if(!tracks.length)throw new Error('共有する音声がありません')
  await context.audioWorklet.addModule(workletURL)
  if(context.state!=='running')await context.resume()
  const controller=new AbortController(),timeout=setTimeout(()=>controller.abort(),5000)
  try{session=await api<AudioSession>('/device/audio/sessions','POST',undefined,true,controller.signal)}finally{clearTimeout(timeout)}
  if(!session?.id||session.sampleRate!==24000||session.channels!==1||session.chunkSamples!==4800||session.idleTimeoutMs<1000)throw new Error('NP1の音声出力を確認できません')
  node=new AudioWorkletNode(context,'screen-audio-pcm',{numberOfInputs:1,numberOfOutputs:1,outputChannelCount:[1]})
  node.port.onmessage=(event:MessageEvent<ArrayBuffer>)=>{if(stopped)return;if(!(event.data instanceof ArrayBuffer)||event.data.byteLength!==9600){void stop();onFailure('音声データを取得できないため、NP1での再生を停止しました。');return};pending=event.data;void send()}
  node.onprocessorerror=()=>{if(!stopped){void stop();onFailure('音声の変換を継続できないため、NP1での再生を停止しました。')}}
  source=context.createMediaStreamSource(new MediaStream(tracks));source.connect(node);node.connect(context.destination)
  tracks.forEach(track=>track.addEventListener('ended',()=>{if(!stopped){void stop();onFailure('共有音声が終了したため、NP1での再生を停止しました。')}},{once:true}))
  return {stop}
 }catch(e){await stop();throw e}
}
