const fs=require('node:fs');const assert=require('node:assert/strict');
(async()=>{const{instance:{exports:api}}=await WebAssembly.instantiate(fs.readFileSync(process.argv[2]),{});const memory=new Uint8Array(api.memory.buffer);const address=api.ncc_sta_buffer();
function packet(command=0x8001,payload=[8,1,16,1,24,1]){const b=Buffer.alloc(66+payload.length);b.writeBigUInt64LE(0x80001f02n);b.writeUInt32LE(1234,8);b.writeUInt32LE(14,14);b.write('Communication\0',18,'utf16le');b.writeUInt32LE(0x4e434357,46);b.writeUInt32LE(0x778899aa,50);b.writeUInt16LE(0x500,54);b.writeUInt16LE(command,56);b.writeBigUInt64LE(BigInt(payload.length),58);b.set(payload,66);return b;}
function parse(b,command=0x8001){memory.fill(0,address,address+600);memory.set(b,address);return api.ncc_sta_parse(b.length,command)}
for(const command of [0x8001,0x8002])for(const state of [1,2]){assert.equal(parse(packet(command,[8,1,16,1,24,state]),command),1);assert.equal(api.ncc_sta_state(),state);assert.equal(api.ncc_sta_success(),1)}
for(const payload of [[],[8,0,16,0,24,0],[8,2,16,3,24,2],[8,1,24,1]]){assert.equal(parse(packet(0x8001,payload)),1);assert.equal(api.ncc_sta_success(),0)}
const metadata=[8,1,16,1,24,1,34,3,65,66,67,42,1,68,48,10];assert.equal(parse(packet(0x8001,metadata)),1);assert.equal(api.ncc_sta_success(),1);assert.equal(parse(packet(0x8002,metadata),0x8002),0);
assert.equal(parse(packet(0x8001,[8,1,16,1,24,2,48,...Array(9).fill(255),1])),1);assert.equal(api.ncc_sta_success(),1);
for(const offset of [0,8,14,18,44,46,50,54,56,58]){const b=packet();b[offset]^=0xff;assert.equal(parse(b),0)}
for(const payload of [[8,3],[16,7],[24,3],[8,1,8,1],[24],[8,129,0],[34,129,1],[34,3,65],[13,1,0,0,0],[56,1],[48,128,128,128,128,8],[48,...Array(10).fill(255),1],Array(386).fill(0)])assert.equal(parse(packet(0x8001,payload)),0);
for(const b of [Buffer.alloc(0),packet().subarray(0,65),Buffer.concat([packet(),Buffer.from([0])])])assert.equal(parse(b),0);
assert.equal(parse(packet(),0x8004),0);
function stat(value,comm='Communication'){return Buffer.from(`1234 (${comm}) `+['S',...Array(18).fill('0'),value].join(' ')+'\n')}
for(const comm of ['Communication','a space ) name']){const b=stat('123456',comm);memory.set(b,address);assert.equal(api.ncc_sta_starttime_parse(b.length),1);assert.equal(api.ncc_sta_starttime_value(),123456n)}
for(const b of [stat('0'),stat('-1'),stat('18446744073709551616'),Buffer.from('1234 (Communication) S 1'),Buffer.from('invalid')]){memory.set(b,address);assert.equal(api.ncc_sta_starttime_parse(b.length),0)}
console.log('PASS: compiled C fixed STA correlation, strict success, signed-int32 metadata skip, bounded process-starttime, no default-ON, malformed rejection');})().catch(e=>{console.error(e);process.exitCode=1});
