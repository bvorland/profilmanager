package core

import "errors"

// ErrAmbiguous means a profile name matched more than one profile.
var ErrAmbiguous = errors.New("ambiguous profile name")

// ErrNotFound means no profile matched the requested name.
var ErrNotFound = errors.New("profile not found")

// ErrCancelled means an interactive core/TUI flow was cancelled by the user.
var ErrCancelled = errors.New("cancelled")
