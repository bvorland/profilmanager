package providers

// init wires the v1 built-in providers. Adding a new built-in is a
// one-line append here plus a new file under internal/providers/.
// Out-of-process providers (v2) will register through a different path.
func init() {
	Register(AzProvider())
	Register(AzdProvider())
	Register(GhProvider())
	Register(KubeProvider())
	Register(GitProvider())
}
