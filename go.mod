module github.com/selman/hauntty

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
	golang.org/x/mod v0.23.0 // indirect
	golang.org/x/sync v0.11.0 // indirect
	golang.org/x/tools v0.30.0 // indirect
	honnef.co/go/tools v0.6.1 // indirect
)

require (
	github.com/google/go-cmp v0.6.0 // indirect
	gotest.tools/v3 v3.5.2
)

tool honnef.co/go/tools/cmd/staticcheck
