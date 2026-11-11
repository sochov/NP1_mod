#ifndef NCC_ALEXA_FRAME_H
#define NCC_ALEXA_FRAME_H
/* AIPI_Event (14 bytes), bounded NString, CommonFreeStyle correlation and
 * protobuf response. Never treat a timeout or an unrelated empty packet as ON. */
static unsigned ncc_a16(const unsigned char*p){return p[0]|((unsigned)p[1]<<8);}
static unsigned ncc_a32(const unsigned char*p){return ncc_a16(p)|((unsigned)ncc_a16(p+2)<<16);}
static unsigned long long ncc_a64(const unsigned char*p){return ncc_a32(p)|((unsigned long long)ncc_a32(p+4)<<32);}
int ncc_alexa_frame(const unsigned char*b,unsigned long size,unsigned source,unsigned nonce,unsigned command,int*state,int*accepted){
 if(!b||!state||!accepted||size<40||size>256||ncc_a64(b)!=0x80001f02ULL||ncc_a32(b+8)!=source)return 0;
 unsigned chars=ncc_a32(b+14);if(chars<1||chars>65)return 0;
 unsigned long offset=18+2*chars;if(offset+20>size||ncc_a16(b+offset-2)!=0)return 0;
 if(ncc_a32(b+offset)!=0x4e434341||ncc_a32(b+offset+4)!=nonce||ncc_a16(b+offset+8)!=0xb00||ncc_a16(b+offset+10)!=command)return 0;
 unsigned long long length=ncc_a64(b+offset+12);offset+=20;if(length>32||offset+length!=size)return 0;
 int value=0,result=0;unsigned seen=0;
 while(offset<size){unsigned key=b[offset++];if((key!=8&&key!=16)||offset==size)return 0;unsigned field=key>>3;if(seen&(1u<<field))return 0;seen|=1u<<field;unsigned v=b[offset++];if(v>2)return 0;
  if(command==0x8004){if(field!=1)return 0;value=(int)v;}
  else if(command==0x8003){if(field==1){if(v>1)return 0;result=(int)v;}else value=(int)v;}
  else return 0;
 }
 if(command!=0x8004&&command!=0x8003)return 0;*state=value;*accepted=result;return 1;
}
#endif
