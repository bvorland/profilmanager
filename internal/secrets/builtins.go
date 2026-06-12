package secrets

import "sync"

var (
	builtinsOnce sync.Once
)

// RegisterBuiltins installs the v1 resolvers (op, wincred, dotenv) in
// the package-level registry. Idempotent — safe to call from multiple
// entry points (cli root init, mcp server init) without double-registering.
//
// Tests typically build their own resolvers and call [Register] directly
// rather than going through this helper.
func RegisterBuiltins() {
	builtinsOnce.Do(func() {
		Register(NewOpResolver())
		Register(NewWinCredResolver())
		Register(NewDotEnvResolver())
	})
}
