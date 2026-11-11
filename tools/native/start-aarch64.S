/* Minimal glibc AArch64 startup: vendor libraries are NOT DT_NEEDED. */
.text
.global _start
.type _start,%function
_start:
    mov x29, xzr
    mov x30, xzr
    mov x5, x0
    ldr x1, [sp]
    add x2, sp, #8
    mov x6, sp
    adrp x0, main
    add x0, x0, :lo12:main
    mov x3, xzr
    mov x4, xzr
    bl __libc_start_main
    brk #0
.section .note.GNU-stack,"",%progbits
