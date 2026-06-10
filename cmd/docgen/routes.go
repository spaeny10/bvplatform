package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// Route is one chi registration discovered in internal/api/router.go.
type Route struct {
	Method  string // GET/POST/PUT/PATCH/DELETE, or ANY for Handle/HandleFunc
	Path    string // full path including parent r.Route prefixes
	Handler string // rendered handler expression, e.g. HandleLogin or (inline)
	Line    int
}

// chiMethods maps chi registration method names to HTTP methods.
var chiMethods = map[string]string{
	"Get":        "GET",
	"Post":       "POST",
	"Put":        "PUT",
	"Patch":      "PATCH",
	"Delete":     "DELETE",
	"Head":       "HEAD",
	"Options":    "OPTIONS",
	"Handle":     "ANY",
	"HandleFunc": "ANY",
}

// extractRoutes statically walks NewRouter in router.go, carrying the
// prefix stack through nested r.Route(...) calls. All registrations in
// this codebase use literal path strings (verified 2026-06-09), so a
// non-literal first argument is reported as an error route rather than
// silently skipped.
func extractRoutes(routerPath string) ([]Route, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, routerPath, nil, 0)
	if err != nil {
		return nil, err
	}

	var newRouter *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "NewRouter" {
			newRouter = fd
			break
		}
	}
	if newRouter == nil {
		return nil, fmt.Errorf("NewRouter not found in %s", routerPath)
	}

	var routes []Route
	walkBody(fset, newRouter.Body, "", &routes)

	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path != routes[j].Path {
			return routes[i].Path < routes[j].Path
		}
		return routes[i].Method < routes[j].Method
	})
	return routes, nil
}

func walkBody(fset *token.FileSet, body *ast.BlockStmt, prefix string, out *[]Route) {
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name := sel.Sel.Name

		// r.Route("/prefix", func(r chi.Router) { ... }) — recurse with
		// the extended prefix, and do NOT let ast.Inspect descend into
		// the func literal (we already walked it with the right prefix).
		if name == "Route" && len(call.Args) == 2 {
			lit, okLit := stringLit(call.Args[0])
			fn, okFn := call.Args[1].(*ast.FuncLit)
			if okLit && okFn {
				walkBody(fset, fn.Body, prefix+lit, out)
				return false
			}
		}

		// r.Group(func(r chi.Router) { ... }) — same prefix.
		if name == "Group" && len(call.Args) == 1 {
			if fn, okFn := call.Args[0].(*ast.FuncLit); okFn {
				walkBody(fset, fn.Body, prefix, out)
				return false
			}
		}

		method, isReg := chiMethods[name]
		if !isReg || len(call.Args) < 1 {
			return true
		}
		// Only count registrations on a chi router receiver: a bare
		// ident (r) or a chained call like r.With(...). This filters
		// out unrelated .Get/.Handle calls on other types.
		if !isRouterReceiver(sel.X) {
			return true
		}
		lit, okLit := stringLit(call.Args[0])
		if !okLit {
			*out = append(*out, Route{
				Method: method, Path: prefix + "<non-literal>",
				Handler: "?", Line: fset.Position(call.Pos()).Line,
			})
			return true
		}
		handler := "(inline)"
		if len(call.Args) >= 2 {
			handler = renderHandler(call.Args[1])
		}
		path := prefix + lit
		if path == "" {
			path = "/"
		}
		*out = append(*out, Route{
			Method: method, Path: path, Handler: handler,
			Line: fset.Position(call.Pos()).Line,
		})
		return true
	})
}

// isRouterReceiver reports whether the expression is `r` or a chain
// rooted at `r` (e.g. r.With(mw).With(mw2)).
func isRouterReceiver(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name == "r"
	case *ast.CallExpr:
		if s, ok := x.Fun.(*ast.SelectorExpr); ok {
			return isRouterReceiver(s.X)
		}
	}
	return false
}

func stringLit(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(bl.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// renderHandler turns the handler argument into a short display name:
// HandleLogin(db, cfg) -> HandleLogin; hub.HandleWebSocket ->
// hub.HandleWebSocket; func literals -> (inline).
func renderHandler(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.CallExpr:
		return renderHandler(x.Fun)
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return renderHandler(x.X) + "." + x.Sel.Name
	case *ast.FuncLit:
		return "(inline)"
	default:
		return "?"
	}
}

// normalizePath collapses every {param} segment to {*} so backend
// routes and frontend template-literal paths compare structurally.
// A trailing chi wildcard `*` becomes {**} (prefix match).
func normalizePath(p string) string {
	segs := strings.Split(strings.TrimSuffix(p, "/"), "/")
	for i, s := range segs {
		switch {
		case s == "*":
			segs[i] = "{**}"
		case strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}"):
			segs[i] = "{*}"
		case strings.Contains(s, "{"):
			// mixed literal+param segment, e.g. "v{n}" — rare; treat as param
			segs[i] = "{*}"
		}
	}
	out := strings.Join(segs, "/")
	if out == "" {
		out = "/"
	}
	return out
}

// pathsMatch compares a normalized backend route against a normalized
// frontend path, honoring the {**} prefix wildcard.
func pathsMatch(route, call string) bool {
	rs := strings.Split(route, "/")
	cs := strings.Split(call, "/")
	for i, r := range rs {
		if r == "{**}" {
			return true // wildcard swallows the rest
		}
		if i >= len(cs) {
			return false
		}
		if r == "{*}" || cs[i] == "{*}" {
			continue
		}
		if r != cs[i] {
			return false
		}
	}
	return len(rs) == len(cs)
}
