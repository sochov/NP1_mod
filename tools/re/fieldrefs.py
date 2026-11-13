"""List code regions that load given struct offsets close together (aarch64)."""
import sys
from elftools.elf.elffile import ELFFile
from capstone import Cs, CS_ARCH_ARM64, CS_MODE_ARM
from capstone.arm64 import ARM64_OP_MEM
path=sys.argv[1]; want=[int(x,16) for x in sys.argv[2].split(',')]; win=int(sys.argv[3]) if len(sys.argv)>3 else 30
elf=ELFFile(open(path,'rb')); t=elf.get_section_by_name('.text')
md=Cs(CS_ARCH_ARM64,CS_MODE_ARM); md.detail=True
ins=list(md.disasm(t.data(),t['sh_addr']))
def disp(i):
    for op in i.operands:
        if op.type==ARM64_OP_MEM: return op.mem.disp
pos={w:[] for w in want}
for k,i in enumerate(ins):
    if i.mnemonic.startswith(('ldr','ldp','ldur')):
        d=disp(i)
        if d in pos: pos[d].append(k)
first=pos[want[0]]
for k in first:
    if all(any(abs(k-j)<=win for j in pos[w]) for w in want[1:]):
        print('=== around',hex(ins[k].address))
        for i in ins[max(0,k-win):k+win*2]: print(f'{i.address:#x}: {i.mnemonic} {i.op_str}')
