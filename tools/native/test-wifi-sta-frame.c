#include "wifi-sta-frame.h"
static unsigned char buffer[600];
static int state,result,error;
static unsigned long long starttime;
unsigned long ncc_sta_buffer(void){return (unsigned long)buffer;}
int ncc_sta_parse(unsigned size,unsigned command){state=result=error=-1;return ncc_wifi_sta_frame(buffer,size,1234,0x778899aa,command,&state,&result,&error);}
int ncc_sta_state(void){return state;}
int ncc_sta_result(void){return result;}
int ncc_sta_error(void){return error;}
int ncc_sta_success(void){return ncc_wifi_sta_success(state,result,error);}
int ncc_sta_starttime_parse(unsigned size){starttime=0;return ncc_wifi_sta_starttime(buffer,size,&starttime);}
unsigned long long ncc_sta_starttime_value(void){return starttime;}
