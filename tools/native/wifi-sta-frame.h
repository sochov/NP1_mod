#ifndef NCC_WIFI_STA_FRAME_H
#define NCC_WIFI_STA_FRAME_H
/* Fixed Communication owner response, never exposes MAC/IP strings. */
static unsigned ncc_a16(const unsigned char*p){return p[0]|((unsigned)p[1]<<8);}
static unsigned ncc_a32(const unsigned char*p){return ncc_a16(p)|((unsigned)ncc_a16(p+2)<<16);}
static unsigned long long ncc_a64(const unsigned char*p){return ncc_a32(p)|((unsigned long long)ncc_a32(p+4)<<32);}
static int ncc_sta_varint(const unsigned char*b,unsigned long end,unsigned long*offset,unsigned*value){unsigned result=0;for(unsigned i=0;i<5;i++){if(*offset>=end)return 0;unsigned byte=b[(*offset)++];if(i==4&&(byte&0xf8))return 0;result|=(byte&127)<<(i*7);if(!(byte&128)){if(i&&!(byte&127))return 0;*value=result;return 1;}}return 0;}
static int ncc_sta_skip_int32(const unsigned char*b,unsigned long end,unsigned long*offset){unsigned long long value=0;for(unsigned i=0;i<10;i++){if(*offset>=end)return 0;unsigned byte=b[(*offset)++];if(i==9&&byte>1)return 0;value|=(unsigned long long)(byte&127)<<(7*i);if(!(byte&128)){if(i&&!(byte&127))return 0;return value<=0x7fffffffULL||value>=0xffffffff80000000ULL;}}return 0;}
/* Parse only /proc/PID/stat field22. The comm may contain spaces or ')'. */
int ncc_wifi_sta_starttime(const unsigned char*b,unsigned long size,unsigned long long*out){if(!b||!out||size<4||size>1023)return 0;unsigned long position=0;int closed=0;for(unsigned long i=0;i<size;i++)if(b[i]==')'){position=i+1;closed=1;}if(!closed)return 0;for(unsigned field=3;field<=22;field++){while(position<size&&b[position]==' ')position++;unsigned long start=position;while(position<size&&b[position]!=' '&&b[position]!='\n')position++;if(start==position)return 0;if(field==22){unsigned long long value=0;if(position-start>20)return 0;for(unsigned long i=start;i<position;i++){unsigned digit=b[i]-'0';if(digit>9||value>(0xffffffffffffffffULL-digit)/10)return 0;value=value*10+digit;}if(!value)return 0;*out=value;return 1;}}return 0;}
int ncc_wifi_sta_frame(const unsigned char*b,unsigned long size,unsigned source,unsigned nonce,unsigned command,int*state,int*result,int*error){
 if(!b||!state||!result||!error||size<66||size>512||ncc_a64(b)!=0x80001f02ULL||ncc_a32(b+8)!=source||(command!=0x8001&&command!=0x8002))return 0;
 const char name[]="Communication";unsigned chars=ncc_a32(b+14);if(chars!=sizeof(name))return 0;for(unsigned i=0;i<chars;i++)if(ncc_a16(b+18+2*i)!=(unsigned char)name[i])return 0;
 unsigned long offset=18+2*chars;if(offset+20>size||ncc_a32(b+offset)!=0x4e434357||ncc_a32(b+offset+4)!=nonce||ncc_a16(b+offset+8)!=0x500||ncc_a16(b+offset+10)!=command)return 0;
 unsigned long long length=ncc_a64(b+offset+12);offset+=20;if(length>384||length!=size-offset)return 0;
 unsigned seen=0;int setting=0,status=0,reason=0;
 while(offset<size){unsigned key;if(!ncc_sta_varint(b,size,&offset,&key))return 0;unsigned field=key>>3,wire=key&7;if(field<1||field>6||(command==0x8002&&field>3)||(seen&(1u<<field)))return 0;seen|=1u<<field;
  if(field==4||field==5){unsigned n;if(wire!=2||!ncc_sta_varint(b,size,&offset,&n)||n>128||n>size-offset)return 0;offset+=n;}
  else if(field==6){if(wire!=0||!ncc_sta_skip_int32(b,size,&offset))return 0;}
  else {unsigned value;if(wire!=0||!ncc_sta_varint(b,size,&offset,&value))return 0;if(field==1){if(value>2)return 0;status=(int)value;}else if(field==2){if(value>6)return 0;reason=(int)value;}else if(field==3){if(value>2)return 0;setting=(int)value;}}
 }
 *state=setting;*result=status;*error=reason;return 1;
}
int ncc_wifi_sta_success(int state,int result,int error){return (state==1||state==2)&&result==1&&error==1;}
#endif
