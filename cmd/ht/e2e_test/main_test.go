package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"gotest.tools/v3/fs"
	"gotest.tools/v3/icmd"

	"code.selman.me/hauntty/internal/config"
	"code.selman.me/hauntty/internal/termtest"
)

var htBin string

type testEnv struct {
	t       *testing.T
	sock    string
	cfgHome string
}

func TestMain(m *testing.M) {
	dir := fs.NewDir(&testing.T{}, "ht-test")
	defer dir.Remove()

	htBin = dir.Join("ht")
	result := icmd.RunCommand("go", "build", "-o", htBin, "code.selman.me/hauntty/cmd/ht")
	if result.ExitCode != 0 {
		panic("build ht: " + result.Combined())
	}

	os.Exit(m.Run())
}

func setup(t *testing.T, cfg *config.Config) *testEnv {
	if cfg == nil {
		cfg = config.Default()
	}

	dir := fs.NewDir(t, "ht-e2e",
		fs.WithDir("config", fs.WithDir("hauntty")),
		fs.WithDir("run"),
		fs.WithDir("state"),
	)
	sock := dir.Join("run", "ht.sock")
	cfgHome := dir.Join("config")
	stateHome := dir.Join("state")

	cfg.Daemon.SocketPath = sock

	var cfgBuf strings.Builder
	if err := toml.NewEncoder(&cfgBuf).Encode(cfg); err != nil {
		t.Fatalf("setup: encode config: %v", err)
	}
	if err := os.WriteFile(dir.Join("config", "hauntty", "config.toml"), []byte(cfgBuf.String()), 0o644); err != nil {
		t.Fatalf("setup: write config: %v", err)
	}

	t.Setenv("XDG_STATE_HOME", stateHome)

	return &testEnv{t: t, sock: sock, cfgHome: cfgHome}
}

func (e *testEnv) env() []string {
	return []string{
		"HAUNTTY_SOCKET=" + e.sock,
		"XDG_CONFIG_HOME=" + e.cfgHome,
		"XDG_STATE_HOME=" + os.Getenv("XDG_STATE_HOME"),
		"HT_BIN=" + htBin,
	}
}

func (e *testEnv) term(cmd []string, opts ...termtest.Option) *termtest.Term {
	path := strings.Join([]string{filepath.Dir(htBin), "/usr/bin", "/bin", "/usr/sbin", "/sbin"}, ":")
	env := append([]string{"PATH=" + path}, e.env()...)
	opts = append([]termtest.Option{termtest.WithEnv(env...)}, opts...)
	return termtest.New(e.t, cmd, opts...)
}

func (e *testEnv) run(args ...string) *icmd.Result {
	cmd := icmd.Command(htBin, args...)
	return icmd.RunCmd(cmd, icmd.WithEnv(append(os.Environ(), e.env()...)...))
}
