package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Feature is one hand-authored block in docs/feature-registry/*.md.
type Feature struct {
	ID       string
	Name     string
	AreaFile string // e.g. 02-recording-playback.md
	Tier     string // core | back-burner | cut
	Status   string // working | partial | placeholder | stub
	Frontend []string
	Routes   []string // raw "METHOD /path" specs
	Flag     string
}

// fieldOrder is the required row order inside each feature block table.
var fieldOrder = []string{
	"ID", "Tier", "Status", "Definition", "Frontend", "Routes",
	"Tables", "Flag", "Docs", "Smoke test", "Notes",
}

var (
	validTier   = map[string]bool{"core": true, "back-burner": true, "cut": true}
	validStatus = map[string]bool{"working": true, "partial": true, "placeholder": true, "stub": true}

	reBlock = regexp.MustCompile(`(?m)^## (.+?) \{#([a-z0-9-]+)\}\s*$`)
	reRow   = regexp.MustCompile(`(?m)^\| \*\*(.+?)\*\* \| (.*?) \|\s*$`)
	reTick  = regexp.MustCompile("`([^`]+)`")
)

// loadRegistry parses every hand-authored area file. A missing or
// empty registry directory is fine (Phase C hasn't run yet) — it
// returns no features and no findings.
func loadRegistry(dir string, routes []Route) ([]Feature, []finding) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}

	// Normalized route set for Routes-field validation.
	type rkey struct{ method, path string }
	routeSet := map[rkey]bool{}
	anyMethod := map[string]bool{}
	for _, rt := range routes {
		np := normalizePath(rt.Path)
		routeSet[rkey{rt.Method, np}] = true
		if rt.Method == "ANY" {
			anyMethod[np] = true
		}
	}
	routeExists := func(method, path string) bool {
		np := normalizePath(path)
		for k := range routeSet {
			if (k.method == method || k.method == "ANY") && pathsMatch(k.path, np) {
				return true
			}
		}
		return false
	}

	var features []Feature
	var findings []finding
	seenIDs := map[string]string{}

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") ||
			name == "README.md" || name == "api-coverage.md" {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(dir, name))
		if rerr != nil {
			findings = append(findings, finding{"warning", fmt.Sprintf("registry: cannot read %s: %v", name, rerr)})
			continue
		}
		src := string(data)

		blocks := reBlock.FindAllStringSubmatchIndex(src, -1)
		for bi, b := range blocks {
			title := src[b[2]:b[3]]
			id := src[b[4]:b[5]]
			end := len(src)
			if bi+1 < len(blocks) {
				end = blocks[bi+1][0]
			}
			body := src[b[1]:end]
			loc := fmt.Sprintf("%s {#%s}", name, id)

			if prev, dup := seenIDs[id]; dup {
				findings = append(findings, finding{"warning",
					fmt.Sprintf("registry: duplicate feature id %q in %s (already in %s)", id, name, prev)})
			}
			seenIDs[id] = name

			rows := reRow.FindAllStringSubmatch(body, -1)
			fields := map[string]string{}
			var order []string
			for _, r := range rows {
				fields[r[1]] = strings.TrimSpace(r[2])
				order = append(order, r[1])
			}

			// Field-order check: the block must contain exactly the
			// schema rows, in order.
			if strings.Join(order, "|") != strings.Join(fieldOrder, "|") {
				findings = append(findings, finding{"warning",
					fmt.Sprintf("registry %s: field rows must be exactly [%s] in order; got [%s]",
						loc, strings.Join(fieldOrder, ", "), strings.Join(order, ", "))})
			}

			ft := Feature{ID: id, Name: title, AreaFile: name,
				Tier: fields["Tier"], Status: fields["Status"], Flag: fields["Flag"]}

			if idField := strings.Trim(fields["ID"], "`"); idField != id {
				findings = append(findings, finding{"warning",
					fmt.Sprintf("registry %s: ID field %q does not match anchor {#%s}", loc, idField, id)})
			}
			if !validTier[ft.Tier] {
				findings = append(findings, finding{"warning",
					fmt.Sprintf("registry %s: Tier %q not in core|back-burner|cut", loc, ft.Tier)})
			}
			if !validStatus[ft.Status] {
				findings = append(findings, finding{"warning",
					fmt.Sprintf("registry %s: Status %q not in working|partial|placeholder|stub", loc, ft.Status)})
			}

			// Frontend paths must exist on disk (repo-relative).
			root := filepath.Dir(filepath.Dir(dir)) // dir = <root>/docs/feature-registry
			for _, m := range reTick.FindAllStringSubmatch(fields["Frontend"], -1) {
				p := m[1]
				ft.Frontend = append(ft.Frontend, p)
				if strings.ContainsAny(p, "*{") {
					continue
				}
				if _, serr := os.Stat(filepath.Join(root, filepath.FromSlash(p))); serr != nil {
					findings = append(findings, finding{"warning",
						fmt.Sprintf("registry %s: Frontend path %s does not exist", loc, p)})
				}
			}

			// Routes must exist in the extracted route table.
			if rv := fields["Routes"]; rv != "" && rv != "—" {
				for _, m := range reTick.FindAllStringSubmatch(rv, -1) {
					spec := strings.TrimSpace(m[1])
					ft.Routes = append(ft.Routes, spec)
					parts := strings.SplitN(spec, " ", 2)
					if len(parts) != 2 || !strings.HasPrefix(parts[1], "/") {
						findings = append(findings, finding{"warning",
							fmt.Sprintf("registry %s: Routes entry %q must be `METHOD /path`", loc, spec)})
						continue
					}
					if !routeExists(parts[0], parts[1]) {
						findings = append(findings, finding{"warning",
							fmt.Sprintf("registry %s: route %q not found in router.go", loc, spec)})
					}
				}
			}

			// A parked feature with a page must carry a flag, or it will
			// still be customer-visible at MVP.
			if ft.Tier == "back-burner" && (ft.Flag == "" || strings.HasPrefix(ft.Flag, "—")) {
				for _, p := range ft.Frontend {
					if strings.Contains(p, "/app/") {
						findings = append(findings, finding{"warning",
							fmt.Sprintf("registry %s: back-burner feature with page %s has no Flag — page stays reachable at MVP", loc, p)})
						break
					}
				}
			}

			features = append(features, ft)
		}
	}

	sort.Slice(features, func(i, j int) bool {
		if features[i].AreaFile != features[j].AreaFile {
			return features[i].AreaFile < features[j].AreaFile
		}
		return features[i].ID < features[j].ID
	})
	return features, findings
}

const (
	rollupBegin = "<!-- BEGIN GENERATED: rollup -->"
	rollupEnd   = "<!-- END GENERATED -->"
)

func renderRollup(features []Feature) string {
	var b strings.Builder
	b.WriteString(rollupBegin + "\n")
	b.WriteString(fmt.Sprintf("_%d features. Regenerate with `go run ./cmd/docgen -write`._\n\n", len(features)))
	b.WriteString("| Feature | ID | Area | Tier | Status |\n|---|---|---|---|---|\n")
	for _, f := range features {
		b.WriteString(fmt.Sprintf("| [%s](%s#%s) | `%s` | %s | %s | %s |\n",
			f.Name, f.AreaFile, f.ID, f.ID, strings.TrimSuffix(f.AreaFile, ".md"), f.Tier, f.Status))
	}
	b.WriteString(rollupEnd)
	return b.String()
}

// updateRollup rewrites the generated block inside README.md. Returns
// false with an error if the README or its markers don't exist yet —
// the Phase C author owns creating them.
func updateRollup(readmePath string, features []Feature) (bool, error) {
	if len(features) == 0 {
		return false, fmt.Errorf("no registry features parsed yet")
	}
	data, err := os.ReadFile(readmePath)
	if err != nil {
		return false, fmt.Errorf("README.md not found (registry not authored yet)")
	}
	src := string(data)
	bi := strings.Index(src, rollupBegin)
	ei := strings.Index(src, rollupEnd)
	if bi < 0 || ei < 0 || ei < bi {
		return false, fmt.Errorf("rollup markers missing in README.md")
	}
	out := src[:bi] + renderRollup(features) + src[ei+len(rollupEnd):]
	if out == src {
		return false, nil
	}
	return true, os.WriteFile(readmePath, []byte(out), 0o644)
}

func checkRollup(readmePath string, features []Feature) []finding {
	if len(features) == 0 {
		return nil
	}
	data, err := os.ReadFile(readmePath)
	if err != nil {
		return []finding{{"warning", "registry README.md missing while area files exist"}}
	}
	src := string(data)
	bi := strings.Index(src, rollupBegin)
	ei := strings.Index(src, rollupEnd)
	if bi < 0 || ei < 0 || ei < bi {
		return []finding{{"warning", "registry README.md is missing the generated rollup markers"}}
	}
	if src[bi:ei+len(rollupEnd)] != renderRollup(features) {
		return []finding{{"warning", "registry README.md rollup table is stale — run `go run ./cmd/docgen -write`"}}
	}
	return nil
}
