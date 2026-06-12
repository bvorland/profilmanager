package core

import (
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func isolateTemplateHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", tmp)
		t.Setenv("APPDATA", filepath.Join(tmp, "appdata"))
		t.Setenv("LOCALAPPDATA", filepath.Join(tmp, "localappdata"))
	} else {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdgcfg"))
		t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "xdgstate"))
	}
	return tmp
}

func TestSuggestTemplates(t *testing.T) {
	isolateTemplateHome(t)
	for _, p := range []*Profile{
		{Schema: SchemaVersion, Name: "Contoso.MainDev", Color: "Cyan"},
		{Schema: SchemaVersion, Name: "Contoso.Backend", Color: "Magenta"},
		{Schema: SchemaVersion, Name: "Contoso.Pipeline", Color: "Yellow"},
		{Schema: SchemaVersion, Name: "Contoso.Platform", Color: "Blue"},
		{Schema: SchemaVersion, Name: "Contoso.Personal", Color: "White"},
		{Schema: SchemaVersion, Name: "Fabrikam.Work", Color: "Red"},
	} {
		path, err := ProfilePath(p.Name)
		if err != nil {
			t.Fatalf("ProfilePath(%q): %v", p.Name, err)
		}
		if err := p.Save(path); err != nil {
			t.Fatalf("Save(%q): %v", p.Name, err)
		}
	}

	want := []string{
		"Contoso.Backend",
		"Contoso.MainDev",
		"Contoso.Personal",
		"Contoso.Pipeline",
		"Contoso.Platform",
	}
	if got := SuggestTemplates("Contoso.Foo"); !reflect.DeepEqual(got, want) {
		t.Fatalf("SuggestTemplates Contoso: got %#v want %#v", got, want)
	}
	if got := SuggestTemplates("Brand.New"); got != nil {
		t.Fatalf("SuggestTemplates Brand: got %#v want nil", got)
	}
}
