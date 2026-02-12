package client

import (
	"fmt"
	"strings"
)

func ParseKeyNotation(notation string) ([]byte, error) {
	notation = strings.TrimSpace(strings.ToLower(notation))

	switch notation {
	case "enter", "return":
		return []byte{0x0d}, nil
	case "escape", "esc":
		return []byte{0x1b}, nil
	case "tab":
		return []byte{0x09}, nil
	case "backspace":
		return []byte{0x7f}, nil
	case "space":
		return []byte{0x20}, nil
	}

	parts := strings.Split(notation, "+")
	if len(parts) == 2 && parts[0] == "ctrl" {
		key := parts[1]
		switch key {
		case "\\":
			return []byte{0x1c}, nil
		case "[":
			return []byte{0x1b}, nil
		case "]":
			return []byte{0x1d}, nil
		case "^":
			return []byte{0x1e}, nil
		case "_":
			return []byte{0x1f}, nil
		case "@":
			return []byte{0x00}, nil
		}
		if len(key) == 1 {
			ch := key[0]
			if ch >= 'a' && ch <= 'z' {
				return []byte{ch - 0x60}, nil
			}
		}
	}

	return nil, fmt.Errorf("unknown key notation: %q", notation)
}
