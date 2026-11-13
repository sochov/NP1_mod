"""Find aarch64 adrp+add/ldr references to a file-offset string and dump surrounding code."""
import sys
from elftools.elf.elffile import ELFFile
from capstone import Cs, CS_ARCH_ARM64, CS_MODE_ARM
from capstone.arm64 import ARM64_OP_IMM, ARM64_OP_REG, ARM64_OP_MEM

path = sys.argv[1]
targets = [int(x, 16) for x in sys.argv[2].split(',')]
before = int(sys.argv[3]) if len(sys.argv) > 3 else 40
after = int(sys.argv[4]) if len(sys.argv) > 4 else 80
f = open(path, 'rb')
elf = ELFFile(f)
data = open(path, 'rb').read()

def off2va(o):
    for seg in elf.iter_segments():
        if seg['p_type'] == 'PT_LOAD' and seg['p_offset'] <= o < seg['p_offset'] + seg['p_filesz']:
            return o - seg['p_offset'] + seg['p_vaddr']
tva = {off2va(t): t for t in targets}
text = elf.get_section_by_name('.text')
code, base = text.data(), text['sh_addr']
md = Cs(CS_ARCH_ARM64, CS_MODE_ARM)
md.detail = True
insns = list(md.disasm(code, base))
adrp = {}
hits = []
for i, ins in enumerate(insns):
    if ins.mnemonic == 'adrp':
        adrp[ins.operands[0].reg] = ins.operands[1].imm
    elif ins.mnemonic == 'add' and len(ins.operands) == 3 and ins.operands[2].type == ARM64_OP_IMM:
        r = ins.operands[1].reg
        if r in adrp:
            v = adrp[r] + ins.operands[2].imm
            if v in tva:
                hits.append((i, v))
for i, v in hits:
    print(f'=== ref to {v:#x} (file {tva[v]:#x}) at {insns[i].address:#x}')
    for ins in insns[max(0, i - before): i + after]:
        print(f'{ins.address:#x}: {ins.mnemonic} {ins.op_str}')
