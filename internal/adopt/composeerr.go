package adopt

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxComposeLine bounds the Compose message that reaches the operator.
const maxComposeLine = 300

var (
	ansiRe = regexp.MustCompile(`(?:\x1b|\x{9b})(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b\n]*(?:\x07|\x1b\\)?|[@-Z\\-_])|\x{9b}[0-?]*[ -/]*[@-~]`)
	// Env file errors such as "line 3: <text>" carry values from the file, so the text is cut after the number.
	lineRe       = regexp.MustCompile(`(\bline \d+:).*`)
	openDeniedRe = regexp.MustCompile(`\bopen (/.+?): permission denied`)
)

// composeFailureLine returns the last meaningful line of the output of a failed docker compose config as one
// line without warnings, escape and control characters, and without the text after "line <n>:".
func composeFailureLine(out string) string {
	out = ansiRe.ReplaceAllString(strings.ToValidUTF8(out, ""), "")
	lines := strings.FieldsFunc(out, func(r rune) bool { return r == '\n' || r == '\r' })
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.Map(func(r rune) rune {
			switch {
			case r == '\t':
				return ' '
			case unicode.IsControl(r):
				return -1
			}
			return r
		}, lines[i])
		l = strings.TrimSpace(lineRe.ReplaceAllString(l, "$1"))
		if l == "" || strings.Contains(l, "level=warning") {
			continue
		}
		return l
	}
	return ""
}

// deniedOpen returns the file named by an "open <path>: permission denied" message.
func deniedOpen(line string) (string, bool) {
	if m := openDeniedRe.FindStringSubmatch(line); m != nil {
		return cleanAbs(m[1])
	}
	return "", false
}

func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
