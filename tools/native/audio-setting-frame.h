#ifndef NCC_AUDIO_SETTING_FRAME_H
#define NCC_AUDIO_SETTING_FRAME_H
/* Firmware-pinned Setting/CommonFreeStyle 0x400. No master-volume writes. */
static unsigned ncc_audio_u16(const unsigned char*p){return p[0]|((unsigned)p[1]<<8);}
static unsigned ncc_audio_u32(const unsigned char*p){return ncc_audio_u16(p)|((unsigned)ncc_audio_u16(p+2)<<16);}
static unsigned long long ncc_audio_u64(const unsigned char*p){return ncc_audio_u32(p)|((unsigned long long)ncc_audio_u32(p+4)<<32);}
static int ncc_audio_varint(const unsigned char*b,unsigned long end,unsigned long*position,unsigned*value){unsigned result=0;for(unsigned i=0;i<5;i++){if(*position>=end)return 0;unsigned byte=b[(*position)++];if(i==4&&(byte&0xf8))return 0;result|=(byte&127)<<(7*i);if(!(byte&128)){if(i&&!(byte&127))return 0;*value=result;return 1;}}return 0;}
static int ncc_audio_equal(const char*a,const char*b){if(!a||!b)return 0;while(*a&&*a==*b){a++;b++;}return *a==*b;}
int ncc_audio_number(const char*s,int maximum){return s&&s[0]>='0'&&s[0]<='0'+maximum&&!s[1]?s[0]-'0':-1;}
int ncc_audio_id(const char*id){return ncc_audio_equal(id,"mute")?1:ncc_audio_equal(id,"volume-correction")?22:0;}
int ncc_audio_validate(const char*id,const char*before,const char*next){int index=ncc_audio_id(id),maximum=index==1?1:2;return index&&ncc_audio_number(before,maximum)>=0&&ncc_audio_number(next,maximum)>=0?index:0;}
unsigned ncc_audio_persistent(int index){return index==1?466:index==22?468:0;}
int ncc_audio_wire(int index,int native){return index==1&&native>=0&&native<=1?(native==0?2:1):index==22&&native>=0&&native<=2?native+1:0;}
int ncc_audio_starttime(const unsigned char*b,unsigned long size,unsigned long long*out){if(!b||!out||size<4||size>1023)return 0;unsigned long position=0;int closed=0;for(unsigned long i=0;i<size;i++)if(b[i]==')'){position=i+1;closed=1;}if(!closed)return 0;for(unsigned field=3;field<=22;field++){while(position<size&&b[position]==' ')position++;unsigned long start=position;while(position<size&&b[position]!=' '&&b[position]!='\n')position++;if(start==position)return 0;if(field==22){unsigned long long value=0;if(position-start>20)return 0;for(unsigned long i=start;i<position;i++){unsigned digit=b[i]-'0';if(digit>9||value>(0xffffffffffffffffULL-digit)/10)return 0;value=value*10+digit;}if(!value)return 0;*out=value;return 1;}}return 0;}
int ncc_audio_frame(const unsigned char*b,unsigned long size,unsigned source,unsigned nonce,unsigned command,int*volume,int*mute,int*correction,int*result){
 if(!b||!volume||!mute||!correction||!result||size<54||size>256||ncc_audio_u64(b)!=0x80001f02ULL||ncc_audio_u32(b+8)!=source||(command!=0x8001&&command!=0x8003&&command!=0x8004))return 0;
 const char name[]="Setting";unsigned chars=ncc_audio_u32(b+14);if(chars!=sizeof(name))return 0;for(unsigned i=0;i<chars;i++)if(ncc_audio_u16(b+18+2*i)!=(unsigned char)name[i])return 0;
 unsigned long offset=18+2*chars;if(offset+20>size||ncc_audio_u32(b+offset)!=0x4e434356||ncc_audio_u32(b+offset+4)!=nonce||ncc_audio_u16(b+offset+8)!=0x400||ncc_audio_u16(b+offset+10)!=command)return 0;
 unsigned long long length=ncc_audio_u64(b+offset+12);offset+=20;if(length>64||length!=size-offset)return 0;
 unsigned seen=0;int v=0,m=0,c=0,r=0;
 while(offset<size){unsigned key,value;if(!ncc_audio_varint(b,size,&offset,&key)||(key&7)!=0||!ncc_audio_varint(b,size,&offset,&value))return 0;unsigned field=key>>3;if(field<1||field>3||(command!=0x8001&&field!=1)||(seen&(1u<<field)))return 0;seen|=1u<<field;
  if(command!=0x8001){if(value>1)return 0;r=(int)value;}
  else if(field==1)v=(int)value;
  else if(field==2){if(value<1||value>2)return 0;m=value==2?0:1;}
  else {if(value<1||value>3)return 0;c=(int)value-1;}
 }
 if(command==0x8001&&(seen&12)!=12)return 0;
 *volume=v;*mute=m;*correction=c;*result=r;return 1;
}
#endif
