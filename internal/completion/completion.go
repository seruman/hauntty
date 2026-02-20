package completion

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/alecthomas/kong"
)

type Spec struct {
	GlobalFlags []Flag
	Commands    []Command
}

type Command struct {
	Name        string
	Aliases     []string
	Help        string
	Flags       []Flag
	Positionals []Positional
}

type Positional struct {
	Name         string
	DynamicTopic string
}

type Flag struct {
	Name     string
	Short    rune
	Help     string
	HasValue bool
	Enum     []string
}

type renderData struct {
	Bin              string
	Helper           string
	GlobalFlags      []Flag
	Commands         []commandRender
	BashCommands     string
	ZshCommandValues string
}

type commandRender struct {
	Name         string
	Help         string
	AllNames     []string
	SeenNames    string
	CasePattern  string
	Flags        []Flag
	Positionals  []Positional
	DynamicName  string
	DynamicTopic string
	BashFlags    string
	ZshArgsBlock string
}

func BuildSpec(root *kong.Node, dynamicFirstPositional map[string]string) Spec {
	if root == nil {
		return Spec{}
	}
	spec := Spec{GlobalFlags: collectFlags(root.Flags)}
	for _, child := range root.Children {
		if child == nil || child.Hidden {
			continue
		}
		cmd := Command{
			Name:        child.Name,
			Aliases:     append([]string(nil), child.Aliases...),
			Help:        child.Help,
			Flags:       collectFlags(child.Flags),
			Positionals: collectPositionals(child, dynamicFirstPositional[child.Name]),
		}
		spec.Commands = append(spec.Commands, cmd)
	}
	return spec
}

func collectFlags(flags []*kong.Flag) []Flag {
	out := make([]Flag, 0, len(flags))
	for _, f := range flags {
		if f == nil || f.Hidden {
			continue
		}
		out = append(out, Flag{
			Name:     f.Name,
			Short:    f.Short,
			Help:     f.Help,
			HasValue: !f.IsBool(),
			Enum:     nonEmptyValues(f.EnumSlice()),
		})
	}
	return out
}

func collectPositionals(node *kong.Node, firstPositionalTopic string) []Positional {
	out := make([]Positional, 0, len(node.Positional))
	for i, p := range node.Positional {
		pos := Positional{Name: strings.ToLower(p.Name)}
		if i == 0 {
			pos.DynamicTopic = firstPositionalTopic
		}
		out = append(out, pos)
	}
	return out
}

func nonEmptyValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func Generate(shell string, bin string, helperCommand string, spec Spec) (string, error) {
	data := buildRenderData(bin, helperCommand, spec)
	switch shell {
	case "fish":
		return render(fishTemplate, data)
	case "zsh":
		return render(zshTemplate, data)
	case "bash":
		return render(bashTemplate, data)
	default:
		return "", fmt.Errorf("unsupported shell: %s", shell)
	}
}

func buildRenderData(bin string, helper string, spec Spec) renderData {
	r := renderData{
		Bin:         bin,
		Helper:      helper,
		GlobalFlags: spec.GlobalFlags,
		Commands:    make([]commandRender, 0, len(spec.Commands)),
	}

	bashCommands := make([]string, 0, len(spec.Commands)*2)
	zshCommandValues := make([]string, 0, len(spec.Commands)*2)
	for _, cmd := range spec.Commands {
		allNames := append([]string{cmd.Name}, cmd.Aliases...)
		bashCommands = append(bashCommands, allNames...)
		for _, name := range allNames {
			zshCommandValues = append(zshCommandValues, fmt.Sprintf("%q", name+"["+cmd.Help+"]"))
		}

		zshArgs := make([]string, 0, len(cmd.Flags)+len(cmd.Positionals))
		for _, f := range cmd.Flags {
			zshArgs = append(zshArgs, zshFlagSpec(f))
		}
		dynamicName := ""
		dynamicTopic := ""
		for i, p := range cmd.Positionals {
			if i == 0 && p.DynamicTopic != "" {
				dynamicName = p.Name
				dynamicTopic = p.DynamicTopic
				zshArgs = append(zshArgs, fmt.Sprintf("'%d:%s:($(%s %s %s))'", i+1, p.Name, bin, helper, p.DynamicTopic))
				continue
			}
			zshArgs = append(zshArgs, fmt.Sprintf("'%d:%s:'", i+1, p.Name))
		}
		if len(zshArgs) == 0 {
			zshArgs = append(zshArgs, "'*:arg:'")
		}

		bashFlags := make([]string, 0, len(cmd.Flags)*2)
		for _, f := range cmd.Flags {
			bashFlags = append(bashFlags, "--"+f.Name)
			if f.Short != 0 {
				bashFlags = append(bashFlags, "-"+string(f.Short))
			}
		}

		r.Commands = append(r.Commands, commandRender{
			Name:         cmd.Name,
			Help:         cmd.Help,
			AllNames:     allNames,
			SeenNames:    strings.Join(allNames, " "),
			CasePattern:  strings.Join(allNames, "|"),
			Flags:        cmd.Flags,
			Positionals:  cmd.Positionals,
			DynamicName:  dynamicName,
			DynamicTopic: dynamicTopic,
			BashFlags:    strings.Join(bashFlags, " "),
			ZshArgsBlock: joinWithContinuation(zshArgs, "    "),
		})
	}
	sort.Strings(bashCommands)
	r.BashCommands = strings.Join(bashCommands, " ")
	r.ZshCommandValues = joinWithContinuation(zshCommandValues, "        ")
	return r
}

func joinWithContinuation(lines []string, indent string) string {
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	for i, line := range lines {
		b.WriteString(indent)
		b.WriteString(line)
		if i < len(lines)-1 {
			b.WriteString(" \\\n")
		} else {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func render(tmpl string, data renderData) (string, error) {
	t, err := template.New("completion").Funcs(template.FuncMap{
		"q":            func(s string) string { return fmt.Sprintf("%q", s) },
		"fishFlagOpts": fishFlagOpts,
		"zshFlagSpec":  zshFlagSpec,
	}).Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}
	return b.String(), nil
}

func fishFlagOpts(f Flag) string {
	var b strings.Builder
	if f.HasValue {
		if len(f.Enum) > 0 {
			fmt.Fprintf(&b, " -xa '%s'", strings.Join(f.Enum, " "))
		} else {
			b.WriteString(" -x")
		}
	}
	if f.Short != 0 {
		fmt.Fprintf(&b, " -s %c", f.Short)
	}
	return b.String()
}

func zshFlagSpec(f Flag) string {
	help := strings.ReplaceAll(f.Help, "'", "")
	if f.Short != 0 {
		if f.HasValue {
			return fmt.Sprintf("'(-%c --%s)'{-%c,--%s=}'[%s]:value:'", f.Short, f.Name, f.Short, f.Name, help)
		}
		return fmt.Sprintf("'(-%c --%s)'{-%c,--%s}'[%s]'", f.Short, f.Name, f.Short, f.Name, help)
	}
	if f.HasValue {
		if len(f.Enum) > 0 {
			return fmt.Sprintf("'--%s=[%s]:(%s)'", f.Name, help, strings.Join(f.Enum, " "))
		}
		return fmt.Sprintf("'--%s=[%s]:value:'", f.Name, help)
	}
	return fmt.Sprintf("'--%s[%s]'", f.Name, help)
}

const fishTemplate = `# fish shell completion for {{.Bin}}
# generated by hauntty
{{- range .GlobalFlags}}
complete -c {{$.Bin}} -f{{fishFlagOpts .}} -l {{.Name}} -d {{q .Help}}
{{- end}}

{{- range .Commands}}
{{- $cmd := .}}
{{- range .AllNames}}
complete -c {{$.Bin}} -f -n '__fish_use_subcommand' -a {{.}} -d {{q $cmd.Help}}
{{- end}}
{{- range .Flags}}
complete -c {{$.Bin}} -f -n '__fish_seen_subcommand_from {{$cmd.SeenNames}}'{{fishFlagOpts .}} -l {{.Name}} -d {{q .Help}}
{{- end}}
{{- if .DynamicTopic}}
complete -c {{$.Bin}} -f -n '__fish_seen_subcommand_from {{.SeenNames}}' -a "({{$.Bin}} {{$.Helper}} {{.DynamicTopic}})" -d {{q .DynamicName}}
{{- end}}

{{- end}}
`

const zshTemplate = `#compdef {{.Bin}}
compdef _{{.Bin}} {{.Bin}}
_{{.Bin}}() {
  local line state
  _arguments -S -C -s \
{{- range .GlobalFlags}}
    {{zshFlagSpec .}} \
{{- end}}
    "1: :->cmds" \
    "*::arg:->args"
  case "$state" in
    cmds)
      _values {{q (printf "%s command" .Bin)}} \
{{.ZshCommandValues}}      ;;
    args)
      case "$line[1]" in
{{- range .Commands}}
        {{.CasePattern}}) _{{$.Bin}}_{{.Name}};;
{{- end}}
      esac
      ;;
  esac
}

{{- range .Commands}}
_{{$.Bin}}_{{.Name}}() {
  _arguments \
{{.ZshArgsBlock}}}

{{- end}}
`

const bashTemplate = `# bash completion for {{.Bin}}
_{{.Bin}}_complete() {
  local cur prev cmd
  COMPREPLY=()
  cur=${COMP_WORDS[COMP_CWORD]}
  prev=${COMP_WORDS[COMP_CWORD-1]}
  cmd=${COMP_WORDS[1]}

  if [[ ${COMP_CWORD} -eq 1 ]]; then
    COMPREPLY=( $(compgen -W {{q .BashCommands}} -- "$cur") )
    return 0
  fi

  case "$cmd" in
{{- range .Commands}}
    {{.CasePattern}})
{{- if .DynamicTopic}}
      if [[ ${COMP_CWORD} -eq 2 ]]; then
        COMPREPLY=( $(compgen -W "$({{$.Bin}} {{$.Helper}} {{.DynamicTopic}})" -- "$cur") )
        return 0
      fi
{{- end}}
{{- if .BashFlags}}
      COMPREPLY=( $(compgen -W {{q .BashFlags}} -- "$cur") )
      return 0
{{- end}}
      ;;
{{- end}}
  esac
}
complete -F _{{.Bin}}_complete {{.Bin}}
`
