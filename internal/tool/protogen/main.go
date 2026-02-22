// protogen generates encode/decode methods, Type methods, type constants,
// and the newMessage dispatch for protocol message structs.
//
// It reads struct definitions annotated with //proto:msg 0xNN directives.
// Non-message structs referenced by message fields are discovered via the
// type graph and sorted topologically; leaves first.
//
// Usage:
//
//	go run ./internal/tool/protogen [flags]
//	  -pkg string    package pattern to load; default "."
//	  -output string output file path; default stdout
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"golang.org/x/tools/go/packages"
)

type typeKind int

const (
	kindPrim  typeKind = iota // scalar: uint8, string, bool, ...
	kindNamed                 // value struct: SessionInfo
	kindPtr                   // *SessionInfo
	kindSlice                 // []T for any T
)

type prim int

const (
	primU8 prim = iota
	primU16
	primU32
	primU64
	primI32
	primBool
	primString
	primBytes
)

var primByName = map[string]prim{
	"uint8":  primU8,
	"uint16": primU16,
	"uint32": primU32,
	"uint64": primU64,
	"int32":  primI32,
	"bool":   primBool,
	"string": primString,
}

var primGoName = map[prim]string{
	primU8:     "uint8",
	primU16:    "uint16",
	primU32:    "uint32",
	primU64:    "uint64",
	primI32:    "int32",
	primBool:   "bool",
	primString: "string",
	primBytes:  "[]byte",
}

var primSuffix = map[prim]string{
	primU8:     "U8",
	primU16:    "U16",
	primU32:    "U32",
	primU64:    "U64",
	primI32:    "I32",
	primBool:   "Bool",
	primString: "String",
	primBytes:  "Bytes",
}

type typ struct {
	Kind typeKind
	Prim prim
	Name string
	Elem *typ
}

func (t typ) IsPrim() bool  { return t.Kind == kindPrim }
func (t typ) IsNamed() bool { return t.Kind == kindNamed }
func (t typ) IsPtr() bool   { return t.Kind == kindPtr }
func (t typ) IsSlice() bool { return t.Kind == kindSlice }

func (t typ) GoName() string {
	switch t.Kind {
	case kindPrim:
		return primGoName[t.Prim]
	case kindNamed:
		return t.Name
	case kindPtr:
		return "*" + t.Elem.GoName()
	case kindSlice:
		return "[]" + t.Elem.GoName()
	}
	return "?"
}

func (t typ) WriteFn() string { return "Write" + primSuffix[t.Prim] }
func (t typ) ReadFn() string  { return "Read" + primSuffix[t.Prim] }

func (t typ) StructRef() string {
	switch t.Kind {
	case kindNamed:
		return t.Name
	case kindPtr, kindSlice:
		if t.Elem != nil {
			return t.Elem.StructRef()
		}
	}
	return ""
}

type field struct {
	Name string
	Type typ
}

type structDef struct {
	Name   string
	Fields []field
	IsMsg  bool
	ID     uint8
}

var directiveRE = regexp.MustCompile(`^//proto:msg\s+(0x[0-9a-fA-F]+|\d+)\s*$`)

func extract(fset *token.FileSet, files []*ast.File) (map[string]structDef, []structDef) {
	var messages []structDef
	allStructs := map[string]structDef{}

	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}

				sd, ok := parseStruct(ts.Name.Name, st)
				if !ok {
					continue
				}

				if idStr, ok := parseDirective(gd.Doc); ok {
					id, err := strconv.ParseUint(idStr, 0, 8)
					if err != nil {
						fatal("bad message ID %q for %s: %v", idStr, sd.Name, err)
					}
					sd.IsMsg = true
					sd.ID = uint8(id)
					messages = append(messages, sd)
				}

				allStructs[sd.Name] = sd
			}
		}
	}

	return allStructs, messages
}

func parseDirective(doc *ast.CommentGroup) (string, bool) {
	if doc == nil {
		return "", false
	}
	for _, c := range doc.List {
		m := directiveRE.FindStringSubmatch(c.Text)
		if m != nil {
			return m[1], true
		}
	}
	return "", false
}

func parseStruct(name string, st *ast.StructType) (structDef, bool) {
	sd := structDef{Name: name}
	if st.Fields == nil {
		return sd, true
	}
	for _, fl := range st.Fields.List {
		if len(fl.Names) == 0 {
			continue
		}
		for _, n := range fl.Names {
			t, ok := parseType(fl.Type)
			if !ok {
				return structDef{}, false
			}
			sd.Fields = append(sd.Fields, field{Name: n.Name, Type: t})
		}
	}
	return sd, true
}

func parseType(expr ast.Expr) (typ, bool) {
	switch t := expr.(type) {
	case *ast.Ident:
		if p, ok := primByName[t.Name]; ok {
			return typ{Kind: kindPrim, Prim: p}, true
		}
		return typ{Kind: kindNamed, Name: t.Name}, true
	case *ast.ArrayType:
		if t.Len != nil {
			return typ{}, false
		}
		if ident, ok := t.Elt.(*ast.Ident); ok && ident.Name == "byte" {
			return typ{Kind: kindPrim, Prim: primBytes}, true
		}
		elem, ok := parseType(t.Elt)
		if !ok {
			return typ{}, false
		}
		return typ{Kind: kindSlice, Elem: &elem}, true
	case *ast.StarExpr:
		elem, ok := parseType(t.X)
		if !ok {
			return typ{}, false
		}
		return typ{Kind: kindPtr, Elem: &elem}, true
	}
	return typ{}, false
}

func validate(messages []structDef, allStructs map[string]structDef) {
	ids := map[uint8]string{}
	for _, m := range messages {
		if prev, ok := ids[m.ID]; ok {
			fatal("duplicate message ID 0x%02x: %s and %s", m.ID, prev, m.Name)
		}
		ids[m.ID] = m.Name
	}

	for _, sd := range allStructs {
		for _, f := range sd.Fields {
			ref := f.Type.StructRef()
			if ref != "" {
				if _, ok := allStructs[ref]; !ok {
					fatal("struct %s field %s references unknown type %s", sd.Name, f.Name, ref)
				}
			}
		}
	}
}

// toposort returns structs in dependency order; leaves first.
// Uses Kahn's algorithm on dependency count.
func toposort(messages []structDef, allStructs map[string]structDef) []structDef {
	needed := map[string]bool{}
	var walk func(string)
	walk = func(name string) {
		if needed[name] {
			return
		}
		needed[name] = true
		for _, f := range allStructs[name].Fields {
			if ref := f.Type.StructRef(); ref != "" {
				walk(ref)
			}
		}
	}
	for _, m := range messages {
		walk(m.Name)
	}

	// deps[A] = {B, C} means A depends on B and C.
	// dependents[B] = {A} means A depends on B.
	deps := map[string][]string{}
	dependents := map[string][]string{}
	for name := range needed {
		for _, f := range allStructs[name].Fields {
			ref := f.Type.StructRef()
			if ref != "" && ref != name {
				deps[name] = append(deps[name], ref)
				dependents[ref] = append(dependents[ref], name)
			}
		}
	}

	depCount := map[string]int{}
	for name := range needed {
		depCount[name] = len(deps[name])
	}

	var queue []string
	for name, n := range depCount {
		if n == 0 {
			queue = append(queue, name)
		}
	}
	sort.Strings(queue)

	var result []structDef
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		result = append(result, allStructs[name])

		for _, other := range dependents[name] {
			depCount[other]--
			if depCount[other] == 0 {
				idx := sort.SearchStrings(queue, other)
				queue = append(queue, "")
				copy(queue[idx+1:], queue[idx:])
				queue[idx] = other
			}
		}
	}

	if len(result) != len(needed) {
		fatal("cycle detected in struct dependencies")
	}

	return result
}

func emit(pkgName string, messages []structDef, ordered []structDef) []byte {
	sorted := make([]structDef, len(messages))
	copy(sorted, messages)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	data := struct {
		Package  string
		Messages []structDef
		Ordered  []structDef
	}{
		Package:  pkgName,
		Messages: sorted,
		Ordered:  ordered,
	}

	var buf bytes.Buffer
	if err := genTmpl.Execute(&buf, data); err != nil {
		fatal("template: %v", err)
	}
	return buf.Bytes()
}

var funcs = template.FuncMap{
	"hex": func(id uint8) string { return fmt.Sprintf("0x%02x", id) },
	"needsErr": func(sd structDef) bool {
		for _, f := range sd.Fields {
			if f.Type.Kind == kindPrim {
				return true
			}
		}
		return false
	},
	"lenVar": func(f field) string {
		return strings.ToLower(f.Name[:1]) + f.Name[1:] + "Len"
	},
}

var genTmpl = template.Must(template.New("gen").Funcs(funcs).Parse(`// Code generated by protogen. DO NOT EDIT.

package {{.Package}}

import "fmt"

const (
{{- range .Messages}}
	Type{{.Name}} uint8 = {{hex .ID}}
{{- end}}
)
{{range .Ordered}}
{{- if .IsMsg}}
func (m *{{.Name}}) Type() uint8 { return Type{{.Name}} }
{{end}}
{{- if not .Fields}}
func (m *{{.Name}}) encode(_ *Encoder) error { return nil }

func (m *{{.Name}}) decode(_ *Decoder) error { return nil }
{{else}}
func (m *{{.Name}}) encode(e *Encoder) error {
{{- range .Fields}}{{template "encode" .}}
{{- end}}
	return nil
}

func (m *{{.Name}}) decode(d *Decoder) error {
{{- if needsErr .}}
	var err error
{{- end}}
{{- range .Fields}}{{template "decode" .}}
{{- end}}
	return nil
}
{{end}}
{{end}}
func newMessage(t uint8) (Message, error) {
	switch t {
{{- range .Messages}}
	case Type{{.Name}}:
		return &{{.Name}}{}, nil
{{- end}}
	default:
		return nil, fmt.Errorf("unknown message type: 0x%02x", t)
	}
}

{{- define "encode"}}
{{- if .Type.IsPrim}}
	if err := e.{{.Type.WriteFn}}(m.{{.Name}}); err != nil {
		return err
	}
{{- else if .Type.IsSlice}}
	if err := e.WriteU32(uint32(len(m.{{.Name}}))); err != nil {
		return err
	}
{{- if .Type.Elem.StructRef}}
	for i := range m.{{.Name}} {
		if err := m.{{.Name}}[i].encode(e); err != nil {
			return err
		}
	}
{{- else}}
	for _, v := range m.{{.Name}} {
		if err := e.{{.Type.Elem.WriteFn}}(v); err != nil {
			return err
		}
	}
{{- end}}
{{- else if .Type.IsPtr}}
	if m.{{.Name}} == nil {
		if err := e.WriteU8(0); err != nil {
			return err
		}
	} else {
		if err := e.WriteU8(1); err != nil {
			return err
		}
		if err := m.{{.Name}}.encode(e); err != nil {
			return err
		}
	}
{{- else if .Type.IsNamed}}
	if err := m.{{.Name}}.encode(e); err != nil {
		return err
	}
{{- end}}
{{- end}}

{{- define "decode"}}
{{- if .Type.IsPrim}}
	if m.{{.Name}}, err = d.{{.Type.ReadFn}}(); err != nil {
		return err
	}
{{- else if .Type.IsSlice}}
	{
		{{lenVar .}}, err := d.ReadU32()
		if err != nil {
			return err
		}
		m.{{.Name}} = make({{.Type.GoName}}, {{lenVar .}})
{{- if .Type.Elem.StructRef}}
		for i := range m.{{.Name}} {
			if err := m.{{.Name}}[i].decode(d); err != nil {
				return err
			}
		}
{{- else}}
		for i := range m.{{.Name}} {
			if m.{{.Name}}[i], err = d.{{.Type.Elem.ReadFn}}(); err != nil {
				return err
			}
		}
{{- end}}
	}
{{- else if .Type.IsPtr}}
	{
		flag, err := d.ReadU8()
		if err != nil {
			return err
		}
		if flag != 0 {
			m.{{.Name}} = &{{.Type.Elem.Name}}{}
			if err := m.{{.Name}}.decode(d); err != nil {
				return err
			}
		}
	}
{{- else if .Type.IsNamed}}
	if err := m.{{.Name}}.decode(d); err != nil {
		return err
	}
{{- end}}
{{- end}}
`))

func main() {
	var (
		flagPkg    string
		flagOutput string
	)
	flag.StringVar(&flagPkg, "pkg", ".", "package pattern to load")
	flag.StringVar(&flagOutput, "output", "", "output file; default stdout")
	flag.Parse()

	cfg := &packages.Config{
		Mode: packages.NeedSyntax | packages.NeedFiles | packages.NeedName | packages.NeedCompiledGoFiles,
	}
	pkgs, err := packages.Load(cfg, flagPkg)
	if err != nil {
		fatal("load package: %v", err)
	}
	if len(pkgs) == 0 {
		fatal("no packages found for %q", flagPkg)
	}
	if packages.PrintErrors(pkgs) > 0 {
		os.Exit(1)
	}

	pkg := pkgs[0]

	allStructs, messages := extract(pkg.Fset, pkg.Syntax)
	validate(messages, allStructs)
	ordered := toposort(messages, allStructs)

	code := emit(pkg.Name, messages, ordered)
	formatted, err := format.Source(code)
	if err != nil {
		os.Stderr.Write(code)
		fatal("gofmt: %v", err)
	}

	if flagOutput == "" {
		os.Stdout.Write(formatted)
		return
	}
	if err := os.WriteFile(flagOutput, formatted, 0o644); err != nil {
		fatal("write %s: %v", flagOutput, err)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "protogen: "+format+"\n", args...)
	os.Exit(1)
}
