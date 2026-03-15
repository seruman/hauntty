package protocol

// DumpFormat encodes the dump output format (bits 0–3) and optional
// flags (bits 4+) in a single byte on the wire.
type DumpFormat uint8

const (
	DumpPlain          DumpFormat = 0    // Plain text, no escape sequences.
	DumpVT             DumpFormat = 1    // VT with colors (safe for display).
	DumpHTML           DumpFormat = 2    // HTML with inline CSS colors.
	DumpFlagUnwrap     DumpFormat = 0x10 // Bit 4: join soft-wrapped lines.
	DumpFlagScrollback DumpFormat = 0x20 // Bit 5: include scrollback history.
	DumpFormatMask     DumpFormat = 0x0F // Bits 0-3: format selector.
)
