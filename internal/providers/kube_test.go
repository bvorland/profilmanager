package providers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bvorland/profilmanager/internal/core"
)

func TestKubeWhoami(t *testing.T) {
	dir := fakePathDir(t)
	viewJSON := `{
		"contexts": [{
			"name": "minikube",
			"context": {
				"cluster": "minikube-cluster",
				"user": "minikube-user",
				"namespace": "kube-system"
			}
		}]
	}`
	writeFakeCLI(t, dir, "kubectl",
		fakeCase{Match: "config", Stdout: "minikube\n", Exit: 0},
	)
	// kubectl is called twice; the same fake CLI must handle BOTH:
	// `config current-context` (stdout: "minikube")
	// `config view --minify -o json` (stdout: viewJSON)
	// Our dispatcher matches only arg #1, so we need a smarter dispatcher.
	// For this test, re-install with a two-stage script that checks arg #2.
	_ = viewJSON
	// Implementation: replace the fake script with a custom one.
	writeKubectlFake(t, dir, "minikube\n", viewJSON)

	st, err := (kubeProvider{}).Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if !st.LoggedIn {
		t.Fatalf("expected LoggedIn=true: %+v", st)
	}
	if st.Subscription != "minikube" {
		t.Errorf("Subscription (context) = %q", st.Subscription)
	}
	if st.Extra["context"] != "minikube" {
		t.Errorf("context extra = %q", st.Extra["context"])
	}
	if st.Account != "minikube-user" {
		t.Errorf("Account (user) = %q", st.Account)
	}
	if st.Extra["namespace"] != "kube-system" {
		t.Errorf("namespace = %q", st.Extra["namespace"])
	}
	if st.Extra["cluster"] != "minikube-cluster" {
		t.Errorf("cluster = %q", st.Extra["cluster"])
	}
}

// writeKubectlFake installs a fake kubectl that dispatches on the
// second positional arg (config current-context vs config view ...).
// Plain writeFakeCLI only branches on arg #1, so kubectl needs a
// hand-rolled script.
func writeKubectlFake(t *testing.T, dir, current, viewJSON string) {
	t.Helper()
	curPath := filepath.Join(dir, "kubectl-current.txt")
	viewPath := filepath.Join(dir, "kubectl-view.json")
	if err := os.WriteFile(curPath, []byte(current), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(viewPath, []byte(viewJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	switch {
	case isWindows():
		body := "@echo off\r\n" +
			"if /I \"%~1\"==\"config\" (\r\n" +
			"  if /I \"%~2\"==\"current-context\" (\r\n" +
			"    type \"" + curPath + "\"\r\n" +
			"    exit /b 0\r\n" +
			"  )\r\n" +
			"  if /I \"%~2\"==\"view\" (\r\n" +
			"    type \"" + viewPath + "\"\r\n" +
			"    exit /b 0\r\n" +
			"  )\r\n" +
			")\r\n" +
			"exit /b 1\r\n"
		path := filepath.Join(dir, "kubectl.cmd")
		// Some Windows shells truncate writes when the file is open;
		// remove first to avoid the .cmd that writeFakeCLI may have
		// dropped earlier.
		_ = os.Remove(path)
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	default:
		body := "#!/bin/sh\n" +
			"if [ \"$1\" = \"config\" ]; then\n" +
			"  if [ \"$2\" = \"current-context\" ]; then\n" +
			"    cat " + shQuote(curPath) + "\n" +
			"    exit 0\n" +
			"  fi\n" +
			"  if [ \"$2\" = \"view\" ]; then\n" +
			"    cat " + shQuote(viewPath) + "\n" +
			"    exit 0\n" +
			"  fi\n" +
			"fi\n" +
			"exit 1\n"
		path := filepath.Join(dir, "kubectl")
		_ = os.Remove(path)
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func isWindows() bool {
	return os.PathSeparator == '\\'
}

func TestKubeWhoamiNoContext(t *testing.T) {
	dir := fakePathDir(t)
	writeFakeCLI(t, dir, "kubectl",
		fakeCase{Stderr: "error: current-context is not set", Exit: 1},
	)
	st, err := (kubeProvider{}).Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if st.LoggedIn {
		t.Errorf("expected LoggedIn=false")
	}
	if !strings.Contains(st.Error, "current-context") {
		t.Errorf("Error = %q", st.Error)
	}
}

func TestKubeApply(t *testing.T) {
	stubCoreDirs(t)
	p := &core.Profile{
		Schema: "1", Name: "k-test",
		Kube: &core.KubeProfile{Context: "ctx-a", Namespace: "ns-a"},
	}
	env, err := (kubeProvider{}).Apply(p)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if env["KUBECONFIG"] == "" {
		t.Errorf("KUBECONFIG not set")
	}
	if !strings.HasSuffix(filepath.ToSlash(env["KUBECONFIG"]), "/kube/k-test/config") {
		t.Errorf("KUBECONFIG suffix unexpected: %q", env["KUBECONFIG"])
	}
	if env["KUBE_CONTEXT"] != "ctx-a" {
		t.Errorf("KUBE_CONTEXT = %q", env["KUBE_CONTEXT"])
	}
	if env["KUBE_NAMESPACE"] != "ns-a" {
		t.Errorf("KUBE_NAMESPACE = %q", env["KUBE_NAMESPACE"])
	}
	if fi, err := os.Stat(filepath.Dir(env["KUBECONFIG"])); err != nil || !fi.IsDir() {
		t.Errorf("kube dir not created: %v %v", fi, err)
	}
}
