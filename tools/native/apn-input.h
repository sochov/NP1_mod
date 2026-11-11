/* Private pipe format only. Never print this structure or place it in argv/env.
 * This parser does not load vendor code, open a device, or change any profile. */
#ifndef NCC_APN_INPUT_H
#define NCC_APN_INPUT_H
#define NCC_APN_INPUT_BYTES 316
struct ncc_apn_profile { unsigned char profile_index, auth; char apn[50], user[50], password[50]; };
struct ncc_apn_profiles { struct ncc_apn_profile profiles[2]; };
static int ncc_apn_field(const unsigned char *source,char *target,int required) {
 unsigned int end=50;
 for(unsigned int i=0;i<50;i++) {
  unsigned char c=source[i];
  if(!c&&end==50)end=i;
  if(end!=50) { if(c)return 0; }
  else if(c<32||c>126||c=='"'||c=='\\')return 0;
 }
 if(end==50||(required&&end==0))return 0;
 for(unsigned int i=0;i<50;i++)target[i]=(char)source[i];
 return 1;
}
static void ncc_apn_clear(struct ncc_apn_profiles *out) {
 volatile unsigned char *p=(volatile unsigned char*)out;
 for(unsigned long i=0;i<sizeof(*out);i++)p[i]=0;
}
static int ncc_apn_parse(const unsigned char *bytes,unsigned long length,struct ncc_apn_profiles *out) {
 if(!out)return 0;
 ncc_apn_clear(out);
 if(!bytes||length!=NCC_APN_INPUT_BYTES)return 0;
 const unsigned char header[8]={'N','C','C','A','P','N','1','\n'};
 for(unsigned int i=0;i<8;i++)if(bytes[i]!=header[i])return 0;
 for(unsigned int i=0;i<2;i++) {
  const unsigned char *record=bytes+8+154*i;
  struct ncc_apn_profile *profile=out->profiles+i;
  if(record[0]!=i+1||record[1]>3||record[2]!=0||record[3]!=0)goto reject;
  profile->profile_index=record[0];profile->auth=record[1];
  if(!ncc_apn_field(record+4,profile->apn,1)||!ncc_apn_field(record+54,profile->user,0)||!ncc_apn_field(record+104,profile->password,0))goto reject;
 }
 return 1;
reject:
 ncc_apn_clear(out);return 0;
}
#endif
