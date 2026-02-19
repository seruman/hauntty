package main

import (
	"os"
	"strings"

	"code.selman.me/hauntty/client"
	"code.selman.me/hauntty/internal/config"
	"github.com/posener/complete"
)

type sessionPredictor struct{}

func (p sessionPredictor) Predict(a complete.Args) []string {
	socket := socketFromCompletionArgs(a)
	c, err := client.Connect(socket)
	if err != nil {
		return nil
	}
	defer c.Close()

	sessions, err := c.List()
	if err != nil {
		return nil
	}

	out := make([]string, 0, len(sessions.Sessions))
	for _, s := range sessions.Sessions {
		out = append(out, s.Name)
	}
	return out
}

func socketFromCompletionArgs(a complete.Args) string {
	for i := 0; i < len(a.All); i++ {
		arg := a.All[i]
		if arg == "--socket" && i+1 < len(a.All) {
			return a.All[i+1]
		}
		if strings.HasPrefix(arg, "--socket=") {
			return strings.TrimPrefix(arg, "--socket=")
		}
	}
	if socket := os.Getenv("HAUNTTY_SOCKET"); socket != "" {
		return socket
	}
	return config.SocketPath()
}
