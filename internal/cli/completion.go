package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/bvorland/profilmanager/internal/core"
)

func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion <bash|zsh|fish|pwsh>",
		Short: "Generate shell completion script",
		Long: `Generate a shell completion script for pm.

Source the output from your shell startup file, for example:

  bash:  source <(pm completion bash)
  zsh:   pm completion zsh > "${fpath[1]}/_pm"
  fish:  pm completion fish | source
  pwsh:  pm completion pwsh | Out-String | Invoke-Expression`,
		Args:              cobra.ExactArgs(1),
		ValidArgs:         []string{"bash", "zsh", "fish", "pwsh"},
		ValidArgsFunction: cobra.FixedCompletions([]string{"bash", "zsh", "fish", "pwsh"}, cobra.ShellCompDirectiveNoFileComp),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := cmd.Root()
			out := cmd.OutOrStdout()
			switch strings.ToLower(args[0]) {
			case "bash":
				return root.GenBashCompletion(out)
			case "zsh":
				return root.GenZshCompletion(out)
			case "fish":
				return root.GenFishCompletion(out, true)
			case "pwsh", "powershell":
				return root.GenPowerShellCompletion(out)
			default:
				return emitError(cmd, errInvalidUsage("unsupported shell %q (want one of: bash, zsh, fish, pwsh)", args[0]))
			}
		},
	}
	return cmd
}

func profileNameCompletions(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	items, _, err := core.ListProfiles()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	prefix := strings.ToLower(toComplete)
	names := make([]string, 0, len(items))
	for _, item := range items {
		name := item.Profile.Name
		if prefix == "" || strings.HasPrefix(strings.ToLower(name), prefix) {
			names = append(names, name)
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func execProfileCompletions(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return profileNameCompletions(cmd, args, toComplete)
}

func maxOneProfileCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return profileNameCompletions(cmd, args, toComplete)
}
