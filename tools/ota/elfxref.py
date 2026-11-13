"""Read-only static helper for an owner-copied ARM64 ELF: map strings to the code that references them.

Usage: python -I tools/ota/elfxref.py ELF PATTERN [PATTERN...]
Prints, for each matching string, the virtual address and every ADRP+ADD/LDR pair that forms it,
with the enclosing symbol when the dynamic symbol table names it. Nothing is executed or modified.
"""
import re, struct, sys
from capstone import Cs, CS_ARCH_ARM64, CS_MODE_ARM

def sections(data):
    shoff, = struct.unpack_from('<Q', data, 0x28); shentsize, shnum, shstrndx = struct.unpack_from('<HHH', data, 0x3a)
    raw = [struct.unpack_from('<IIQQQQIIQQ', data, shoff + i * shentsize) for i in range(shnum)]
    names = raw[shstrndx]
    def name(off):
        start = names[4] + off; return data[start:data.index(b'\0', start)].decode()
    return {name(s[0]): s for s in raw}

def symbols(data, secs):
    out = []
    for symtab, strtab in (('.dynsym', '.dynstr'), ('.symtab', '.strtab')):
        if symtab not in secs or strtab not in secs: continue
        st, sr = secs[symtab], secs[strtab]
        for i in range(st[5] // 24):
            n, info, other, shndx, value, size = struct.unpack_from('<IBBHQQ', data, st[4] + i * 24)
            if value and size and info & 0xf == 2:
                start = sr[4] + n; out.append((value, size, data[start:data.index(b'\0', start)].decode()))
    return sorted(out)

def main():
    path, patterns = sys.argv[1], sys.argv[2:]
    data = open(path, 'rb').read(); secs = sections(data); syms = symbols(data, secs)
    ro = secs['.rodata']; text = secs['.text']
    targets = {}
    for m in re.finditer(rb'[\x20-\x7e]{4,}\0', data[ro[4]:ro[4] + ro[5]]):
        s = m.group()[:-1].decode()
        if any(re.search(p, s) for p in patterns): targets[ro[3] + m.start()] = s
    md = Cs(CS_ARCH_ARM64, CS_MODE_ARM); md.detail = False
    pages = {}
    code = data[text[4]:text[4] + text[5]]
    for ins in md.disasm(code, text[3]):
        if ins.mnemonic == 'adrp':
            reg, imm = ins.op_str.split(', #'); pages[reg] = (int(imm, 16), ins.address)
        elif ins.mnemonic in ('add', 'ldr') and ', #' in ins.op_str:
            parts = re.split(r',\s*', ins.op_str.replace('[', '').replace(']', ''))
            if len(parts) >= 3 and parts[1] in pages and parts[2].startswith('#'):
                addr = pages[parts[1]][0] + int(parts[2][1:], 16)
                if addr in targets and ins.address - pages[parts[1]][1] < 64:
                    owner = next((n for v, sz, n in reversed(syms) if v <= ins.address < v + sz), '?')
                    print(f'{ins.address:#x}  {targets[addr]!r}  in {owner}')
    for addr, s in sorted(targets.items()): print(f'string {addr:#x} {s!r}')

main()
