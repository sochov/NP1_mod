/* Firmware allowlist, hashes only; vendor material is not redistributed. */
struct vendor_fingerprint{const char*path;const char*hash;};
static const struct vendor_fingerprint vendor_files[]={
 {"/usr/local/bin/Setting","d5b876fd8e5a3c18037b25812f5d989351045ecd024b40a99c551c4d00f7e0ac"},
 {"/usr/local/lib/libAplLib.so","83bcfe30838001b382e58771fd28eabf17e43e6e72568ee5ad80656d223eae00"},
 {"/usr/local/lib/libAIPI.so","a1c6cdb1bbaccd112c19836bf3088f0091d2d5a156423a629fad11e27a7689a4"},
 {"/usr/local/lib/libNStandardLib.so","aecb93e09b5dbb664171b814bc389aa453389caa7ff311e690a411187bff6f46"},
 {"/usr/local/lib/libNPCommonLib.so","c1d85c18df4f9fe6829fdb6ad0c13cd8df16cae36f2f0e5f5c444d2359956281"},
 {"/usr/local/lib/libNDeviceLibSys.so","c0dde7fa35974f249a4194eb26badceb9d1e50eaa0877018d30cbda369fab715"},
 {"/usr/local/lib/libNDeviceLibITS.so","1d8d1a569b06190188d06b3c6979cdd75626dbec3f1718f799dee31e49f499fb"},
 {"/usr/local/lib/libNEventBaseLib.so","a85381c4b74aad21e83e19cab9cfa5e2e7d5b6f5c5cc98593afab85e9c19efa1"},
 {"/usr/local/lib/libNSystemInfoLib.so","d3eb7abf42b3c9c7a87c63dcfee19b56e46713055e76af928dcb76421d4da226"},
 {"/usr/local/lib/libNStorageLib.so","7622b8a954843fae89049d3435f1a88a8f6103059b1b1d52b4249627dc6f74a6"},
 {"/usr/local/lib/libNPSystemMonitorLib.so","73badb54785ab6e03dc6b3ede9f23d861fbda3ce3421540db183c3635ceb0c5f"},
 {"/usr/local/lib/libputillib.so","c64385b4dda865e2097f7fb43ac247ce88d72870e13d2cb106dc85e34a965715"},
};
