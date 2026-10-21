package inputassist

import (
	"strings"
	"unicode"
)

// LooksTranslatable is the old app's check that a selection is worth
// translating. There is no minimum length ("好的" must work); only structured
// text that should be left alone is refused: nothing but digits and symbols,
// a URL, an email address.
func LooksTranslatable(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if !strings.ContainsFunc(t, unicode.IsLetter) {
		return false
	}
	return !LooksLikeURL(t) && !LooksLikeEmail(t)
}

func hasSpace(s string) bool { return strings.ContainsFunc(s, unicode.IsSpace) }

// LooksLikeURL covers http(s) and www prefixes, anything with ://, and bare
// domains such as example.com/产品.
func LooksLikeURL(text string) bool {
	l := strings.ToLower(text)
	if strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "www.") {
		return true
	}
	if hasSpace(text) {
		return false
	}
	return strings.Contains(l, "://") || LooksLikeBareDomain(text)
}

func isASCIIAlpha(r rune) bool { return r < 128 && unicode.IsLetter(r) }

// LooksLikeBareDomain is a host without a scheme: dot separated ASCII labels
// ending in a top level domain of two or more letters, so "12.5" is not one.
func LooksLikeBareDomain(text string) bool {
	if hasSpace(text) {
		return false
	}
	host := text
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return false
	}
	top := labels[len(labels)-1]
	if len(top) < 2 {
		return false
	}
	for _, r := range top {
		if !isASCIIAlpha(r) {
			return false
		}
	}
	for _, label := range labels[:len(labels)-1] {
		if label == "" {
			return false
		}
		for _, r := range label {
			if !(isASCIIAlpha(r) || (r < 128 && unicode.IsDigit(r)) || r == '-') {
				return false
			}
		}
	}
	return true
}

// LooksLikeEmail is one @ with something before it and a dot after it.
func LooksLikeEmail(text string) bool {
	if hasSpace(text) {
		return false
	}
	parts := strings.Split(text, "@")
	return len(parts) == 2 && parts[0] != "" && strings.Contains(parts[1], ".")
}
