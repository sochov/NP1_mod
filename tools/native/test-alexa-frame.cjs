const fs=require('node:fs');const assert=require('node:assert/strict');
(async()=>{
 const {instance:{exports:api}}=await WebAssembly.instantiate(fs.readFileSync(process.argv[2]),{});const memory=new Uint8Array(api.memory.buffer);const address=api.ncc_alexa_buffer();
 function packet(command=0x8004,payload=[]){const value=Buffer.alloc(50+payload.length);value.writeBigUInt64LE(0x80001f02n);value.writeUInt32LE(1234,8);value.writeUInt32LE(6,14);value.write('Voice\0',18,'utf16le');value.writeUInt32LE(0x4e434341,30);value.writeUInt32LE(0x778899aa,34);value.writeUInt16LE(0xb00,38);value.writeUInt16LE(command,40);value.writeBigUInt64LE(BigInt(payload.length),42);value.set(payload,50);return value;}
 function parse(value,command=0x8004){memory.fill(0,address,address+300);memory.set(value,address);return api.ncc_alexa_parse(value.length,command)}
 for(const [payload,want] of [[[],0],[[8,0],0],[[8,1],1],[[8,2],2]]){assert.equal(parse(packet(0x8004,payload)),1);assert.equal(api.ncc_alexa_value(),want)}
 assert.equal(parse(packet(0x8003,[8,1,16,1]),0x8003),1);assert.equal(api.ncc_alexa_accepted(),1);assert.equal(api.ncc_alexa_value(),1);
 assert.equal(parse(packet(0x8003,[]),0x8003),1);assert.equal(api.ncc_alexa_accepted(),0);
 for(const offset of [0,8,14,28,30,34,38,40,42]){const invalid=packet();invalid[offset]^=0xff;assert.equal(parse(invalid),0)}
 for(const payload of [[8,3],[8,1,8,1],[16,1],[8],[8,128,0],Array(34).fill(0)])assert.equal(parse(packet(0x8004,payload)),0);
 for(const invalid of [Buffer.alloc(0),packet().subarray(0,49),Buffer.concat([packet(),Buffer.from([0])])])assert.equal(parse(invalid),0);
 console.log('PASS: compiled C Alexa correlation, bounded AIPI lengths, ON/OFF/FIXED, refusal/default distinction, malformed rejection');
})().catch(error=>{console.error(error);process.exitCode=1});
