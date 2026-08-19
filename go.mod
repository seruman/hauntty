module code.selman.me/hauntty

go 1.27rc3

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/alecthomas/kong v1.14.0
	github.com/creack/pty v1.1.24
	golang.org/x/sys v0.44.0
	golang.org/x/term v0.40.0
	gotest.tools/v3 v3.5.2
)

require (
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/ncruces/wasm2go v0.3.1 // indirect
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678 // indirect
	golang.org/x/mod v0.36.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/telemetry v0.0.0-20260508192327-42602be52be6 // indirect
	golang.org/x/tools v0.45.0 // indirect
	honnef.co/go/tools v0.7.0 // indirect
	mvdan.cc/gofumpt v0.9.2 // indirect
)

tool (
	github.com/ncruces/wasm2go
	golang.org/x/tools/cmd/deadcode
	golang.org/x/tools/cmd/goimports
	honnef.co/go/tools/cmd/staticcheck
	mvdan.cc/gofumpt
)

replace github.com/ncruces/wasm2go => github.com/seruman/wasm2go v0.0.0-20260819030703-5ffe00aac316
