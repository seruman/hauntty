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
	msg := &Create{
		Name:       "test-session",
		Command:    []string{"/bin/bash", "-l"},
		Env:        []string{"TERM=xterm-256color"},
		CWD:        "/home/user",
		Scrollback: 10000,
		Force:      true,
	}
	got := roundTrip(t, msg).(*Create)
	assert.Equal(t, got.Name, msg.Name)
	assert.DeepEqual(t, got.Command, msg.Command)
	assert.DeepEqual(t, got.Env, msg.Env)
	assert.Equal(t, got.CWD, msg.CWD)
	assert.Equal(t, got.Scrollback, msg.Scrollback)
	assert.Equal(t, got.Force, msg.Force)
}

func TestAttachEncodeDecode(t *testing.T) {
	msg := &Attach{
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
	got := roundTrip(t, msg).(*Attach)
	assert.Equal(t, got.Name, msg.Name)
	assert.DeepEqual(t, got.Command, msg.Command)
	assert.DeepEqual(t, got.Env, msg.Env)
	assert.Equal(t, got.CWD, msg.CWD)
	assert.Equal(t, got.Cols, msg.Cols)
	assert.Equal(t, got.Rows, msg.Rows)
	assert.Equal(t, got.Xpixel, msg.Xpixel)
	assert.Equal(t, got.Ypixel, msg.Ypixel)
	assert.Equal(t, got.ReadOnly, msg.ReadOnly)
	assert.Equal(t, got.Restore, msg.Restore)
	assert.Equal(t, got.Scrollback, msg.Scrollback)
}

func TestDumpEncodeDecode(t *testing.T) {
	msg := &Dump{
		Name:   "session-1",
		Format: DumpHTML,
	}
	got := roundTrip(t, msg).(*Dump)
	assert.Equal(t, got.Name, msg.Name)
	assert.Equal(t, got.Format, msg.Format)
}

func TestSendKeyEncodeDecode(t *testing.T) {
	msg := &SendKey{
		Name: "session-1",
		Key:  KeyCode(0x41),
		Mods: KeyMods(0x02),
	}
	got := roundTrip(t, msg).(*SendKey)
	assert.Equal(t, got.Name, msg.Name)
	assert.Equal(t, got.Key, msg.Key)
	assert.Equal(t, got.Mods, msg.Mods)
}
