// docgen extracts the backend route table and the frontend API-call
// surface, cross-references them, and maintains the generated parts of
// docs/feature-registry/ (api-coverage.md + the rollup table in
// README.md). It also lints hand-authored feature-registry blocks.
//
// Modes:
//
//	go run ./cmd/docgen -write    regenerate generated files in place
//	go run ./cmd/docgen -check    regenerate in memory, warn on drift +
//	                              registry lint findings; always exit 0
//	go run ./cmd/docgen -check -strict   same but exit 1 on any finding
//
// Run from the repo root (CI does). -root overrides for local use.
//
// Why static extraction instead of chi.Walk at runtime: NewRouter
// requires live dependencies (DB pool, recording engine, a media
// auditor that starts goroutines in the constructor), so importing it
// from a tool would need the full stack. router.go is a single file of
// literal registrations — the AST walk in routes.go covers it exactly.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

type finding struct {
	level string // "warning" | "error"
	msg   string
}

func main() {
	var (
		root   = flag.String("root", ".", "repo root (containing go.mod)")
		write  = flag.Bool("write", false, "write generated files in place")
		check  = flag.Bool("check", false, "verify generated files are current + lint registry")
		strict = flag.Bool("strict", false, "with -check: exit 1 on findings instead of warn-only")
	)
	flag.Parse()

	if !*write && !*check {
		fmt.Fprintln(os.Stderr, "docgen: pass -write or -check")
		os.Exit(2)
	}

	routerPath := filepath.Join(*root, "internal", "api", "router.go")
	frontendSrc := filepath.Join(*root, "frontend", "src")
	registryDir := filepath.Join(*root, "docs", "feature-registry")

	routes, err := extractRoutes(routerPath)
	if err != nil {
		fatal("route extraction: %v", err)
	}
	calls, err := extractFrontendCalls(frontendSrc)
	if err != nil {
		fatal("frontend extraction: %v", err)
	}
	features, lintFindings := loadRegistry(registryDir, routes)

	cov := crossReference(routes, calls, features)
	coverageMD := renderCoverage(cov)

	var findings []finding
	findings = append(findings, lintFindings...)

	coveragePath := filepath.Join(registryDir, "api-coverage.md")
	readmePath := filepath.Join(registryDir, "README.md")

	if *write {
		if err := os.MkdirAll(registryDir, 0o755); err != nil {
			fatal("mkdir: %v", err)
		}
		if err := os.WriteFile(coveragePath, []byte(coverageMD), 0o644); err != nil {
			fatal("write %s: %v", coveragePath, err)
		}
		fmt.Printf("docgen: wrote %s (%d routes, %d frontend calls, %d features)\n",
			coveragePath, len(routes), len(calls), len(features))
		if updated, err := updateRollup(readmePath, features); err != nil {
			fmt.Printf("docgen: rollup skipped: %v\n", err)
		} else if updated {
			fmt.Printf("docgen: updated rollup table in %s\n", readmePath)
		}
	}

	if *check {
		if onDisk, err := os.ReadFile(coveragePath); err != nil {
			findings = append(findings, finding{"warning",
				fmt.Sprintf("%s missing — run `go run ./cmd/docgen -write`", rel(*root, coveragePath))})
		} else if string(onDisk) != coverageMD {
			findings = append(findings, finding{"warning",
				fmt.Sprintf("%s is stale — run `go run ./cmd/docgen -write` and commit", rel(*root, coveragePath))})
		}
		findings = append(findings, checkRollup(readmePath, features)...)

		for _, fc := range cov.frontendOnly {
			findings = append(findings, finding{"warning",
				fmt.Sprintf("frontend calls %s %s (%s:%d) but no backend route matches — likely 404 at runtime",
					fc.Method, fc.Path, fc.File, fc.Line)})
		}

		emit(findings)
		if *strict && len(findings) > 0 {
			os.Exit(1)
		}
	}
}

func emit(findings []finding) {
	gha := os.Getenv("GITHUB_ACTIONS") == "true"
	for _, f := range findings {
		if gha {
			fmt.Printf("::%s ::docgen: %s\n", f.level, f.msg)
		} else {
			fmt.Printf("docgen %s: %s\n", f.level, f.msg)
		}
	}
	if len(findings) == 0 {
		fmt.Println("docgen: clean — generated docs current, registry valid")
	} else {
		fmt.Printf("docgen: %d finding(s)\n", len(findings))
	}
}

func rel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(p)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "docgen: "+format+"\n", args...)
	os.Exit(2)
}
