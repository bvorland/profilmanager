// Package themes provides embedded oh-my-posh theme assets shipped
// with pm. The default statusline theme is rendered by `pm statusline`
// when invoked from Copilot CLI's statusLine integration.
package themes

import _ "embed"

// StatuslineOMP is the default oh-my-posh theme used by `pm statusline`.
// It's embedded at build time and written to disk on first use (and on
// `pm prompt install-statusline`).
//
//go:embed statusline.omp.json
var StatuslineOMP []byte
