package protocol

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestRequestMessageTypes(t *testing.T) {
	tests := []struct {
		name    string
		message Message
		want    MessageType
	}{
		{"Create", &Create{}, TypeCreate},
		{"Attach", &Attach{}, TypeAttach},
		{"Input", &Input{}, TypeInput},
		{"Resize", &Resize{}, TypeResize},
		{"Detach", &Detach{}, TypeDetach},
		{"List", &List{}, TypeList},
		{"Kill", &Kill{}, TypeKill},
		{"Send", &Send{}, TypeSend},
		{"SendKey", &SendKey{}, TypeSendKey},
		{"Dump", &Dump{}, TypeDump},
		{"Prune", &Prune{}, TypePrune},
		{"Kick", &Kick{}, TypeKick},
		{"Status", &Status{}, TypeStatus},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.message.Type(), tt.want)
		})
	}
}

func TestCreateEncodeDecode(t *testing.T) {
	message := &Create{
		Name:       "test-session",
		Command:    []string{"/bin/bash", "-l"},
		Env:        []string{"TERM=xterm-256color"},
		CWD:        "/home/user",
		Scrollback: 10000,
		Force:      true,
	}

	got := roundTrip(t, message).(*Create)
	assert.DeepEqual(t, got, message)
}

func TestAttachEncodeDecode(t *testing.T) {
	message := &Attach{
		Name:       "session-1",
		Command:    []string{"/bin/zsh"},
		Env:        []string{"HOME=/home/user"},
		CWD:        "/tmp",
		Cols:       120,
		Rows:       40,
		Xpixel:     1920,
		Ypixel:     1080,
		ReadOnly:   true,
		Restore:    false,
		Scrollback: 5000,
	}

	got := roundTrip(t, message).(*Attach)
	assert.DeepEqual(t, got, message)
}

func TestDumpEncodeDecode(t *testing.T) {
	message := &Dump{
		Name:   "session-1",
		Format: DumpHTML,
	}

	got := roundTrip(t, message).(*Dump)
	assert.DeepEqual(t, got, message)
}

func TestSendKeyEncodeDecode(t *testing.T) {
	message := &SendKey{
		Name: "session-1",
		Key:  KeyCode(0x41),
		Mods: KeyMods(0x02),
	}

	got := roundTrip(t, message).(*SendKey)
	assert.DeepEqual(t, got, message)
}
