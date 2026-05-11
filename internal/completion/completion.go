package completion

import (
	"fmt"
	"strings"
)

type Shell string

const (
	ShellBash Shell = "bash"
	ShellZsh  Shell = "zsh"
	ShellFish Shell = "fish"
)

func Script(shell string) ([]byte, error) {
	switch Shell(strings.ToLower(strings.TrimSpace(shell))) {
	case ShellBash:
		return []byte(bashScript), nil
	case ShellZsh:
		return []byte(zshScript), nil
	case ShellFish:
		return []byte(fishScript), nil
	default:
		return nil, fmt.Errorf("unsupported shell %q: expected bash, zsh, or fish", shell)
	}
}

const bashScript = `# bash completion for opstack-doctor
_opstack_doctor_completion() {
  local cur prev cmd subcmd
  COMPREPLY=()
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"
  cmd="${COMP_WORDS[1]}"
  subcmd="${COMP_WORDS[2]}"

  case "$prev" in
    --output)
      case "$cmd" in
        check|demo) COMPREPLY=( $(compgen -W "human json prometheus" -- "$cur") ) ;;
        validate) COMPREPLY=( $(compgen -W "human json" -- "$cur") ) ;;
      esac
      return 0
      ;;
    --fail-on)
      case "$cmd" in
        validate) COMPREPLY=( $(compgen -W "fail warn none" -- "$cur") ) ;;
        check|demo) COMPREPLY=( $(compgen -W "fail warn" -- "$cur") ) ;;
      esac
      return 0
      ;;
    --scenario)
      COMPREPLY=( $(compgen -W "healthy warn fail" -- "$cur") )
      return 0
      ;;
  esac

  if [[ "$cur" == -* ]]; then
    case "$cmd" in
      validate) COMPREPLY=( $(compgen -W "--config --output --fail-on" -- "$cur") ) ;;
      check) COMPREPLY=( $(compgen -W "--config --output --fail-on" -- "$cur") ) ;;
      demo) COMPREPLY=( $(compgen -W "--scenario --output --fail-on" -- "$cur") ) ;;
      export) [[ "$subcmd" == "metrics" ]] && COMPREPLY=( $(compgen -W "--config" -- "$cur") ) ;;
      generate) [[ "$subcmd" == "alerts" || "$subcmd" == "runbook" ]] && COMPREPLY=( $(compgen -W "--config --out" -- "$cur") ) ;;
    esac
    return 0
  fi

  case "$COMP_CWORD" in
    1)
      COMPREPLY=( $(compgen -W "validate check export demo generate completion version help" -- "$cur") )
      ;;
    2)
      case "$cmd" in
        export) COMPREPLY=( $(compgen -W "metrics" -- "$cur") ) ;;
        generate) COMPREPLY=( $(compgen -W "alerts runbook" -- "$cur") ) ;;
        completion) COMPREPLY=( $(compgen -W "bash zsh fish" -- "$cur") ) ;;
      esac
      ;;
  esac
}

complete -F _opstack_doctor_completion opstack-doctor
`

const zshScript = `#compdef opstack-doctor

_opstack_doctor() {
  local -a commands validate_flags check_flags demo_flags export_subcommands generate_subcommands completion_shells
  commands=(
    'validate:validate config and topology intent offline'
    'check:run read-only live diagnostics'
    'export:emit scrapeable exports'
    'demo:run mocked local scenarios'
    'generate:generate alerts or runbooks'
    'completion:generate shell completion'
    'version:print version'
    'help:show help'
  )
  validate_flags=(
    '--config[path to doctor YAML config]:config file:_files'
    '--output[output format]:format:(human json)'
    '--fail-on[exit nonzero on severity]:severity:(fail warn none)'
  )
  check_flags=(
    '--config[path to doctor YAML config]:config file:_files'
    '--output[output format]:format:(human json prometheus)'
    '--fail-on[exit nonzero on severity]:severity:(fail warn)'
  )
  demo_flags=(
    '--scenario[demo scenario]:scenario:(healthy warn fail)'
    '--output[output format]:format:(human json prometheus)'
    '--fail-on[exit nonzero on severity]:severity:(fail warn)'
  )
  export_subcommands=('metrics:emit doctor findings as Prometheus text metrics')
  generate_subcommands=('alerts:generate Prometheus alert rules' 'runbook:generate Markdown runbook')
  completion_shells=('bash:bash completion' 'zsh:zsh completion' 'fish:fish completion')

  case ${words[2]} in
    validate)
      _arguments "${validate_flags[@]}"
      ;;
    check)
      _arguments "${check_flags[@]}"
      ;;
    demo)
      _arguments "${demo_flags[@]}"
      ;;
    export)
      if (( CURRENT == 3 )); then
        _describe 'export subcommand' export_subcommands
      elif [[ ${words[3]} == metrics ]]; then
        _arguments '--config[path to doctor YAML config]:config file:_files'
      fi
      ;;
    generate)
      if (( CURRENT == 3 )); then
        _describe 'generate subcommand' generate_subcommands
      elif [[ ${words[3]} == alerts || ${words[3]} == runbook ]]; then
        _arguments '--config[path to doctor YAML config]:config file:_files' '--out[path for generated output]:output file:_files'
      fi
      ;;
    completion)
      _describe 'shell' completion_shells
      ;;
    *)
      _describe 'command' commands
      ;;
  esac
}

_opstack_doctor "$@"
`

const fishScript = `# fish completion for opstack-doctor
complete -c opstack-doctor -f

complete -c opstack-doctor -n "__fish_use_subcommand" -a validate -d "Validate config and topology intent offline"
complete -c opstack-doctor -n "__fish_use_subcommand" -a check -d "Run read-only live diagnostics"
complete -c opstack-doctor -n "__fish_use_subcommand" -a export -d "Emit scrapeable exports"
complete -c opstack-doctor -n "__fish_use_subcommand" -a demo -d "Run mocked local scenarios"
complete -c opstack-doctor -n "__fish_use_subcommand" -a generate -d "Generate alerts or runbooks"
complete -c opstack-doctor -n "__fish_use_subcommand" -a completion -d "Generate shell completion"
complete -c opstack-doctor -n "__fish_use_subcommand" -a version -d "Print version"
complete -c opstack-doctor -n "__fish_use_subcommand" -a help -d "Show help"

complete -c opstack-doctor -n "__fish_seen_subcommand_from validate check" -l config -r -d "Path to doctor YAML config"
complete -c opstack-doctor -n "__fish_seen_subcommand_from validate" -l output -xa "human json" -d "Output format"
complete -c opstack-doctor -n "__fish_seen_subcommand_from validate" -l fail-on -xa "fail warn none" -d "Exit nonzero on severity"

complete -c opstack-doctor -n "__fish_seen_subcommand_from check" -l output -xa "human json prometheus" -d "Output format"
complete -c opstack-doctor -n "__fish_seen_subcommand_from check" -l fail-on -xa "fail warn" -d "Exit nonzero on severity"

complete -c opstack-doctor -n "__fish_seen_subcommand_from demo" -l scenario -xa "healthy warn fail" -d "Demo scenario"
complete -c opstack-doctor -n "__fish_seen_subcommand_from demo" -l output -xa "human json prometheus" -d "Output format"
complete -c opstack-doctor -n "__fish_seen_subcommand_from demo" -l fail-on -xa "fail warn" -d "Exit nonzero on severity"

complete -c opstack-doctor -n "__fish_seen_subcommand_from export; and not __fish_seen_subcommand_from metrics" -a metrics -d "Emit doctor findings as Prometheus text metrics"
complete -c opstack-doctor -n "__fish_seen_subcommand_from export; and __fish_seen_subcommand_from metrics" -l config -r -d "Path to doctor YAML config"

complete -c opstack-doctor -n "__fish_seen_subcommand_from generate; and not __fish_seen_subcommand_from alerts runbook" -a alerts -d "Generate Prometheus alert rules"
complete -c opstack-doctor -n "__fish_seen_subcommand_from generate; and not __fish_seen_subcommand_from alerts runbook" -a runbook -d "Generate Markdown runbook"
complete -c opstack-doctor -n "__fish_seen_subcommand_from generate; and __fish_seen_subcommand_from alerts runbook" -l config -r -d "Path to doctor YAML config"
complete -c opstack-doctor -n "__fish_seen_subcommand_from generate; and __fish_seen_subcommand_from alerts runbook" -l out -r -d "Path for generated output"

complete -c opstack-doctor -n "__fish_seen_subcommand_from completion" -xa "bash zsh fish" -d "Shell"
`
