package inputassist

import "strings"

// App is how a target application can be recognised. A user may write any of
// the three in the lists: "WeChat", "WeChat.exe" or a bundle id.
type App struct {
	ID   string // bundle id (macOS)
	Name string // display name
	Exe  string // executable file name
	PID  int
}

// AppAllowed is the old app's InputAssistAppFilter.allows. An app that cannot
// be recognised at all is never touched; the blocklist beats the scope.
func AppAllowed(app *App, scope string, blocklist, allowlist []string) bool {
	if app == nil {
		return false
	}
	if AppMatches(*app, blocklist) {
		return false
	}
	if scope == ScopeAllowlist {
		return AppMatches(*app, allowlist)
	}
	return true
}

// AppMatches reports whether any entry names the app, ignoring case, spaces
// and a trailing ".app" or ".exe".
func AppMatches(app App, entries []string) bool {
	if len(entries) == 0 {
		return false
	}
	have := map[string]bool{}
	for _, v := range []string{app.ID, app.Name, app.Exe} {
		if n := normalizeApp(v); n != "" {
			have[n] = true
		}
	}
	for _, e := range entries {
		if have[normalizeApp(e)] {
			return true
		}
	}
	return false
}

func normalizeApp(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, ".app")
	s = strings.TrimSuffix(s, ".exe")
	return s
}
