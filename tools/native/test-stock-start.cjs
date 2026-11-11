const fs=require('fs'),assert=require('assert');
(async()=>{const{instance}=await WebAssembly.instantiate(fs.readFileSync(process.argv[2]));const e=instance.exports;const ptr=e.ncc_start_buffer();let checks=0;
function put(value){const b=typeof value==='string'?Buffer.from(value):value;new Uint8Array(e.memory.buffer,ptr,8192).fill(0);new Uint8Array(e.memory.buffer,ptr,b.length).set(b);return b.length;}
function check(name,input,expected,...args){let actual=e['ncc_start_'+name](put(input),...args);if(typeof actual==='bigint')actual=BigInt.asUintN(64,actual);assert.equal(actual,expected,name);checks++;}
check('number','18446744073709551615',18446744073709551615n,10);check('number','18446744073709551616',0n,10);check('number','12z',0n,10);
check('time','42 (complex ) name) S '+Array.from({length:18},()=>0).join(' ')+' 98765 0',98765n);check('time','broken stat',0n);
check('threads','Name:\tapp\nThreads:\t1\n',1);check('threads','Threads:\t12\n',12);check('threads','XThreads:\t12\n',0);check('threads','Threads:\t-1\n',0);
check('invocation','OTHER=1\0INVOCATION_ID=0123456789abcdef0123456789abcdef\0',1);check('invocation','INVOCATION_ID=0123456789abcdef0123456789abcdee\0',0);check('invocation','INVOCATION_ID=0123456789abcdef0123456789abcdef',0);check('invocation','INVOCATION_ID=0123456789abcdef0123456789abcdef\0INVOCATION_ID=0123456789abcdef0123456789abcdef\0',0);
put('0123456789abcdef0123456789abcdef');assert.equal(e.ncc_start_hex(),1);put('0123456789abcdef0123456789abcdeF');assert.equal(e.ncc_start_hex(),0);checks+=2;
const header=Buffer.alloc(128);header.writeUInt32LE(129,32);header.writeBigUInt64LE(3n,112);check('header',header,1);const bad=Buffer.from(header);bad[8]=1;check('header',bad,0);bad[8]=0;bad.writeUInt32LE(128,32);check('header',bad,0);check('header',Buffer.concat([header,Buffer.from([0])]),0);
const map='10000-11000 rw-s 00000000 00:01 123 /tmp/victoria/victoria_TEST.map\n';check('map',map,65536n);check('map',map.replace(' 123 ',' 124 '),0n);check('map',map.replace('rw-s','rw-p'),0n);check('map',map+map,0n);check('map',map.replace('00000000','00001000'),0n);
check('futex','98 0x1006c 0x9 0x0 0x0 0x0 0xffffffff 0x0 0x0\n',65644n);check('futex','running\n',0n);check('futex','99 0x1006c 0x9\n',0n);
check('waiting','98 0x1006c 0x0\n',1);check('waiting','98 0x10068 0x0\n',1);check('waiting','98 0x10064 0x0\n',0);check('waiting','98 0x10070 0x0\n',0);check('waiting','99 0x1006c 0x0\n',0);
console.log('PASS: stock start-barrier validation ('+checks+' assertions)');})().catch(error=>{console.error(error);process.exit(1)});
