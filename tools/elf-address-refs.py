"""Read-only AArch64 local constant references. No vendor code is executed.

This is a candidate index, not a control-flow proof. It follows ADR/ADRP,
constant ADD and MOV within EH function ranges, invalidates written registers
and call-clobbered registers, and emits addresses only. Check each candidate
with elf-disassemble.py. Dependencies: pyelftools, capstone.
"""
import argparse
from capstone import Cs, CS_ARCH_ARM64, CS_MODE_ARM
from capstone.arm64 import ARM64_OP_IMM, ARM64_OP_REG
from elftools.elf.elffile import ELFFile

p = argparse.ArgumentParser()
p.add_argument('file')
p.add_argument('--target', type=lambda value: int(value, 0), required=True)
a = p.parse_args()

def register(instruction, identifier):
    name = instruction.reg_name(identifier)
    if name.startswith('w') and name[1:].isdigit():
        name = 'x' + name[1:]
    return name

with open(a.file, 'rb') as stream:
    elf = ELFFile(stream)
    starts = set()
    for entry in elf.get_dwarf_info().EH_CFI_entries():
        if hasattr(entry, 'header') and 'initial_location' in entry.header:
            starts.add(entry['initial_location'])
    text = elf.get_section_by_name('.text')
    md = Cs(CS_ARCH_ARM64, CS_MODE_ARM)
    md.detail = True
    md.skipdata = True
    constants = {}
    current = None
    for instruction in md.disasm(text.data(), text['sh_addr']):
        if instruction.address in starts:
            constants.clear()
            current = instruction.address
        if instruction.id == 0:
            constants.clear()
            continue
        operands = instruction.operands
        result = None
        if instruction.mnemonic in ('adr', 'adrp') and len(operands) == 2 and operands[1].type == ARM64_OP_IMM:
            result = operands[1].imm
        elif instruction.mnemonic == 'add' and len(operands) == 3 and operands[1].type == ARM64_OP_REG and operands[2].type == ARM64_OP_IMM:
            source = register(instruction, operands[1].reg)
            if source in constants:
                result = constants[source] + (operands[2].imm << operands[2].shift.value)
        elif instruction.mnemonic == 'mov' and len(operands) == 2:
            if operands[1].type == ARM64_OP_IMM:
                result = operands[1].imm
            elif operands[1].type == ARM64_OP_REG:
                result = constants.get(register(instruction, operands[1].reg))
        _, written = instruction.regs_access()
        for identifier in written:
            constants.pop(register(instruction, identifier), None)
        if result is not None and operands and operands[0].type == ARM64_OP_REG:
            destination = register(instruction, operands[0].reg)
            constants[destination] = result
            if result == a.target:
                print(hex(instruction.address), 'function=' + (hex(current) if current is not None else 'unknown'), instruction.mnemonic, instruction.op_str)
        if instruction.mnemonic in ('bl', 'blr'):
            for number in range(19):
                constants.pop('x' + str(number), None)
