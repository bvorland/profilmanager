package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// shimTools is the v1 set of tools pm shell-init knows how to wrap. Each
// becomes a shell function that routes through `pm exec --profile ...`
// when a profile is active, and falls through to the real binary when
// not. Keep this list small and explicit.
var shimTools = []string{"az", "azd", "gh", "kubectl", "git"}

func newShellInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shell-init [bash|zsh|pwsh|fish]",
		Short: "Emit shell init code (session ID + optional tool shims)",
		Long: `Emit shell init code to be sourced from a shell rc.

The emitted code:
  1. Always: sets PM_SESSION_ID (so pm doctor doesn't fall back to PPID).
  2. With --with-shims: defines wrapper functions for az / azd / gh /
     kubectl / git that route through ` + "`pm exec --profile <active>`" + ` when
     a profile is active and fall through to the real tool when not.

Install (one-time):

  bash / zsh   →  add  ` + "`eval \"$(pm shell-init --shell bash)\"`" + `  to ~/.bashrc / ~/.zshrc
  fish         →  add  ` + "`pm shell-init --shell fish | source`" + `        to ~/.config/fish/config.fish
  PowerShell   →  add  ` + "`pm shell-init --shell pwsh | Invoke-Expression`" + `  to $PROFILE

Shims are OPT-IN by design: silently rerouting raw ` + "`az`" + ` calls would be
surprising. Operators must consent by passing --with-shims.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runShellInit,
	}
	cmd.Flags().String("shell", "", "shell flavor: bash|zsh|pwsh|fish (auto-detected if empty)")
	cmd.Flags().Bool("with-shims", false, "also install az/azd/gh/kubectl/git wrapper functions")
	return cmd
}

func runShellInit(cmd *cobra.Command, args []string) error {
	shell, _ := cmd.Flags().GetString("shell")
	shell = strings.ToLower(strings.TrimSpace(shell))
	if len(args) == 1 {
		argShell := strings.ToLower(strings.TrimSpace(args[0]))
		if shell != "" && shell != argShell {
			return emitError(cmd, errInvalidUsage("pass shell either positionally or with --shell, not both with different values"))
		}
		shell = argShell
	}
	if shell == "" {
		shell = detectShell()
	}
	withShims, _ := cmd.Flags().GetBool("with-shims")

	id, err := newSessionID()
	if err != nil {
		return emitError(cmd, err)
	}

	out := cmd.OutOrStdout()
	switch shell {
	case "bash", "zsh":
		fmt.Fprintln(out, "# pm shell-init (bash/zsh) — generated; do not edit by hand")
		fmt.Fprintln(out, "if [ -z \"${PM_SESSION_ID:-}\" ]; then")
		line, _ := sessionInitLine("bash", id)
		fmt.Fprintln(out, "  "+line)
		fmt.Fprintln(out, "fi")
		if withShims {
			fmt.Fprintln(out)
			for _, tool := range shimTools {
				fmt.Fprintln(out, bashShim(tool))
			}
		}
	case "fish":
		fmt.Fprintln(out, "# pm shell-init (fish) — generated; do not edit by hand")
		line, _ := sessionInitLine("fish", id)
		fmt.Fprintln(out, "if not set -q PM_SESSION_ID")
		fmt.Fprintln(out, "  "+line)
		fmt.Fprintln(out, "end")
		if withShims {
			fmt.Fprintln(out)
			for _, tool := range shimTools {
				fmt.Fprintln(out, fishShim(tool))
			}
		}
	case "pwsh", "powershell":
		fmt.Fprintln(out, "# pm shell-init (pwsh) — generated; do not edit by hand")
		line, _ := sessionInitLine("pwsh", id)
		fmt.Fprintln(out, "if (-not $env:PM_SESSION_ID) {")
		fmt.Fprintln(out, "  "+line)
		fmt.Fprintln(out, "}")
		fmt.Fprintln(out)
		fmt.Fprintln(out, pwshPMWrapper())
		fmt.Fprintln(out)
		fmt.Fprintln(out, "# >>> pm completion >>>")
		fmt.Fprintln(out, "if (Get-Command pm -ErrorAction SilentlyContinue) {")
		fmt.Fprintln(out, "  (& pm completion pwsh | Out-String) | Invoke-Expression")
		fmt.Fprintln(out, "}")
		fmt.Fprintln(out, "# <<< pm completion <<<")
		if withShims {
			fmt.Fprintln(out)
			for _, tool := range shimTools {
				fmt.Fprintln(out, pwshShim(tool))
			}
		}
	default:
		return emitError(cmd, errInvalidUsage("unsupported shell %q (want one of: bash, zsh, pwsh, fish)", shell))
	}
	return nil
}

func pwshPMWrapper() string {
	return strings.Join([]string{
		"# === pm console encoding ===",
		"# Make sure pipes between pwsh and pm.exe are UTF-8 in both directions.",
		"# (pm.exe also calls SetConsoleOutputCP(65001) on Windows for non-pwsh",
		"# invocations, but $OutputEncoding governs pwsh's piping.)",
		"try {",
		"    if ([Console]::OutputEncoding.CodePage -ne 65001) {",
		"        [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new()",
		"    }",
		"    if ($OutputEncoding.CodePage -ne 65001) {",
		"        $OutputEncoding = [System.Text.UTF8Encoding]::new()",
		"    }",
		"} catch {}",
		"# === end pm console encoding ===",
		"",
		"# === pm shell integration ===",
		"$env:PM_SHELL_INIT_LOADED = '1'",
		"",
		"function global:pm {",
		"    $exe = (Get-Command -Name 'pm.exe' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1).Path",
		"    if (-not $exe) {",
		"        Write-Error 'pm.exe not found in PATH'",
		"        return",
		"    }",
		"",
		"    # Intercept \"pm env apply ...\" — auto-pipe through Invoke-Expression so the",
		"    # generated script lands in the calling shell. Skip when flags would change",
		"    # the output (--help, -h, --shell <non-pwsh>), letting users still ask for",
		"    # help or generate non-pwsh scripts.",
		"    if ($args.Count -ge 2 -and $args[0] -eq 'env' -and $args[1] -eq 'apply') {",
		"        $autoApply = $true",
		"        for ($i = 2; $i -lt $args.Count; $i++) {",
		"            $a = $args[$i]",
		"            if ($a -eq '-h' -or $a -eq '--help') { $autoApply = $false; break }",
		"            if ($a -eq '--shell') {",
		"                if ($i + 1 -lt $args.Count) {",
		"                    $shellVal = $args[$i + 1]",
		"                    if ($shellVal -ne 'pwsh' -and $shellVal -ne 'powershell') {",
		"                        $autoApply = $false; break",
		"                    }",
		"                }",
		"            }",
		"        }",
		"        if ($autoApply) {",
		"            & $exe @args | Invoke-Expression",
		"            $global:LASTEXITCODE = $LASTEXITCODE",
		"            return",
		"        }",
		"    }",
		"",
		"    # Intercept only \"pm profile new ...\" to watch for the apply marker",
		"    $intercept = ($args.Count -ge 2 -and $args[0] -eq 'profile' -and $args[1] -eq 'new')",
		"",
		"    if (-not $intercept) {",
		"        & $exe @args",
		"        return",
		"    }",
		"",
		"    $applyName = $null",
		"    & $exe @args | ForEach-Object {",
		"        if ($_ -is [string] -and $_ -match '^##pm-apply:(.+)$') {",
		"            $applyName = $matches[1].Trim()",
		"        } else {",
		"            $_",
		"        }",
		"    }",
		"    $exitCode = $LASTEXITCODE",
		"",
		"    if ($applyName -and $exitCode -eq 0) {",
		"        # Auto-apply via pm env apply in the calling shell.",
		"        & $exe env apply $applyName | Invoke-Expression",
		"        Write-Host \"\"",
		"        Write-Host \"$([char]0x2713) Profile '$applyName' applied to current shell\" -ForegroundColor Green",
		"    }",
		"",
		"    $global:LASTEXITCODE = $exitCode",
		"}",
		"# === end pm shell integration ===",
	}, "\n")
}

// bashShim renders a bash/zsh function for `tool`. Uses `command tool ...`
// for the fallthrough to bypass any shim recursion.
func bashShim(tool string) string {
	return strings.Join([]string{
		tool + "() {",
		"  local __pm_profile",
		`  __pm_profile="$(command pm whoami --profile-name 2>/dev/null)"`,
		`  if [ -n "$__pm_profile" ]; then`,
		`    command pm exec --profile "$__pm_profile" -- ` + tool + ` "$@"`,
		"  else",
		"    command " + tool + ` "$@"`,
		"  fi",
		"}",
	}, "\n")
}

func fishShim(tool string) string {
	return strings.Join([]string{
		"function " + tool,
		"  set -l __pm_profile (command pm whoami --profile-name 2>/dev/null)",
		`  if test -n "$__pm_profile"`,
		"    command pm exec --profile $__pm_profile -- " + tool + " $argv",
		"  else",
		"    command " + tool + " $argv",
		"  end",
		"end",
	}, "\n")
}

// pwshShim renders a PowerShell function. Uses Get-Command -CommandType
// Application to resolve the real executable, bypassing function lookup
// (otherwise we recurse forever).
func pwshShim(tool string) string {
	return strings.Join([]string{
		"function " + tool + " {",
		"  $__pm_profile = (& pm whoami --profile-name 2>$null)",
		"  if ($__pm_profile) {",
		"    & pm exec --profile $__pm_profile -- " + tool + " @args",
		"  } else {",
		"    & (Get-Command -CommandType Application " + tool + " | Select-Object -First 1).Source @args",
		"  }",
		"}",
	}, "\n")
}
