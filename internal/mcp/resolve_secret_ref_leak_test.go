package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/bvorland/profilmanager/internal/secrets"
)

// TestResolveSecretRefDoesNotLeakDotEnvFileContents is a regression test for
// the metadata-only contract of resolve_secret_ref. A prior implementation
// echoed the raw offending line of a dotenv:// file back through the error
// string (and the audit log), so a prompt-injected agent could point the ref
// at ~/.git-credentials — whose first line has no '=' — and read the
// credential. The tool must never surface file bytes in the structured
// content, the text content, or the audit log.
func TestResolveSecretRefDoesNotLeakDotEnvFileContents(t *testing.T) {
	withFreshState(t)
	// Ensure the dotenv resolver is registered (production wires this via
	// NewServer/Exec → secrets.RegisterBuiltins). Without it, DescribeRef
	// short-circuits with ErrNoResolver and never exercises the file read.
	secrets.RegisterBuiltins()

	// A credential-store file whose first line is NOT KEY=VALUE, mirroring
	// ~/.git-credentials ("https://user:TOKEN@host").
	secret := "s3cr3t-token-must-never-surface"
	dir := t.TempDir()
	credPath := filepath.Join(dir, ".git-credentials")
	if err := os.WriteFile(credPath, []byte("https://user:"+secret+"@github.com\n"), 0o600); err != nil {
		t.Fatalf("write credential fixture: %v", err)
	}

	ref := "dotenv://" + filepath.ToSlash(credPath) + "#anykey"
	res, err := handleResolveSecretRef(context.Background(),
		callRequestWith(map[string]any{"ref": ref}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}

	blob, _ := json.Marshal(res.StructuredContent)
	var textAll string
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			textAll += tc.Text
		}
	}
	if strings.Contains(string(blob), secret) || strings.Contains(textAll, secret) {
		t.Fatalf("resolve_secret_ref leaked dotenv file contents:\nstructured=%s\ntext=%s", blob, textAll)
	}

	// The raw line must not reach the audit log either (same root cause).
	auditDir, err := AuditDir()
	if err != nil {
		t.Fatalf("AuditDir: %v", err)
	}
	for _, e := range readAuditLines(t, auditDir) {
		if strings.Contains(e.Error, secret) || strings.Contains(e.Ref, secret) {
			t.Fatalf("audit log leaked dotenv file contents: %+v", e)
		}
	}
}
