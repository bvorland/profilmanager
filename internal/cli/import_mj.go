package cli

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/spf13/cobra"
)

// mjExportScript is the canonical exporter, embedded so the binary stays
// self-contained. The byte-for-byte twin lives at scripts/mj-export.ps1
// (for operator visibility and standalone debugging); import_mj_test.go
// fails the build if the two drift.
//
//go:embed mj-export.ps1
var mjExportScript []byte

// mjExportResult mirrors the JSON document mj-export.ps1 prints on stdout.
type mjExportResult struct {
	SourceScript string      `json:"source_script"`
	ProfilesDir  string      `json:"profiles_dir"`
	Profiles     []mjProfile `json:"profiles"`
	Error        string      `json:"error,omitempty"`
}

type mjProfile struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Color string `json:"color"`
}

// importSummary is the shape `pm import-mj --json` prints on success.
type importSummary struct {
	Imported []string        `json:"imported"`
	Skipped  []importSkipped `json:"skipped"`
	Errors   []importError   `json:"errors"`
}

type importSkipped struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type importError struct {
	Name  string `json:"name,omitempty"`
	Error string `json:"error"`
}

type importMjOpts struct {
	FromPowerShell string
	ProfilesDir    string
	DryRun         bool
	Force          bool
	JSON           bool
}

func newImportMJCmd() *cobra.Command {
	var opts importMjOpts
	cmd := &cobra.Command{
		Use:   "import-mj",
		Short: "Import profiles from Majid Hajian's mj PowerShell CLI",
		Long: "Reads $script:ProfilesList from a PowerShell profile.ps1 (via a\n" +
			"sidecar exporter — we never parse PowerShell from Go) and the\n" +
			"per-profile .env files under ~/PSProfiles, then writes one TOML\n" +
			"per profile under the pm profiles dir.\n\n" +
			"Idempotent: skips profiles that already exist unless --force.\n" +
			"Secret values in .env files are written to disk only as TOML\n" +
			"entries — they are never logged or printed by this command.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runImportMJ(cmd.OutOrStdout(), cmd.ErrOrStderr(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.FromPowerShell, "from-powershell", "",
		"Path to profile.ps1 (default: auto-discover under $HOME)")
	cmd.Flags().StringVar(&opts.ProfilesDir, "profiles-dir", "",
		"Directory holding <name>.env files (default: exporter's profiles_dir)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false,
		"Print what would happen; write nothing")
	cmd.Flags().BoolVar(&opts.Force, "force", false,
		"Overwrite existing profile TOML files")
	cmd.Flags().BoolVar(&opts.JSON, "json", false,
		"Emit machine-readable JSON summary")
	return cmd
}

func runImportMJ(stdout, stderr io.Writer, opts importMjOpts) error {
	summary := importSummary{
		Imported: []string{},
		Skipped:  []importSkipped{},
		Errors:   []importError{},
	}

	psBin, err := findPowerShell()
	if err != nil {
		return err
	}

	result, err := runMJExport(psBin, opts.FromPowerShell)
	if err != nil {
		return err
	}
	if result.Error != "" && len(result.Profiles) == 0 {
		return fmt.Errorf("mj-export reported: %s", result.Error)
	}
	if result.Error != "" {
		fmt.Fprintf(stderr, "warning: mj-export: %s\n", result.Error)
	}

	profilesDir := opts.ProfilesDir
	if profilesDir == "" {
		profilesDir = result.ProfilesDir
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("locate user home: %w", err)
	}

	for _, mp := range result.Profiles {
		prof, perr := buildProfileFromMJ(mp, profilesDir, home)
		if perr != nil {
			summary.Errors = append(summary.Errors, importError{Name: mp.Name, Error: perr.Error()})
			if !opts.JSON {
				fmt.Fprintf(stderr, "error  %s: %v\n", mp.Name, perr)
			}
			continue
		}

		target, err := core.ProfilePath(prof.Name)
		if err != nil {
			summary.Errors = append(summary.Errors, importError{Name: prof.Name, Error: err.Error()})
			continue
		}

		exists := false
		if _, statErr := os.Stat(target); statErr == nil {
			exists = true
		} else if !errors.Is(statErr, os.ErrNotExist) {
			summary.Errors = append(summary.Errors, importError{
				Name: prof.Name, Error: fmt.Sprintf("stat %s: %v", target, statErr),
			})
			continue
		}

		if exists && !opts.Force {
			summary.Skipped = append(summary.Skipped, importSkipped{Name: prof.Name, Reason: "already exists"})
			if !opts.JSON {
				fmt.Fprintf(stdout, "skip            %s (already exists)\n", prof.Name)
			}
			continue
		}

		if opts.DryRun {
			action := "would create"
			if exists {
				action = "would overwrite"
			}
			if !opts.JSON {
				fmt.Fprintf(stdout, "%-15s %s -> %s\n", action, prof.Name, target)
			}
			summary.Imported = append(summary.Imported, prof.Name)
			continue
		}

		if err := prof.Save(target); err != nil {
			summary.Errors = append(summary.Errors, importError{Name: prof.Name, Error: err.Error()})
			if !opts.JSON {
				fmt.Fprintf(stderr, "error  %s: %v\n", prof.Name, err)
			}
			continue
		}
		summary.Imported = append(summary.Imported, prof.Name)
		if !opts.JSON {
			verb := "import"
			if exists {
				verb = "overwrite"
			}
			fmt.Fprintf(stdout, "%-15s %s -> %s\n", verb, prof.Name, target)
		}
	}

	if opts.JSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(summary)
	}
	fmt.Fprintf(stdout, "\nimported: %d  skipped: %d  errors: %d\n",
		len(summary.Imported), len(summary.Skipped), len(summary.Errors))
	if opts.DryRun {
		fmt.Fprintln(stdout, "(dry-run: no files written)")
	}
	return nil
}

// findPowerShell resolves the PowerShell binary used to drive the exporter.
// Prefers cross-platform pwsh; on Windows we fall back to legacy powershell.
func findPowerShell() (string, error) {
	if p, err := exec.LookPath("pwsh"); err == nil {
		return p, nil
	}
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("powershell"); err == nil {
			return p, nil
		}
	}
	return "", errors.New("neither 'pwsh' nor 'powershell' found on PATH; install PowerShell 7+ or run on Windows")
}

// runMJExport materializes the embedded exporter to a temp file, invokes
// it with -NoProfile -NonInteractive, and unmarshals its stdout.
func runMJExport(psBin, profileScript string) (*mjExportResult, error) {
	tmpDir, err := os.MkdirTemp("", "pm-mj-export-*")
	if err != nil {
		return nil, fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	scriptPath := filepath.Join(tmpDir, "mj-export.ps1")
	if err := os.WriteFile(scriptPath, mjExportScript, 0o644); err != nil {
		return nil, fmt.Errorf("write script: %w", err)
	}

	args := []string{"-NoProfile", "-NonInteractive", "-File", scriptPath}
	if profileScript != "" {
		args = append(args, "-ProfileScript", profileScript)
	}

	cmd := exec.Command(psBin, args...)
	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	runErr := cmd.Run()

	out := strings.TrimSpace(stdoutBuf.String())
	var result mjExportResult
	if out != "" {
		if jerr := json.Unmarshal([]byte(out), &result); jerr != nil {
			return nil, fmt.Errorf("parse exporter JSON: %w (stderr: %s)", jerr, strings.TrimSpace(stderrBuf.String()))
		}
	}
	if runErr != nil {
		// Exit code 2 with a JSON body and an "error" field is the
		// exporter's contract for "soft failure" — return the body, let
		// the caller decide. Anything else is hard failure.
		if result.Error == "" {
			return &result, fmt.Errorf("exporter failed: %w (stderr: %s)", runErr, strings.TrimSpace(stderrBuf.String()))
		}
	}
	return &result, nil
}

// skipEnvKeys are env vars hoisted into the typed Azure/Azd structs;
// keeping them in .env means we'd duplicate the same fact in two places.
var skipEnvKeys = map[string]struct{}{
	"AZURE_PROFILE_NAME": {},
	"AZURE_CONFIG_DIR":   {},
	"AZD_CONFIG_DIR":     {},
}

// buildProfileFromMJ projects a single mj profile entry plus its .env file
// (if any) onto the v1 core.Profile model.
func buildProfileFromMJ(mp mjProfile, profilesDir, home string) (*core.Profile, error) {
	if err := core.ValidateName(mp.Name); err != nil {
		return nil, fmt.Errorf("invalid profile name %q: %w", mp.Name, err)
	}
	p := &core.Profile{
		Schema: core.SchemaVersion,
		Name:   mp.Name,
		Label:  mp.Label,
		Color:  mp.Color,
		Azure:  &core.AzureProfile{ConfigDir: filepath.Join(home, ".azure-"+mp.Name)},
		Azd:    &core.AzdProfile{ConfigDir: filepath.Join(home, ".azd-"+mp.Name)},
	}
	entries, err := readMJEnvFile(filepath.Join(profilesDir, mp.Name+".env"))
	if err != nil {
		return nil, err
	}
	p.Env = entries
	return p, nil
}

// readMJEnvFile parses a key=value file. It deliberately handles only the
// subset that `op run --env-file` accepts (which is what mj writes):
//   - blank lines and # comments are skipped
//   - the first '=' splits KEY and VALUE; nothing is trimmed from the key's
//     side beyond surrounding whitespace
//   - trailing whitespace on the value is removed (CRLF, tabs, spaces);
//     leading/inner whitespace is preserved
//   - values starting with op:// become EnvEntry.Ref; everything else is
//     EnvEntry.Value
//   - empty values are skipped silently (validator rejects them anyway)
//   - secret VALUES are never logged; we only return them as struct fields.
func readMJEnvFile(path string) ([]core.EnvEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read env %s: %w", path, err)
	}
	var out []core.EnvEntry
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		if key == "" {
			continue
		}
		if _, skip := skipEnvKeys[key]; skip {
			continue
		}
		val := strings.TrimRight(line[eq+1:], " \t")
		if val == "" {
			continue
		}
		if strings.HasPrefix(val, "op://") {
			out = append(out, core.EnvEntry{Key: key, Ref: val})
		} else {
			out = append(out, core.EnvEntry{Key: key, Value: val})
		}
	}
	return out, nil
}
