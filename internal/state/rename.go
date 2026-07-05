package state

// RenameProfileMarkers updates the session/operator markers that reference
// oldName so they point at newName after a profile rename:
//
//   - the current session's active-profile marker (only when it currently
//     points at oldName), and
//   - the operator-global last-profile pointer (only when it points at
//     oldName).
//
// Best-effort: it attempts both updates and returns the first error, so a
// failure updating one marker does not skip the other.
func RenameProfileMarkers(oldName, newName string) error {
	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if active, _, err := GetActiveProfile(); err != nil {
		note(err)
	} else if active == oldName {
		note(SetActiveProfile(newName))
	}

	if last, err := GetLastProfile(); err != nil {
		note(err)
	} else if last == oldName {
		note(SetLastProfile(newName))
	}

	return firstErr
}
