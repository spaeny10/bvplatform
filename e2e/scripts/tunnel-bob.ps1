# Opens the SSH tunnel the smoke tests run through.
#
# Why a tunnel at all: the public Ironsight URL sits behind oauth2-proxy
# (Google SSO) — Playwright can't drive that login. Hitting bob's port 3000
# directly gives the app-native /auth/login form instead. The Next.js
# server rewrites /auth, /api and /hls to the Go API same-origin, so the
# whole app works through this single forwarded port.
#
# Known gap: /ws does NOT proxy cleanly through the tunnel — an expected
# (and allowlisted) WebSocket console error appears on authenticated pages.
#
# bob is only reachable from fred, hence the jump through the `fred` SSH
# alias. Keep this window open while tests run; Ctrl+C to close.
ssh -N -L 13000:192.168.103.48:3000 fred
