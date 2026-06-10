package main

import (
	"fmt"
	"sort"
	"strings"
)

// externalRoutes are backend routes legitimately called by clients other
// than the Next.js frontend — their absence from the frontend-call scan
// is expected, not drift.
var externalRoutes = map[string]string{
	"POST /api/integrations/milesight/sense/{*}": "camera push webhook (token-auth)",
	"GET /share/{*}":          "public evidence share links (token-auth)",
	"GET /metrics":            "Prometheus scraper (network-trust)",
	"GET /api/health":         "Docker HEALTHCHECK / uptime monitors",
	"GET /media/v1/{*}":       "minted media URLs consumed via <img>/<video> src",
	"ANY /exports/{**}":       "evidence ZIP download links",
	"GET /api/cameras/{*}/vca/snapshot": "snapshot <img> src (UUID-auth)",
	"ANY /api/cameras/{*}/web-ui/{**}":  "camera web-UI iframe (browser-driven)",
	"ANY /api/cameras/{*}/web-ui":       "camera web-UI iframe (browser-driven)",
}

type matchRow struct {
	route    Route
	callers  []FrontendCall
	features []string // feature IDs claiming this route
}

type coverage struct {
	matched      []matchRow
	backendOnly  []matchRow // no frontend caller; note from allowlist if any
	frontendOnly []FrontendCall
	nRoutes      int
	nCalls       int
}

func crossReference(routes []Route, calls []FrontendCall, features []Feature) coverage {
	cov := coverage{nRoutes: len(routes), nCalls: len(calls)}

	// Feature route claims, normalized.
	type claim struct {
		method, path, id string
	}
	var claims []claim
	for _, f := range features {
		for _, spec := range f.Routes {
			parts := strings.SplitN(spec, " ", 2)
			if len(parts) == 2 {
				claims = append(claims, claim{parts[0], normalizePath(parts[1]), f.ID})
			}
		}
	}

	usedCall := make([]bool, len(calls))
	for _, rt := range routes {
		np := normalizePath(rt.Path)
		row := matchRow{route: rt}
		for ci, c := range calls {
			if rt.Method != "ANY" && c.Method != rt.Method {
				// WS calls hit GET-registered websocket routes.
				if !(c.Method == "WS" && rt.Method == "GET") {
					continue
				}
			}
			if pathsMatch(np, normalizePath(c.Path)) {
				row.callers = append(row.callers, c)
				usedCall[ci] = true
			}
		}
		for _, cl := range claims {
			if (cl.method == rt.Method || rt.Method == "ANY" || cl.method == "ANY") && pathsMatch(np, cl.path) {
				row.features = append(row.features, cl.id)
			}
		}
		if len(row.callers) > 0 {
			cov.matched = append(cov.matched, row)
		} else {
			cov.backendOnly = append(cov.backendOnly, row)
		}
	}
	for ci, c := range calls {
		if !usedCall[ci] {
			cov.frontendOnly = append(cov.frontendOnly, c)
		}
	}
	sort.Slice(cov.frontendOnly, func(i, j int) bool { return cov.frontendOnly[i].Path < cov.frontendOnly[j].Path })
	return cov
}

func renderCoverage(cov coverage) string {
	var b strings.Builder
	b.WriteString("# API coverage matrix\n\n")
	b.WriteString("<!-- GENERATED FILE — DO NOT HAND-EDIT. Regenerate: go run ./cmd/docgen -write -->\n\n")
	b.WriteString(fmt.Sprintf(
		"Extracted from `internal/api/router.go` and `frontend/src/**`. %d backend routes, %d resolvable frontend call sites.\n\n",
		cov.nRoutes, cov.nCalls))
	b.WriteString(fmt.Sprintf("- **Matched** (route has ≥1 frontend caller): %d\n", len(cov.matched)))
	b.WriteString(fmt.Sprintf("- **Backend-only** (no frontend caller found): %d\n", len(cov.backendOnly)))
	b.WriteString(fmt.Sprintf("- **Frontend-only** (no backend route — likely 404): %d\n\n", len(cov.frontendOnly)))

	b.WriteString("## A. Matched routes\n\n")
	b.WriteString("| Route | Handler | Callers | Features |\n|---|---|---|---|\n")
	for _, m := range cov.matched {
		var callers []string
		seen := map[string]bool{}
		for _, c := range m.callers {
			d := c.callDisplay()
			if !seen[d] {
				seen[d] = true
				callers = append(callers, d)
			}
		}
		b.WriteString(fmt.Sprintf("| `%s %s` | `%s` | %s | %s |\n",
			m.route.Method, m.route.Path, m.route.Handler,
			strings.Join(callers, "<br>"), featureLinks(m.features)))
	}

	b.WriteString("\n## B. Backend-only routes\n\n")
	b.WriteString("Routes with no frontend caller. Annotated routes are called by external clients by design; unannotated rows are either unwired backend surface or callers the static scan cannot resolve.\n\n")
	b.WriteString("| Route | Handler | External caller | Features |\n|---|---|---|---|\n")
	for _, m := range cov.backendOnly {
		key := m.route.Method + " " + normalizePath(m.route.Path)
		note := externalRoutes[key]
		if note == "" {
			note = externalRoutes["ANY "+normalizePath(m.route.Path)]
		}
		if note == "" {
			note = "—"
		}
		b.WriteString(fmt.Sprintf("| `%s %s` | `%s` | %s | %s |\n",
			m.route.Method, m.route.Path, m.route.Handler, note, featureLinks(m.features)))
	}

	b.WriteString("\n## C. Frontend-only calls\n\n")
	if len(cov.frontendOnly) == 0 {
		b.WriteString("None — every resolvable frontend call matches a backend route.\n")
	} else {
		b.WriteString("These call sites match no registered route — each one 404s at runtime and needs a fix or removal.\n\n")
		b.WriteString("| Call | Where |\n|---|---|\n")
		for _, c := range cov.frontendOnly {
			b.WriteString(fmt.Sprintf("| `%s %s` | %s |\n", c.Method, c.Path, c.callDisplay()))
		}
	}
	return b.String()
}

func featureLinks(ids []string) string {
	if len(ids) == 0 {
		return "—"
	}
	var out []string
	for _, id := range ids {
		out = append(out, "`"+id+"`")
	}
	return strings.Join(out, " ")
}
