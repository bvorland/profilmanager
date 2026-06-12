package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/runner"
	"github.com/bvorland/profilmanager/internal/tui"
	"github.com/bvorland/profilmanager/internal/wizard"
	"github.com/charmbracelet/lipgloss"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type profileNewOpts struct {
	From       string
	NoTemplate bool
	NoLogin    bool
	Apply      bool
	NoApply    bool
}

var profileNewLoginRunner = runProfileNewLogin

type wizardReader struct {
	buf *bufio.Reader
}

func newWizardReader(in io.Reader, interactive bool) *wizardReader {
	_ = interactive
	return &wizardReader{buf: bufio.NewReader(in)}
}

func (w *wizardReader) Close() error {
	return nil
}

func (w *wizardReader) Prompt(out io.Writer, label string) (string, error) {
	fmt.Fprint(out, label)
	line, err := w.buf.ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}

func newProfileNewCmd() *cobra.Command {
	var opts profileNewOpts
	cmd := &cobra.Command{
		Use:   "new [name]",
		Short: "Create a new profile with an interactive wizard",
		Long: `Create a new profile using the shared wizard step model.

With <name>, the name step is pre-filled. With --from <template>, fields are
copied from an existing profile and the wizard jumps to the first missing
field. Use ` + "`profile add`" + ` for non-interactive scripted creation.`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return emitError(cmd, errInvalidUsage("pm profile new requires an interactive terminal; use 'pm profile add' for non-interactive scripted creation"))
			}
			return emitError(cmd, runProfileNewWizard(cmd.OutOrStdout(), cmd.ErrOrStderr(), os.Stdin, args, opts, true))
		},
		ValidArgsFunction: profileNameCompletions,
	}
	cmd.Flags().StringVar(&opts.From, "from", "", "copy reusable fields from an existing profile")
	cmd.Flags().BoolVar(&opts.NoTemplate, "no-template", false, "do not offer or apply template copying")
	cmd.Flags().BoolVar(&opts.NoLogin, "no-login", false, "skip the first-time Azure sign-in prompt")
	cmd.Flags().BoolVar(&opts.Apply, "apply", false, "apply the new profile to the current shell after save")
	cmd.Flags().BoolVar(&opts.NoApply, "no-apply", false, "do not offer to apply the new profile after save")
	return cmd
}

func runProfileNewWizard(out, errOut io.Writer, in io.Reader, args []string, opts profileNewOpts, interactive bool) error {
	if opts.From != "" && len(args) == 0 {
		return errInvalidUsage("--from requires a new profile name")
	}
	if opts.Apply && opts.NoApply {
		return errInvalidUsage("--apply and --no-apply are mutually exclusive")
	}
	reader := newWizardReader(in, interactive)
	defer reader.Close()
	if interactive {
		fmt.Fprintln(errOut, styleDim.Render("Tip: paste with Ctrl+V, Shift+Insert, or right-click (if your terminal is set up for it)."))
	}
	state := &wizard.State{}
	prefilled := map[string]bool{}
	templated := false

	if len(args) > 0 {
		name := strings.TrimSpace(args[0])
		if err := core.ValidateName(name); err != nil {
			return errInvalidUsage("%v", err)
		}
		if opts.From != "" {
			loaded, err := wizard.LoadTemplate(opts.From, name)
			if err != nil {
				return err
			}
			state = loaded
			templated = true
		} else {
			state.Name = name
			prefilled["name"] = true
		}
	}

	steps := wizard.Steps()
	for i, step := range steps {
		if profileNewShouldSkip(step, state) || prefilled[step.ID()] || (templated && stepHasValue(state, step.ID())) {
			continue
		}
		for {
			printStepPrompt(out, i+1, len(steps), step, state)
			line, err := reader.Prompt(out, "> ")
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}
			input := strings.TrimSpace(line)
			if input == "" {
				input = step.Default(state)
			}
			if err := step.Validate(input); err != nil {
				fmt.Fprintln(out, styleError.Render("✗ "+err.Error()))
				if errors.Is(err, io.EOF) {
					return err
				}
				continue
			}
			step.Apply(state, input)
			if step.ID() == "name" && !opts.NoTemplate {
				loaded, copied, err := maybeLoadTemplate(out, reader, state.Name)
				if err != nil {
					return err
				}
				if copied {
					state = loaded
					templated = true
				}
			}
			if step.ID() == "color" {
				printColorPreview(out, state.Color)
			}
			break
		}
	}

	profile, err := wizard.Build(state)
	if err != nil {
		return err
	}
	path, err := core.ProfilePath(profile.Name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return errInvalidUsage("profile %q already exists at %s", profile.Name, path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	body, err := toml.Marshal(profile)
	if err != nil {
		return fmt.Errorf("marshal profile preview: %w", err)
	}
	printPreview(out, body)
	if ok, err := confirm(out, reader, "Save? [Y/n] ", true); err != nil {
		return err
	} else if !ok {
		fmt.Fprintln(out, styleDim.Render("aborted"))
		return nil
	}
	if err := profile.Save(path); err != nil {
		return err
	}
	fmt.Fprintln(out, styleOK.Render("✓ Saved profile to"), path)
	if err := maybePromptFirstLogin(out, reader, profile, state, opts); err != nil {
		fmt.Fprintln(out, styleError.Render("✗ "+err.Error()))
	}
	if err := maybePromptProfileApply(out, errOut, reader, profile.Name, opts, interactive); err != nil {
		return err
	}
	return nil
}

func maybePromptProfileApply(out, errOut io.Writer, reader *wizardReader, name string, opts profileNewOpts, interactive bool) error {
	if opts.Apply && opts.NoApply {
		return errInvalidUsage("--apply and --no-apply are mutually exclusive")
	}
	apply := false
	switch {
	case opts.Apply:
		apply = true
	case opts.NoApply:
		apply = false
	case interactive:
		fmt.Fprintln(errOut)
		fmt.Fprintln(errOut, "Apply this profile to your current shell now?")
		fmt.Fprintln(errOut, "  (loads env vars so az, gh, kubectl, and copilot see this profile)")
		fmt.Fprintln(errOut)
		fmt.Fprintln(errOut, "  [Y] Yes, apply now")
		fmt.Fprintln(errOut, "  [n] No, I'll apply later")
		fmt.Fprintln(errOut)
		line, err := reader.Prompt(errOut, "> ")
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		choice := strings.ToLower(strings.TrimSpace(line))
		apply = choice == "" || choice == "y" || choice == "yes"
	default:
		return nil
	}

	if !apply {
		fmt.Fprintf(errOut, "Apply later with: pm env apply %s | Invoke-Expression\n", name)
		return nil
	}
	if os.Getenv("PM_SHELL_INIT_LOADED") != "" {
		fmt.Fprintln(errOut, "✓ Applying to current shell...")
		fmt.Fprintf(out, "##pm-apply:%s\n", name)
		return nil
	}
	printProfileApplyFallbackBanner(errOut, name)
	return nil
}

func printProfileApplyFallbackBanner(errOut io.Writer, name string) {
	top, mid, bottom, side := "╔", "═", "╚", "║"
	topRight, bottomRight := "╗", "╝"
	if !colorsOn {
		top, mid, bottom, side = "+", "-", "+", "|"
		topRight, bottomRight = "+", "+"
	}
	line := strings.Repeat(mid, 68)
	headline := styleWarn.Render("⚠️   Profile saved — but NOT applied to your current shell.")
	fmt.Fprintln(errOut)
	fmt.Fprintf(errOut, "%s%s%s\n", top, line, topRight)
	fmt.Fprintf(errOut, "%s  %s       %s\n", side, headline, side)
	fmt.Fprintf(errOut, "%s%s%s\n", bottom, line, bottomRight)
	fmt.Fprintln(errOut)
	fmt.Fprintln(errOut, "  Your new environment vars are NOT loaded yet. Tools (az, gh, kubectl,")
	fmt.Fprintln(errOut, "  copilot, ...) will still see the host config.")
	fmt.Fprintln(errOut)
	fmt.Fprintln(errOut, "  To apply NOW (one-time):")
	fmt.Fprintf(errOut, "      pm env apply %s | Invoke-Expression\n", name)
	fmt.Fprintln(errOut)
	fmt.Fprintln(errOut, "  To make this automatic for every new profile, add to your $PROFILE:")
	fmt.Fprintln(errOut, "      pm shell-init pwsh | Out-String | Invoke-Expression")
	fmt.Fprintln(errOut, "  Then reload:  . $PROFILE   (or open a fresh pwsh tab)")
	fmt.Fprintln(errOut)
}

func maybePromptFirstLogin(out io.Writer, reader *wizardReader, profile *core.Profile, state *wizard.State, opts profileNewOpts) error {
	if opts.NoLogin || (strings.TrimSpace(state.Tenant) == "" && strings.TrimSpace(state.Subscription) == "") {
		return nil
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Run first-time sign-in now?")
	fmt.Fprintf(out, "  This is equivalent to: pm exec %s -- az login --use-device-code\n", profile.Name)
	fmt.Fprintln(out, "  Skip if you'll do it later, or if this profile isn't an Azure profile.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  [Y] Yes, run az login (device code flow)")
	fmt.Fprintln(out, "  [a] Yes, run azd auth login (Azure Developer CLI too)")
	fmt.Fprintln(out, "  [n] No, skip")
	fmt.Fprintln(out)

	line, err := reader.Prompt(out, "> ")
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	choice := strings.ToLower(strings.TrimSpace(line))
	if choice == "n" || choice == "no" {
		fmt.Fprintf(out, "Skipping. Run later with: pm exec %s -- az login --use-device-code\n", profile.Name)
		return nil
	}

	command := "az"
	args := []string{"login", "--use-device-code"}
	loginLabel := "az login"
	retry := fmt.Sprintf("pm exec %s -- az login --use-device-code", profile.Name)
	if choice == "a" {
		command = "azd"
		args = []string{"auth", "login"}
		loginLabel = "azd auth login"
		retry = fmt.Sprintf("pm exec %s -- azd auth login", profile.Name)
	}

	code, runErr := profileNewLoginRunner(profile, command, args, os.Stdin, out, os.Stderr)
	if runErr == nil {
		fmt.Fprintf(out, "✓ Signed in to %s\n", profile.Name)
		return nil
	}
	if code < 0 {
		code = 1
	}
	fmt.Fprintf(out, "✗ %s failed (exit %d). You can retry with: %s\n", loginLabel, code, retry)
	return nil
}

func runProfileNewLogin(profile *core.Profile, command string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	plan, cleanup, err := runner.Compose(context.Background(), profile, runner.ComposeOpts{ResolveSecrets: true})
	if err != nil {
		return 1, err
	}
	defer cleanup()

	exe, err := exec.LookPath(command)
	if err != nil {
		return 1, err
	}
	cmd := exec.CommandContext(context.Background(), exe, args...)
	cmd.Env = runner.EnvSlice(os.Environ(), plan.Env)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), err
		}
		return 1, err
	}
	return 0, nil
}

func profileNewShouldSkip(step wizard.Step, state *wizard.State) bool {
	if step.ID() == "label" {
		return false
	}
	return step.Skippable(state)
}

func printStepPrompt(out io.Writer, n, total int, step wizard.Step, state *wizard.State) {
	fmt.Fprintf(out, "%s %s\n", styleBold.Render(fmt.Sprintf("[step %d/%d]", n, total)), step.Title())
	fmt.Fprintln(out, styleDim.Render(step.Help()))
	if def := step.Default(state); def != "" {
		fmt.Fprintf(out, "%s %s\n", styleDim.Render("Default:"), def)
	}
	fmt.Fprint(out, "> ")
}

func maybeLoadTemplate(out io.Writer, reader *wizardReader, name string) (*wizard.State, bool, error) {
	templates := core.SuggestTemplates(name)
	if len(templates) == 0 {
		return nil, false, nil
	}
	if ok, err := confirm(out, reader, "Copy fields from an existing profile? [y/N] ", false); err != nil {
		return nil, false, err
	} else if !ok {
		return nil, false, nil
	}
	for i, name := range templates {
		fmt.Fprintf(out, "  %d) %s\n", i+1, name)
	}
	for {
		line, err := reader.Prompt(out, "Template number: ")
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, false, err
		}
		n, convErr := strconv.Atoi(strings.TrimSpace(line))
		if convErr == nil && n >= 1 && n <= len(templates) {
			state, err := wizard.LoadTemplate(templates[n-1], name)
			return state, err == nil, err
		}
		fmt.Fprintln(out, styleError.Render("✗ choose a listed template number"))
		if errors.Is(err, io.EOF) {
			return nil, false, err
		}
	}
}

func confirm(out io.Writer, reader *wizardReader, prompt string, defaultYes bool) (bool, error) {
	line, err := reader.Prompt(out, prompt)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	if ans == "" {
		return defaultYes, nil
	}
	switch ans {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return false, nil
	}
}

func printPreview(out io.Writer, body []byte) {
	border := strings.Repeat("-", 72)
	fmt.Fprintln(out, border)
	fmt.Fprint(out, string(body))
	if len(body) == 0 || body[len(body)-1] != '\n' {
		fmt.Fprintln(out)
	}
	fmt.Fprintln(out, border)
}

func printColorPreview(out io.Writer, color string) {
	swatch := lipgloss.NewStyle().Foreground(tui.ProfileColor(color)).Render("████")
	fmt.Fprintf(out, "Preview: %s %s\n", swatch, color)
}

func stepHasValue(s *wizard.State, id string) bool {
	switch id {
	case "name":
		return s.Name != ""
	case "label":
		return s.Label != ""
	case "color":
		return s.Color != ""
	case "preset":
		return s.Preset != ""
	case "tenant":
		return s.Tenant != ""
	case "subscription":
		return s.Subscription != ""
	case "azure_config_dir":
		return s.AzureConfigDir != ""
	case "azd_config_dir":
		return s.AzdConfigDir != ""
	case "gh_account":
		return s.GhAccount != ""
	case "gh_host":
		return s.GhHost != ""
	case "kube_context":
		return s.KubeContext != ""
	case "kube_namespace":
		return s.KubeNamespace != ""
	case "git_author":
		return s.GitAuthor != ""
	case "git_email":
		return s.GitEmail != ""
	default:
		return false
	}
}
