# ── Stage 1: deps ──────────────────────────────────────────────
# Using the alpine image keeps layers small; we don't need glibc.
FROM docker.io/library/node:20-alpine AS deps
WORKDIR /app

COPY package.json package-lock.json* ./
# Use ci when a lockfile is present (reproducible), fall back to install.
RUN if [ -f package-lock.json ]; then npm ci; else npm install; fi

# ── Stage 2: build ─────────────────────────────────────────────
FROM docker.io/library/node:20-alpine AS build
WORKDIR /app

COPY --from=deps /app/node_modules ./node_modules
COPY . .

# Next.js bakes static rewrites into .next/routes-manifest.json at build
# time. next.config.js reads process.env.API_INTERNAL_URL to decide where
# /api, /auth, /ws, etc. get proxied. If the ARG isn't set, the fallback
# (localhost:8080) gets compiled in, and the frontend container then
# tries to reach the api on ITS OWN loopback — which always fails.
# Compose passes this as a build arg (see docker-compose.yml frontend
# build.args); for native dev builds it stays empty and localhost wins.
ARG API_INTERNAL_URL=""
ENV API_INTERNAL_URL=${API_INTERNAL_URL}

# Production build. next.config.js 'output: standalone' is strongly
# recommended for container deploys — it bundles everything needed into
# .next/standalone so we don't ship node_modules at runtime. If that
# flag isn't set yet, this still works but the runtime image gets larger.
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

# ── Stage 3: runtime ───────────────────────────────────────────
FROM docker.io/library/node:20-alpine AS runtime

RUN addgroup -S -g 10002 ironsight && \
    adduser  -S -u 10002 -G ironsight ironsight

WORKDIR /app

# Copy only what's needed for runtime. If the build is using Next's
# standalone output, the files below work as-is; if not, you'll want to
# copy node_modules too (and node_modules weights ~400MB).
COPY --from=build --chown=ironsight:ironsight /app/public ./public
COPY --from=build --chown=ironsight:ironsight /app/.next ./.next
COPY --from=build --chown=ironsight:ironsight /app/package.json ./package.json

# Pull node_modules from deps so `next start` has what it needs without
# the full build toolchain tagging along. Replace with a `.next/standalone`
# copy once the app opts into standalone output.
COPY --from=deps --chown=ironsight:ironsight /app/node_modules ./node_modules

USER ironsight

ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3000

EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD wget --spider --quiet http://127.0.0.1:3000 || exit 1

CMD ["npm", "run", "start"]
