#include "stock-start-core.h"
static u8 input[8192];
unsigned long ncc_start_buffer(void){return (unsigned long)input;}
unsigned long long ncc_start_number(unsigned n,unsigned base){return start_number(input,n,base);}
unsigned long long ncc_start_time(unsigned n){return start_time(input,n);}
unsigned ncc_start_threads(unsigned n){return start_threads(input,n);}
int ncc_start_header(unsigned n){return start_header(input,n);}
int ncc_start_invocation(unsigned n){return start_invocation(input,n,"0123456789abcdef0123456789abcdef");}
int ncc_start_hex(void){return start_hex_id((const char*)input);}
unsigned long long ncc_start_map(unsigned n){return start_map(input,n,"/tmp/victoria/victoria_TEST.map",123);}
unsigned long long ncc_start_futex(unsigned n){return start_futex(input,n);}
int ncc_start_waiting(unsigned n){return start_wait_address(65536,start_futex(input,n));}
void *memcpy(void*d,const void*s,size_t n){u8*a=d;const u8*b=s;while(n--)*a++=*b++;return d;}
void *memset(void*d,int c,size_t n){u8*a=d;while(n--)*a++=(u8)c;return d;}
