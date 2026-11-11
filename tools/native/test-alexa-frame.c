#include "alexa-frame.h"
static unsigned char buffer[300];
static int value,result;
unsigned long ncc_alexa_buffer(void){return (unsigned long)buffer;}
int ncc_alexa_parse(unsigned size,unsigned command){value=-1;result=-1;return ncc_alexa_frame(buffer,size,1234,0x778899aa,command,&value,&result);}
int ncc_alexa_value(void){return value;}
int ncc_alexa_accepted(void){return result;}
