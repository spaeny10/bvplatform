package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// FrontendCall is one HTTP/stream call site found in frontend/src.
type FrontendCall struct {
	Method string // GET unless an explicit method: '...' option is present; WS for WebSocket
	Path   string // resolved path with ${expr} replaced by {*}
	Func   string // nearest enclosing exported function, or (module)
	File   string // repo-relative, forward slashes
	Line   int
	Kind   string // fetch | authFetch | fetchJSON | EventSource | WebSocket
}

var (
	// const NAME = '/api'; — per-file string consts used inside template
	// literals (API_BASE, BASE, and anything else of the same shape).
	reConst = regexp.MustCompile(`(?m)^(?:export )?const (\w+)\s*=\s*'([^']*)'`)
	// call sites; group 1 = kind
	reCall = regexp.MustCompile(`(?:\bawait\s+)?\b(authFetch|fetchJSON|fetch|new EventSource|new WebSocket)\s*(?:<[^>(]*>)?\(`)
	// nearest enclosing exported function for attribution
	reFunc = regexp.MustCompile(`export (?:async )?(?:function (\w+)|const (\w+)\s*=)`)
	// explicit method option inside the call's argument span
	reMethod = regexp.MustCompile(`method:\s*['"](\w+)['"]`)
)

// extractFrontendCalls scans every .ts/.tsx file under frontend/src for
// fetch-family calls whose first argument is a string or template
// literal resolving to an absolute path (/api, /auth, /ws, ...).
// Dynamic first arguments (variables) can't be resolved statically and
// are skipped — the route side of the diff still covers them.
func extractFrontendCalls(srcDir string) ([]FrontendCall, error) {
	var calls []FrontendCall
	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".ts" && ext != ".tsx" {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		relPath := filepath.ToSlash(strings.TrimPrefix(path, filepath.Dir(filepath.Dir(srcDir))+string(filepath.Separator)))
		calls = append(calls, scanFile(string(data), relPath)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(calls, func(i, j int) bool {
		if calls[i].Path != calls[j].Path {
			return calls[i].Path < calls[j].Path
		}
		return calls[i].File < calls[j].File
	})
	return calls, nil
}

func scanFile(src, relPath string) []FrontendCall {
	consts := map[string]string{}
	for _, m := range reConst.FindAllStringSubmatch(src, -1) {
		consts[m[1]] = m[2]
	}

	// Pre-compute exported-function positions for attribution.
	type fnPos struct {
		off  int
		name string
	}
	var fns []fnPos
	for _, m := range reFunc.FindAllStringSubmatchIndex(src, -1) {
		name := ""
		if m[2] >= 0 {
			name = src[m[2]:m[3]]
		} else if m[4] >= 0 {
			name = src[m[4]:m[5]]
		}
		fns = append(fns, fnPos{m[0], name})
	}
	enclosing := func(off int) string {
		name := "(module)"
		for _, f := range fns {
			if f.off > off {
				break
			}
			name = f.name
		}
		return name
	}

	var out []FrontendCall
	for _, m := range reCall.FindAllStringSubmatchIndex(src, -1) {
		kind := src[m[2]:m[3]]
		argStart := m[1] // position right after '('
		arg, span := firstArgAndSpan(src, argStart)
		path, ok := resolveURLArg(arg, consts)
		if !ok {
			continue
		}
		// Only absolute app paths participate in the diff.
		if !strings.HasPrefix(path, "/") {
			continue
		}
		keep := false
		for _, p := range []string{"/api", "/auth", "/ws", "/share", "/media", "/exports", "/hls"} {
			if strings.HasPrefix(path, p) {
				keep = true
				break
			}
		}
		if !keep {
			continue
		}
		method := "GET"
		if kind == "new WebSocket" {
			method = "WS"
		}
		if mm := reMethod.FindStringSubmatch(span); mm != nil {
			method = strings.ToUpper(mm[1])
		}
		kindName := strings.TrimPrefix(kind, "new ")
		out = append(out, FrontendCall{
			Method: method,
			Path:   path,
			Func:   enclosing(m[0]),
			File:   relPath,
			Line:   1 + strings.Count(src[:m[0]], "\n"),
			Kind:   kindName,
		})
	}
	return out
}

// firstArgAndSpan returns the first argument expression starting at
// argStart and the full argument span up to the call's closing paren,
// skipping nested parens, quotes, and template literals (including
// ${...} interpolations).
func firstArgAndSpan(src string, argStart int) (firstArg, span string) {
	depth := 1 // we are just past the opening paren
	i := argStart
	firstArgEnd := -1
	for i < len(src) {
		c := src[i]
		switch c {
		case '\'', '"':
			i = skipQuoted(src, i, c)
			continue
		case '`':
			i = skipTemplate(src, i)
			continue
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				if firstArgEnd < 0 {
					firstArgEnd = i
				}
				return strings.TrimSpace(src[argStart:firstArgEnd]), src[argStart:i]
			}
		case ',':
			if depth == 1 && firstArgEnd < 0 {
				firstArgEnd = i
			}
		}
		i++
	}
	return strings.TrimSpace(src[argStart:]), src[argStart:]
}

func skipQuoted(src string, i int, quote byte) int {
	i++
	for i < len(src) {
		if src[i] == '\\' {
			i += 2
			continue
		}
		if src[i] == quote {
			return i + 1
		}
		i++
	}
	return i
}

// skipTemplate advances past a backtick template literal, including
// nested ${...} expressions (which may themselves contain templates).
func skipTemplate(src string, i int) int {
	i++ // past opening backtick
	for i < len(src) {
		switch {
		case src[i] == '\\':
			i += 2
		case src[i] == '`':
			return i + 1
		case src[i] == '$' && i+1 < len(src) && src[i+1] == '{':
			depth := 1
			i += 2
			for i < len(src) && depth > 0 {
				switch src[i] {
				case '{':
					depth++
				case '}':
					depth--
				case '`':
					i = skipTemplate(src, i) - 1
				}
				i++
			}
		default:
			i++
		}
	}
	return i
}

// resolveURLArg turns the first argument of a fetch-family call into a
// concrete path: plain string literals pass through; template literals
// get per-file consts substituted and remaining ${...} replaced with
// {*}; everything else (bare variables) is unresolvable.
func resolveURLArg(arg string, consts map[string]string) (string, bool) {
	arg = strings.TrimSpace(arg)
	if len(arg) >= 2 && (arg[0] == '\'' || arg[0] == '"') {
		end := strings.IndexByte(arg[1:], arg[0])
		if end < 0 {
			return "", false
		}
		return arg[1 : 1+end], true
	}
	if len(arg) >= 2 && arg[0] == '`' {
		body := arg[1:]
		if idx := strings.LastIndexByte(body, '`'); idx >= 0 {
			body = body[:idx]
		}
		// Substitute known consts, then any leftover interpolation -> {*}
		reInterp := regexp.MustCompile(`\$\{([^}]*)\}`)
		resolved := reInterp.ReplaceAllStringFunc(body, func(m string) string {
			inner := m[2 : len(m)-1]
			if v, ok := consts[strings.TrimSpace(inner)]; ok {
				return v
			}
			return "{*}"
		})
		// Drop the query string — route matching is path-only.
		if q := strings.IndexByte(resolved, '?'); q >= 0 {
			resolved = resolved[:q]
		}
		return resolved, true
	}
	return "", false
}

// callDisplay is the human-readable form used in the coverage tables.
func (c FrontendCall) callDisplay() string {
	return fmt.Sprintf("`%s` (%s:%d)", c.Func, c.File, c.Line)
}
