package config

import (
	"fmt"
	"regexp"
	"strings"
)

// Built-in placeholder names. Project values add their own; they may not reuse these.
const (
	PhVersion         = "version"
	PhPreviousVersion = "previous_version"
	PhDir             = "dir"
	PhEnvFile         = "env_file"
	PhVar             = "var"
	PhServices        = "services"
)

var builtinPlaceholders = []string{PhVersion, PhPreviousVersion, PhDir, PhEnvFile, PhVar, PhServices}

func isBuiltin(name string) bool {
	for _, b := range builtinPlaceholders {
		if b == name {
			return true
		}
	}
	return false
}

// perImage placeholders take a different value for each image of a run.
func isPerImage(name string) bool {
	return name == PhVersion || name == PhPreviousVersion || name == PhVar
}

var placeholderName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// segment is a literal run or a placeholder reference.
type segment struct {
	lit  string
	name string
}

// parseText splits text into literals and placeholders. "{{" and "}}" are literal braces; any other brace
// must belong to a well-formed {name}.
func parseText(s string) ([]segment, error) {
	var segs []segment
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			segs = append(segs, segment{lit: lit.String()})
			lit.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '{':
			if i+1 < len(s) && s[i+1] == '{' {
				lit.WriteByte('{')
				i++
				continue
			}
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				return nil, fmt.Errorf("unclosed \"{\" at byte %d", i)
			}
			name := s[i+1 : i+end]
			if !placeholderName.MatchString(name) {
				return nil, fmt.Errorf("%q is not a placeholder name", "{"+name+"}")
			}
			flush()
			segs = append(segs, segment{name: name})
			i += end
		case '}':
			if i+1 < len(s) && s[i+1] == '}' {
				lit.WriteByte('}')
				i++
				continue
			}
			return nil, fmt.Errorf("stray \"}\" at byte %d", i)
		default:
			lit.WriteByte(c)
		}
	}
	flush()
	return segs, nil
}

// textNames returns the placeholder names in s; text that does not parse has none.
func textNames(s string) []string {
	segs, err := parseText(s)
	if err != nil {
		return nil
	}
	var names []string
	for _, sg := range segs {
		if sg.name != "" {
			names = append(names, sg.name)
		}
	}
	return names
}

// shellQuote wraps s in single quotes so a POSIX shell reads it back as one unchanged word.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// quotedPlaceholder returns the first placeholder of a shell text that the author has put inside quotes or
// right after a backslash. Such a placeholder would break the automatic single-quoting. The scan follows
// shell rules: single quotes have no escapes, double quotes and unquoted text honor backslashes.
func quotedPlaceholder(s string) (string, bool) {
	segs, err := parseText(s)
	if err != nil {
		return "", false
	}
	state, escaped := byte(0), false
	for _, sg := range segs {
		if sg.name != "" {
			if state != 0 || escaped {
				return sg.name, true
			}
			continue
		}
		for i := 0; i < len(sg.lit); i++ {
			c := sg.lit[i]
			switch {
			case escaped:
				escaped = false
			case state == '\'':
				if c == '\'' {
					state = 0
				}
			case c == '\\':
				escaped = true
			case state == '"':
				if c == '"' {
					state = 0
				}
			case c == '\'' || c == '"':
				state = c
			}
		}
	}
	return "", false
}

func quotedMessage(name string) string {
	return fmt.Sprintf("placeholder {%s} is inside quotes or after a backslash", name)
}

// value is a placeholder value: a scalar or a list.
type value struct {
	scalar string
	list   []string
	isList bool
}

// lookupFunc yields the value of a placeholder, or an error that says why there is none.
type lookupFunc func(name string) (value, *phError)

// phError is a placeholder failure with the problem code it maps to.
type phError struct {
	code string
	msg  string
}

// substitute fills the placeholders of s. In shell mode every value is single-quoted; otherwise values are
// inserted verbatim, because the result is handed on as one argument and never reaches a shell. A list
// value is only legal in shell mode, where it becomes a sequence of quoted words.
func substitute(s string, shell bool, lookup lookupFunc) (string, *phError) {
	segs, err := parseText(s)
	if err != nil {
		return "", &phError{CodeInvalidPlaceholder, err.Error()}
	}
	if shell {
		if name, bad := quotedPlaceholder(s); bad {
			return "", &phError{CodeQuotedPlaceholder, quotedMessage(name)}
		}
	}
	var b strings.Builder
	for _, sg := range segs {
		if sg.name == "" {
			b.WriteString(sg.lit)
			continue
		}
		v, perr := lookup(sg.name)
		if perr != nil {
			return "", perr
		}
		switch {
		case v.isList && shell:
			q := make([]string, len(v.list))
			for i, e := range v.list {
				q[i] = shellQuote(e)
			}
			b.WriteString(strings.Join(q, " "))
		case v.isList:
			return "", &phError{CodeListPlaceholder, fmt.Sprintf("{%s} is a list and cannot be embedded in a single argument", sg.name)}
		case shell:
			b.WriteString(shellQuote(v.scalar))
		default:
			b.WriteString(v.scalar)
		}
	}
	return b.String(), nil
}

// expandList substitutes every element of a list field. An element that is exactly a list placeholder
// expands into its items.
func expandList(elems []string, lookup lookupFunc) ([]string, *phError) {
	var out []string
	for _, e := range elems {
		if segs, err := parseText(e); err == nil && len(segs) == 1 && segs[0].name != "" {
			v, perr := lookup(segs[0].name)
			if perr != nil {
				return nil, perr
			}
			if v.isList {
				out = append(out, v.list...)
				continue
			}
		}
		s, perr := substitute(e, false, lookup)
		if perr != nil {
			return nil, perr
		}
		out = append(out, s)
	}
	return out, nil
}
