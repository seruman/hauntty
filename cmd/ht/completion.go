package main

import (
	"fmt"

	"code.selman.me/hauntty/internal/config"
)

type CompletionCmd struct {
	Shell string `arg:"" enum:"bash,zsh,fish" help:"Shell type (bash, zsh, fish)."`
}

func (cmd *CompletionCmd) Run(_ *config.Config) error {
	switch cmd.Shell {
	case "bash":
		fmt.Print(completionBash)
	case "zsh":
		fmt.Print(completionZsh)
	case "fish":
		fmt.Print(completionFish)
	}
	return nil
}

const completionBash = `_ht_completions() {
	local cur="${COMP_WORDS[COMP_CWORD]}"
	local subcmd=""
	for ((i=1; i<COMP_CWORD; i++)); do
		case "${COMP_WORDS[i]}" in
			--socket) ((i++));;
			-*) ;;
			*) subcmd="${COMP_WORDS[i]}"; break;;
		esac
	done
	if [[ -z "$subcmd" ]]; then
		COMPREPLY=($(compgen -W "attach list kill send dump detach wait status prune init config daemon" -- "$cur"))
		return
	fi
	local argpos=0
	for ((j=i+1; j<COMP_CWORD; j++)); do
		case "${COMP_WORDS[j]}" in
			-*) ;;
			*) ((argpos++));;
		esac
	done
	if [[ $argpos -eq 0 ]]; then
		case "$subcmd" in
			attach|a|kill|send|dump|wait)
				local sessions
				sessions=$(ht list -a 2>/dev/null | tail -n +2 | awk '{print $1}')
				COMPREPLY=($(compgen -W "$sessions" -- "$cur"))
				;;
		esac
	fi
}
complete -o default -F _ht_completions ht
`

const completionZsh = `#compdef ht

_ht_sessions() {
	local output
	output=$(ht list -a 2>/dev/null | tail -n +2 | awk '{print $1}')
	[[ -n "$output" ]] && compadd ${(f)output}
}

_ht() {
	local curcontext=$curcontext state line
	typeset -A opt_args
	local -a commands=(
		'attach:Attach to a session'
		'a:Attach to a session'
		'list:List sessions'
		'ls:List sessions'
		'kill:Kill a session'
		'send:Send input to a session'
		'dump:Dump session contents'
		'detach:Detach from current session'
		'wait:Wait for output match'
		'status:Show status'
		'st:Show status'
		'prune:Delete dead sessions'
		'init:Create default config'
		'config:Print configuration'
		'daemon:Start daemon'
	)
	_arguments -C \
		'--version[Print version]' \
		'--socket=[Unix socket path]:path:_files' \
		'1:command:->cmd' \
		'*::arg:->args'
	case $state in
		cmd)
			_describe 'command' commands
			;;
		args)
			(( CURRENT == 2 )) || return
			case $line[1] in
				attach|a|kill|send|dump|wait)
					_ht_sessions
					;;
			esac
			;;
	esac
}

_ht
`

const completionFish = `function __ht_sessions
	ht list -a 2>/dev/null | tail -n +2 | awk '{print $1}'
end

complete -c ht -f
complete -c ht -n __fish_use_subcommand -a attach -d 'Attach to a session'
complete -c ht -n __fish_use_subcommand -a a -d 'Attach to a session'
complete -c ht -n __fish_use_subcommand -a list -d 'List sessions'
complete -c ht -n __fish_use_subcommand -a ls -d 'List sessions'
complete -c ht -n __fish_use_subcommand -a kill -d 'Kill a session'
complete -c ht -n __fish_use_subcommand -a send -d 'Send input to a session'
complete -c ht -n __fish_use_subcommand -a dump -d 'Dump session contents'
complete -c ht -n __fish_use_subcommand -a detach -d 'Detach from current session'
complete -c ht -n __fish_use_subcommand -a wait -d 'Wait for output match'
complete -c ht -n __fish_use_subcommand -a status -d 'Show status'
complete -c ht -n __fish_use_subcommand -a st -d 'Show status'
complete -c ht -n __fish_use_subcommand -a prune -d 'Delete dead sessions'
complete -c ht -n __fish_use_subcommand -a init -d 'Create default config'
complete -c ht -n __fish_use_subcommand -a config -d 'Print configuration'
complete -c ht -n __fish_use_subcommand -a daemon -d 'Start daemon'
complete -c ht -n '__fish_seen_subcommand_from attach a' -a '(__ht_sessions)'
complete -c ht -n '__fish_seen_subcommand_from kill' -a '(__ht_sessions)'
complete -c ht -n '__fish_seen_subcommand_from send' -a '(__ht_sessions)'
complete -c ht -n '__fish_seen_subcommand_from dump' -a '(__ht_sessions)'
complete -c ht -n '__fish_seen_subcommand_from wait' -a '(__ht_sessions)'
`
