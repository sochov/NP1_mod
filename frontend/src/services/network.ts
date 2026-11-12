// State labels shared by the detailed network cards and the compact dashboard tile.
type Loose=Record<string,any>|undefined
export const apLabel=(hotspot:Loose)=>hotspot?.enabled===true?'提供中':hotspot?.enabled===false?'停止中':'取得待ち'
export const wifiLabel=(wifi:Loose)=>wifi?.wpa_state==='COMPLETED'?'接続済み':['DISCONNECTED','INACTIVE','INTERFACE_DISABLED'].includes(wifi?.wpa_state)?'未接続':'取得待ち'
export const vpnLabel=(vpn:Loose)=>vpn?.online===true?'オンライン':vpn?.backendState==='NeedsLogin'?'認証待ち':vpn?.online===false?'オフライン':'取得待ち'
