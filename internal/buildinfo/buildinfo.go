// Package buildinfo exposes build-time metadata injected via Go ldflags.
//
// The Dockerfile and the GitHub Actions build workflow both inject GitSHA
// at build time:
//
//	go build -ldflags "-X ironsight/internal/buildinfo.GitSHA=$(git rev-parse --short HEAD)" ...
//
// In local dev without ldflags, GitSHA is the empty string and the /api/health
// endpoint returns "" for the git_sha field — this is expected and not an error.
package buildinfo

// GitSHA is the short (7-char) git commit SHA baked in at build time.
// Populated by -ldflags "-X ironsight/internal/buildinfo.GitSHA=<sha>".
// Empty string when built without ldflags (local dev, go run, etc.).
var GitSHA string
