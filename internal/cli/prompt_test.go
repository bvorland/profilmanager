package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildPMSegment_HasExpectedShape(t *testing.T) {
	seg := buildPMSegment()

	if seg["type"] != "text" {
		t.Fatalf("type = %v, want text", seg["type"])
	}
	if seg["style"] != "powerline" {
		t.Fatalf("style = %v, want powerline", seg["style"])
	}
	props, ok := seg["properties"].(map[string]any)
	if !ok || props["pm_managed"] == nil {
		t.Fatalf("properties.pm_managed missing: %#v", seg["properties"])
	}
	if seg["background"] == nil {
		t.Fatalf("background missing")
	}
	if seg["background_templates"] == nil {
		t.Fatalf("background_templates missing")
	}
	template, ok := seg["template"].(string)
	if !ok {
		t.Fatalf("template has type %T", seg["template"])
	}
	for _, want := range []string{"PM_ACTIVE_PROFILE", "PM_ACTIVE_PROFILE_EMOJI"} {
		if !strings.Contains(template, want) {
			t.Fatalf("template missing %s: %s", want, template)
		}
	}

	bgTemplates, ok := seg["background_templates"].([]string)
	if !ok {
		t.Fatalf("background_templates has type %T", seg["background_templates"])
	}
	joined := strings.Join(bgTemplates, "\n")
	if !strings.Contains(joined, "PM_ACTIVE_PROFILE_BG") {
		t.Fatalf("background_templates missing PM_ACTIVE_PROFILE_BG: %v", bgTemplates)
	}
	if !strings.Contains(joined, "#c50f1f") {
		t.Fatalf("background_templates missing red no-profile fallback: %v", bgTemplates)
	}
}

func TestSegmentIsPMManaged(t *testing.T) {
	tests := []struct {
		name string
		seg  map[string]any
		want bool
	}{
		{name: "managed", seg: map[string]any{"properties": map[string]any{"pm_managed": "v0.8"}}, want: true},
		{name: "empty properties", seg: map[string]any{"properties": map[string]any{}}, want: false},
		{name: "no properties", seg: map[string]any{}, want: false},
		{name: "properties wrong type", seg: map[string]any{"properties": "wrong"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := segmentIsPMManaged(tt.seg); got != tt.want {
				t.Fatalf("segmentIsPMManaged() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractOMPConfigPath_CurrentRegex(t *testing.T) {
	tests := []struct {
		name    string
		profile string
		want    string
	}{
		{
			name:    "legacy init flags",
			profile: `oh-my-posh --init --shell pwsh --config C:\github\terminal\jandedobbeleer.json | Invoke-Expression`,
			want:    `C:\github\terminal\jandedobbeleer.json`,
		},
		{
			name:    "quoted path with space in current flag form",
			profile: `oh-my-posh --init --shell pwsh --config 'C:\Themes\my theme.json' | iex`,
			want:    `C:\Themes\my theme.json`,
		},
		{
			name:    "modern init syntax is not detected yet",
			profile: `oh-my-posh init pwsh --config 'C:\Themes\my theme.json' | iex`,
			want:    "",
		},
		{name: "no oh-my-posh", profile: "# no oh-my-posh here", want: ""},
		{name: "empty", profile: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractOMPConfigPath(tt.profile); got != tt.want {
				t.Fatalf("extractOMPConfigPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectOMPThemePathFromCandidates_Precedence(t *testing.T) {
	t.Run("POSH_THEME wins when set and file exists", func(t *testing.T) {
		dir := t.TempDir()
		poshTheme := filepath.Join(dir, "posh.json")
		writeFile(t, poshTheme, "{}")
		profileTheme := filepath.Join(dir, "profile.json")
		writeFile(t, profileTheme, "{}")
		profile := filepath.Join(dir, "Microsoft.PowerShell_profile.ps1")
		writeFile(t, profile, "oh-my-posh --init --shell pwsh --config "+profileTheme+" | iex")

		got, err := detectOMPThemePathFromCandidates(poshTheme, []string{profile})
		if err != nil {
			t.Fatalf("detectOMPThemePathFromCandidates: %v", err)
		}
		if got != poshTheme {
			t.Fatalf("got %q, want %q", got, poshTheme)
		}
	})

	t.Run("POSH_THEME missing falls through to profile scan", func(t *testing.T) {
		dir := t.TempDir()
		theme := filepath.Join(dir, "profile.json")
		writeFile(t, theme, "{}")
		profile := filepath.Join(dir, "Microsoft.PowerShell_profile.ps1")
		writeFile(t, profile, "oh-my-posh --init --shell pwsh --config "+theme+" | iex")

		got, err := detectOMPThemePathFromCandidates(filepath.Join(dir, "missing.json"), []string{profile})
		if err != nil {
			t.Fatalf("detectOMPThemePathFromCandidates: %v", err)
		}
		if got != theme {
			t.Fatalf("got %q, want %q", got, theme)
		}
	})

	t.Run("first profile candidate with config wins", func(t *testing.T) {
		dir := t.TempDir()
		theme := filepath.Join(dir, "second.json")
		writeFile(t, theme, "{}")
		first := filepath.Join(dir, "first.ps1")
		second := filepath.Join(dir, "second.ps1")
		writeFile(t, first, "# no oh-my-posh here")
		writeFile(t, second, "oh-my-posh --init --shell pwsh --config "+theme+" | iex")

		got, err := detectOMPThemePathFromCandidates("", []string{first, second})
		if err != nil {
			t.Fatalf("detectOMPThemePathFromCandidates: %v", err)
		}
		if got != theme {
			t.Fatalf("got %q, want %q", got, theme)
		}
	})

	t.Run("all empty returns actionable error", func(t *testing.T) {
		dir := t.TempDir()
		first := filepath.Join(dir, "first.ps1")
		second := filepath.Join(dir, "second.ps1")
		writeFile(t, first, "# no config")
		writeFile(t, second, "# no config either")

		_, err := detectOMPThemePathFromCandidates("", []string{first, second})
		if err == nil {
			t.Fatalf("expected error")
		}
		msg := err.Error()
		for _, want := range []string{"no oh-my-posh theme found", first, second, "--theme"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("error missing %q: %s", want, msg)
			}
		}
	})
}

func TestPatchTheme_InsertsAfterSession_OnVirgin(t *testing.T) {
	patched, err := patchTheme([]byte(themeWithSegments("session", "path")))
	if err != nil {
		t.Fatalf("patchTheme: %v", err)
	}
	segs := leftSegments(t, patched)
	if got := segmentTypes(segs); !reflect.DeepEqual(got, []string{"session", "text", "path"}) {
		t.Fatalf("segment types = %#v", got)
	}
	pm, ok := segs[1].(map[string]any)
	if !ok || !segmentIsPMManaged(pm) {
		t.Fatalf("inserted segment is not pm-managed: %#v", segs[1])
	}
}

func TestPatchTheme_ReplacesExistingPMSegment(t *testing.T) {
	orig := []byte(`{
	  "blocks": [{
	    "type": "prompt",
	    "alignment": "left",
	    "segments": [
	      {"type": "session"},
	      {"type": "text", "properties": {"pm_managed": "v0.7"}},
	      {"type": "path"}
	    ]
	  }]
	}`)

	patched, err := patchTheme(orig)
	if err != nil {
		t.Fatalf("patchTheme: %v", err)
	}
	segs := leftSegments(t, patched)
	if len(segs) != 3 {
		t.Fatalf("segment count = %d, want 3", len(segs))
	}
	pm, _ := segs[1].(map[string]any)
	props, _ := pm["properties"].(map[string]any)
	if props["pm_managed"] != "v0.8.1" {
		t.Fatalf("pm_managed = %v, want v0.8.1", props["pm_managed"])
	}
	again, err := patchTheme(patched)
	if err != nil {
		t.Fatalf("patchTheme again: %v", err)
	}
	if !bytes.Equal(again, patched) {
		t.Fatalf("patchTheme is not idempotent\nfirst:\n%s\nsecond:\n%s", patched, again)
	}
}

func TestPatchTheme_NoSessionSegment_InsertsAtStart(t *testing.T) {
	patched, err := patchTheme([]byte(themeWithSegments("path", "git")))
	if err != nil {
		t.Fatalf("patchTheme: %v", err)
	}
	if got := segmentTypes(leftSegments(t, patched)); !reflect.DeepEqual(got, []string{"text", "path", "git"}) {
		t.Fatalf("segment types = %#v", got)
	}
}

func TestPatchTheme_NoLeftBlock_Errors(t *testing.T) {
	_, err := patchTheme([]byte(`{"blocks":[{"type":"rprompt","alignment":"right","segments":[{"type":"path"}]}]}`))
	if err == nil {
		t.Fatalf("expected error")
	}
	if msg := err.Error(); !strings.Contains(msg, "left") || !strings.Contains(msg, "alignment") {
		t.Fatalf("error should mention left/alignment, got %q", msg)
	}
}

func TestPatchTheme_RoundTripsThroughJSONValid(t *testing.T) {
	fixture := []byte(`{
	  "final_space": true,
	  "blocks": [{
	    "type": "prompt",
	    "alignment": "left",
	    "segments": [
	      {"type": "session", "template": " {{ .UserName }}@{{ .HostName }} "},
	      {"type": "path", "powerline_symbol": "\ue0b0", "template": " \uf07c {{ .Path }} "}
	    ]
	  }]
	}`)
	patched, err := patchTheme(fixture)
	if err != nil {
		t.Fatalf("patchTheme: %v", err)
	}
	if !json.Valid(patched) {
		t.Fatalf("patched JSON is invalid:\n%s", patched)
	}
	root, err := decodeTheme(patched)
	if err != nil {
		t.Fatalf("decodeTheme: %v", err)
	}
	encoded, err := encodeTheme(root)
	if err != nil {
		t.Fatalf("encodeTheme: %v", err)
	}
	if !json.Valid(encoded) {
		t.Fatalf("re-encoded JSON is invalid:\n%s", encoded)
	}
}

func TestRemovePMSegment_StripsManagedSegment(t *testing.T) {
	patched, err := patchTheme([]byte(themeWithSegments("session", "path")))
	if err != nil {
		t.Fatalf("patchTheme: %v", err)
	}
	removedJSON, removed, err := removePMSegment(patched)
	if err != nil {
		t.Fatalf("removePMSegment: %v", err)
	}
	if !removed {
		t.Fatalf("removed = false, want true")
	}
	if got := segmentTypes(leftSegments(t, removedJSON)); !reflect.DeepEqual(got, []string{"session", "path"}) {
		t.Fatalf("segment types = %#v", got)
	}
	again, removed, err := removePMSegment(removedJSON)
	if err != nil {
		t.Fatalf("removePMSegment again: %v", err)
	}
	if removed {
		t.Fatalf("removed = true, want false")
	}
	if !bytes.Equal(again, removedJSON) {
		t.Fatalf("remove without pm should return input unchanged")
	}
}

func TestWritePatchedTheme_AtomicAndBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "theme.json")
	writeFile(t, path, "original content")

	if err := writePatchedTheme(path, []byte("new content"), true); err != nil {
		t.Fatalf("writePatchedTheme: %v", err)
	}
	assertFileContent(t, path, "new content")
	assertFileContent(t, path+".bak", "original content")
	if _, err := os.Stat(path + ".new"); !os.IsNotExist(err) {
		t.Fatalf(".new should not exist after atomic rename, stat err=%v", err)
	}

	if err := writePatchedTheme(path, []byte("newer content"), true); err != nil {
		t.Fatalf("writePatchedTheme second: %v", err)
	}
	assertFileContent(t, path, "newer content")
	assertFileContent(t, path+".bak", "original content")
}

func TestPromptSegmentCmd_EmitsValidJSON(t *testing.T) {
	e2eEnv(t)
	stdout, stderr, err := e2eRunRoot("prompt", "segment", "--omp")
	if err != nil {
		t.Fatalf("prompt segment: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON:\n%s", stdout)
	}
	var seg map[string]any
	if err := json.Unmarshal([]byte(stdout), &seg); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if seg["type"] != "text" {
		t.Fatalf("type = %v, want text", seg["type"])
	}
}

func TestPromptInstallCmd_DryRun_DoesNotWrite(t *testing.T) {
	e2eEnv(t)
	path := filepath.Join(t.TempDir(), "theme.json")
	orig := []byte(themeWithSegments("session", "path"))
	writeFile(t, path, string(orig))

	stdout, stderr, err := runCLI(t, "prompt", "install", "--omp", "--theme", path, "--dry-run")
	if err != nil {
		t.Fatalf("prompt install --dry-run: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, orig) {
		t.Fatalf("theme was modified\nwant:\n%s\ngot:\n%s", orig, got)
	}
	if !json.Valid([]byte(stdout)) || !themeHasPMSegment([]byte(stdout)) {
		t.Fatalf("stdout should contain patched JSON with pm segment:\n%s", stdout)
	}
}

func TestPromptInstallCmd_WritesAndBacksUp(t *testing.T) {
	e2eEnv(t)
	path := filepath.Join(t.TempDir(), "theme.json")
	orig := []byte(themeWithSegments("session", "path"))
	writeFile(t, path, string(orig))

	stdout, stderr, err := runCLI(t, "prompt", "install", "--omp", "--theme", path)
	if err != nil {
		t.Fatalf("prompt install: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	patched := readFile(t, path)
	if !themeHasPMSegment(patched) {
		t.Fatalf("patched theme missing pm segment:\n%s", patched)
	}
	assertFileContent(t, path+".bak", string(orig))
	count := len(leftSegments(t, patched))

	stdout, stderr, err = runCLI(t, "prompt", "install", "--omp", "--theme", path)
	if err != nil {
		t.Fatalf("prompt install again: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	again := readFile(t, path)
	if !themeHasPMSegment(again) {
		t.Fatalf("repatched theme missing pm segment:\n%s", again)
	}
	if got := len(leftSegments(t, again)); got != count {
		t.Fatalf("segment count = %d, want %d (no duplicate)", got, count)
	}
	assertFileContent(t, path+".bak", string(orig))
}

func TestPromptUninstallCmd_RemovesSegment(t *testing.T) {
	t.Run("restores backup when present", func(t *testing.T) {
		e2eEnv(t)
		path := filepath.Join(t.TempDir(), "theme.json")
		orig := []byte(themeWithSegments("session", "path"))
		writeFile(t, path, string(orig))
		if stdout, stderr, err := runCLI(t, "prompt", "install", "--omp", "--theme", path); err != nil {
			t.Fatalf("prompt install: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
		}

		stdout, stderr, err := runCLI(t, "prompt", "uninstall", "--omp", "--theme", path)
		if err != nil {
			t.Fatalf("prompt uninstall: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
		}
		got := readFile(t, path)
		if themeHasPMSegment(got) {
			t.Fatalf("theme still has pm segment:\n%s", got)
		}
		if !bytes.Equal(got, orig) {
			t.Fatalf("uninstall with backup should restore original\nwant:\n%s\ngot:\n%s", orig, got)
		}
	})

	t.Run("strips managed segment when backup is absent", func(t *testing.T) {
		e2eEnv(t)
		path := filepath.Join(t.TempDir(), "theme.json")
		patched, err := patchTheme([]byte(themeWithSegments("session", "path")))
		if err != nil {
			t.Fatalf("patchTheme: %v", err)
		}
		writeFile(t, path, string(patched))

		stdout, stderr, err := runCLI(t, "prompt", "uninstall", "--omp", "--theme", path)
		if err != nil {
			t.Fatalf("prompt uninstall: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
		}
		got := readFile(t, path)
		if themeHasPMSegment(got) {
			t.Fatalf("theme still has pm segment:\n%s", got)
		}
		if gotTypes := segmentTypes(leftSegments(t, got)); !reflect.DeepEqual(gotTypes, []string{"session", "path"}) {
			t.Fatalf("segment types = %#v", gotTypes)
		}
	})
}

func themeWithSegments(types ...string) string {
	var b strings.Builder
	b.WriteString(`{"blocks":[{"type":"prompt","alignment":"left","segments":[`)
	for i, typ := range types {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"type":`)
		enc, _ := json.Marshal(typ)
		b.Write(enc)
		b.WriteString(`}`)
	}
	b.WriteString(`]}]}`)
	return b.String()
}

func leftSegments(t *testing.T, themeJSON []byte) []any {
	t.Helper()
	root, err := decodeTheme(themeJSON)
	if err != nil {
		t.Fatalf("decodeTheme: %v\n%s", err, themeJSON)
	}
	blocks, _ := root["blocks"].([]any)
	for _, b := range blocks {
		block, _ := b.(map[string]any)
		if block["type"] == "prompt" && block["alignment"] == "left" {
			segs, _ := block["segments"].([]any)
			return segs
		}
	}
	t.Fatalf("left prompt block not found in %s", themeJSON)
	return nil
}

func segmentTypes(segs []any) []string {
	out := make([]string, 0, len(segs))
	for _, s := range segs {
		seg, _ := s.(map[string]any)
		typ, _ := seg["type"].(string)
		out = append(out, typ)
	}
	return out
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return content
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got := string(readFile(t, path))
	if got != want {
		t.Fatalf("%s content = %q, want %q", path, got, want)
	}
}
