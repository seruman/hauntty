package protocol

const (
	DumpPlain          uint8 = 0    // Plain text, no escape sequences.
	DumpVT             uint8 = 1    // VT with colors (safe for display).
	DumpHTML           uint8 = 2    // HTML (reserved).
	DumpFlagUnwrap     uint8 = 0x10 // Bit 4: join soft-wrapped lines.
	DumpFlagScrollback uint8 = 0x20 // Bit 5: include scrollback history.
	DumpFormatMask     uint8 = 0x0F // Bits 0-3: format selector.
)
