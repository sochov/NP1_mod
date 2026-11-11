#include "audio-setting-frame.h"
static unsigned char buffer[1024];
static int volume,mute,correction,result;
static unsigned long long starttime;
unsigned long ncc_audio_buffer(void){return (unsigned long)buffer;}
int ncc_audio_parse(unsigned size,unsigned command){volume=mute=correction=result=-1;return ncc_audio_frame(buffer,size,1234,0x778899aa,command,&volume,&mute,&correction,&result);}
int ncc_audio_volume(void){return volume;}
int ncc_audio_mute(void){return mute;}
int ncc_audio_correction(void){return correction;}
int ncc_audio_result(void){return result;}
int ncc_audio_starttime_parse(unsigned size){starttime=0;return ncc_audio_starttime(buffer,size,&starttime);}
unsigned long long ncc_audio_starttime_value(void){return starttime;}
