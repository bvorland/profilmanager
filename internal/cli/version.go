package cli

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// Build-time injected metadata. Release builds override these via:
//
//	go build -ldflags "\
//	  -X github.com/bvorland/profilmanager/internal/cli.Version=v1.2.3 \
//	  -X github.com/bvorland/profilmanager/internal/cli.Commit=abcdef0 \
//	  -X github.com/bvorland/profilmanager/internal/cli.Date=2026-01-02T03:04:05Z"
//
// When unset (typical `go build` / `go install`), we fall back to
// debug.ReadBuildInfo for VCS metadata so local builds still print
// something useful.
var (
	Version = "0.0.0-dev"
	Commit  = ""
	Date    = ""
)

// buildInfo collects the bits we want to expose in `pm version` output.
type buildInfo struct {
	Version  string
	Commit   string
	Modified bool
	Date     string
	GoVer    string
	OS       string
	Arch     string
}

func collectBuildInfo() buildInfo {
	bi := buildInfo{
		Version: Version,
		Commit:  Commit,
		Date:    Date,
		GoVer:   runtime.Version(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return bi
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if bi.Commit == "" {
				bi.Commit = s.Value
			}
		case "vcs.modified":
			bi.Modified = s.Value == "true"
		case "vcs.time":
			if bi.Date == "" {
				bi.Date = s.Value
			}
		}
	}
	return bi
}

func (b buildInfo) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "pm %s", b.Version)
	if b.Commit != "" {
		short := b.Commit
		if len(short) > 12 {
			short = short[:12]
		}
		fmt.Fprintf(&sb, " (%s", short)
		if b.Modified {
			sb.WriteString("-dirty")
		}
		sb.WriteString(")")
	}
	fmt.Fprintf(&sb, " %s %s/%s", b.GoVer, b.OS, b.Arch)
	if b.Date != "" {
		fmt.Fprintf(&sb, " built %s", b.Date)
	}
	return sb.String()
}

// versionTemplate is used by the implicit `--version` flag Cobra renders.
func versionTemplate() string {
	return collectBuildInfo().String() + "\n"
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the pm version and build metadata",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), collectBuildInfo().String())
			return nil
		},
	}
}
