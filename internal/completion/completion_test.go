package completion

import (
	"strings"
	"testing"
)

func TestScriptSupportedShells(t *testing.T) {
	tests := []struct {
		shell string
		want  []string
	}{
		{"bash", []string{"complete -F _opstack_doctor_completion opstack-doctor", "validate check export demo fixture generate completion version help", "--config --output --fail-on"}},
		{"zsh", []string{"#compdef opstack-doctor", "completion_shells", "validate_flags"}},
		{"fish", []string{"complete -c opstack-doctor", "__fish_seen_subcommand_from validate", "bash zsh fish"}},
	}
	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			data, err := Script(tt.shell)
			if err != nil {
				t.Fatalf("Script(%q) error = %v", tt.shell, err)
			}
			got := string(data)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Fatalf("Script(%q) missing %q:\n%s", tt.shell, want, got)
				}
			}
		})
	}
}

func TestScriptUnsupportedShell(t *testing.T) {
	if _, err := Script("powershell"); err == nil {
		t.Fatalf("Script() should reject unsupported shells")
	}
}
