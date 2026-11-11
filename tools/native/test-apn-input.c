#include "apn-input.h"
static unsigned char input[NCC_APN_INPUT_BYTES+1];
static struct ncc_apn_profiles output;
unsigned long ncc_apn_buffer(void){return (unsigned long)input;}
int ncc_apn_test_parse(unsigned long size){return ncc_apn_parse(input,size,&output);}
int ncc_apn_output_zero(void){unsigned char *p=(unsigned char*)&output;for(unsigned long i=0;i<sizeof(output);i++)if(p[i])return 0;return 1;}
int ncc_apn_test_auth(unsigned index){return index<2?output.profiles[index].auth:-1;}
