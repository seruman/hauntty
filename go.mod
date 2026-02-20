module code.selman.me/hauntty

go 1.26

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/alecthomas/kong v1.14.0
	github.com/creack/pty v1.1.24
	github.com/tetratelabs/wazero v1.11.0
	golang.org/x/sys v0.41.0
	golang.org/x/term v0.40.0
)

require (
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678 // indirect
	golang.org/x/mod v0.33.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/telemetry v0.0.0-20260209163413-e7419c687ee4 // indirect
	golang.org/x/tools v0.42.0 // indirect
	golang.org/x/tools/go/expect v0.1.1-deprecated // indirect
	honnef.co/go/tools v0.6.1 // indirect
	mvdan.cc/gofumpt v0.9.2 // indirect
)

require (
	github.com/google/go-cmp v0.7.0 // indirect
	gotest.tools/v3 v3.5.2
)

tool (
	golang.org/x/tools/cmd/deadcode
	golang.org/x/tools/cmd/goimports
	honnef.co/go/tools/cmd/staticcheck
	mvdan.cc/gofumpt
)
