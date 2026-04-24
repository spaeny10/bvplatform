# Housekeeping — Rename & Theming Guide

This document covers two things that get asked a lot:

1. **How do I rename the product?** (When the final name is chosen.)
2. **How do I change the colors / theme?**

Both answers are "edit one file" if you follow the structure this document describes.

---

## 1. The rename checklist

When the product's final name is decided, the user-visible rename is a single-file change:

### 1.1 Edit [frontend/src/lib/branding.ts](../src/lib/branding.ts)

```ts
export const BRAND = {
    name:         'NewName',                      // was 'Ironsight'
    shortName:    'NewName',
    tagline:      'New tagline line',
    description:  'Longer SEO/PWA description...',
    homeUrl:      'https://newname.ai',
    supportEmail: 'support@newname.ai',
    noreplyEmail: 'noreply@newname.ai',
    colors: { /* only touch if the brand palette is also changing — see §2 */ },
    logoSrc: null,                                // set to '/logo.svg' to override with a static image
    icons: { /* only touch if you're shipping new PWA icons */ },
};
```

That one edit flows through to:

- **Every page title.** `layout.tsx` uses `{ template: \`%s — ${BRAND.name}\` }` so every sub-page inherits.
- **Meta description.** Pulled from `BRAND.description`.
- **PWA manifest.** [src/app/manifest.ts](../src/app/manifest.ts) is a Next.js dynamic manifest — it reads `BRAND` and Next serves it as `/manifest.webmanifest`. No static JSON to keep in sync.
- **Logo component.** [src/components/shared/Logo.tsx](../src/components/shared/Logo.tsx) renders `BRAND.name` as an SVG wordmark. The split point ("IRON | Sight") is detected automatically from the first internal capital letter, so "SkyWatch" or "BluVigil" all render correctly.
- **iOS home-screen install title.** `<meta name="apple-mobile-web-app-title">` in layout reads `BRAND.shortName`.
- **Apple `@ironsight.io` demo emails.** *Not* auto-updated — they're in [cmd/server/main.go](../../cmd/server/main.go) seed data. See 1.2.

### 1.2 Things that aren't wired to BRAND (change manually once)

| What | Where | Why not automated |
|---|---|---|
| Demo user emails (`@ironsight.io`) | [cmd/server/main.go](../../cmd/server/main.go) — seed block, 6 occurrences | Seed data only matters on first boot of a fresh DB. Fix once; existing installs are unaffected. |
| Documentation prose | `frontend/Documents/*.md` | Docs say "Ironsight does X". Find-replace at rename time. |
| Go module name (`onvif-tool`) | [go.mod](../../go.mod) | Internal identifier, zero user impact. Renaming would churn every `import` statement. Don't. |
| DB name (`onvif_tool`) | [.env.example](../../.env.example), [docker-compose.yml](../../docker-compose.yml) | Renaming requires a DB migration for existing deployments. Pick one at release and stick with it. |
| Container names (`ironsight-*`) | [docker-compose.yml](../../docker-compose.yml) | Parameterize via `COMPOSE_PROJECT_NAME=newname` in `.env` — containers become `newname-api`, etc. No code change. |
| `localStorage` keys (`ironsight_token`) | scattered across the frontend | Renaming these logs every user out. Leave alone. |

### 1.3 Asset replacement

Replace these files in [frontend/public/](../public/) with equivalents of the same dimensions:

- `favicon.ico` — 32×32 or multi-res
- `icon-192.png` — 192×192 PWA icon
- `icon-512.png` — 512×512 PWA icon

The logo inside the app (`<Logo>`) uses generated SVG text by default. If you want a **fully custom logo** (pictorial, not just the name as text), drop an SVG at `/public/logo.svg` and set:

```ts
// branding.ts
logoSrc: '/logo.svg',
```

### 1.4 Checklist

```
□ Edit frontend/src/lib/branding.ts — name, shortName, tagline, description, emails, URL
□ Replace frontend/public/favicon.ico, icon-192.png, icon-512.png
□ (Optional) Drop /public/logo.svg and set BRAND.logoSrc
□ If brand colors changed: update BRAND.colors — see §2
□ Find-replace demo-data emails in cmd/server/main.go (6 hits)
□ Set COMPOSE_PROJECT_NAME=newname in .env
□ Find-replace docs: Ironsight_Architecture.md, MasterDeployment.md, MobileAppPlan.md, this file
□ npx tsc --noEmit   (should pass)
□ npx next build     (should pass)
```

Total time to rename, including asset swap: **under 30 minutes.**

---

## 2. The theming system

### 2.1 Three layers, clearly separated

| Layer | Where | What it controls |
|---|---|---|
| **Brand tokens** | `BRAND.colors` in [branding.ts](../src/lib/branding.ts) | Primary / secondary / tertiary — the three colors that read as "the brand" |
| **Canonical design tokens** | `:root` block in [globals.css](../src/app/globals.css) | Surfaces, text, semantic accents (green/red/blue), borders, shadows, radius, fonts, easing — full design system |
| **Scoped palettes** | `.portal-shell` in [portal.css](../src/app/portal/portal.css); `.sg-*` in [operator.css](../src/app/operator/operator.css) | Per-feature deviations — e.g., the customer portal's warm-cream light theme |

### 2.2 The cascade

```
BRAND.colors (branding.ts)
    ↓ injected via <style> in layout.tsx
--brand-primary, --brand-secondary, --brand-tertiary  (CSS vars on :root)
    ↓ aliased in globals.css
--accent-orange, --accent-crimson, --accent-amber
    ↓ referenced by
Every component styled with var(--accent-*)
```

Change `BRAND.colors.primary = '#0066CC'` and every CTA button, active tab, and logo dot in the app turns blue — without touching a stylesheet.

### 2.3 Light vs dark mode

**Dark mode is the canonical default.** Every `:root` rule in [globals.css](../src/app/globals.css) defines the dark palette. This matches the operator console, admin panel, and analytics pages — security-console aesthetic.

**Light mode is prepared but not activated globally.** There's a `[data-theme="light"]` block in globals.css with a full light-mode palette (bg, text, accents, borders, shadows). To activate a page in light mode:

```tsx
<html data-theme="light">
// or scoped:
<div data-theme="light"> ... </div>
```

**The customer portal is a special case.** It carries its own warm/cream palette scoped to `.portal-shell` in [portal.css](../src/app/portal/portal.css) — an intentional design distinct from just "light mode with the same colors". Don't collapse the portal's tokens into the canonical set; they're separate by design.

To add a theme toggle UI later: it writes `data-theme` to `<html>` and persists the choice in localStorage. Small component, ~30 lines.

### 2.4 Semantic accent colors (don't track the brand)

These stay fixed regardless of the brand:

- `--accent-green` — online / healthy status
- `--accent-red` — offline / critical status
- `--accent-blue` — active selection / info
- `--accent-purple` — analytics / AI-generated labels
- `--accent-cyan` — active border highlights

A rebrand shouldn't turn the "online" dot from green to the brand color — the semantic meaning of green here is universal. Keep these as-is in globals.css.

---

## 3. When adding a new component

Follow this pattern so future rebrands don't touch your code:

```tsx
// ✓ Good — uses tokens
<div style={{
  background: 'var(--bg-card)',
  border: '1px solid var(--border-color)',
  color: 'var(--text-primary)',
  borderRadius: 'var(--radius-md)',
}}>

// ✗ Bad — hardcoded hex. Will be missed by the rebrand.
<div style={{
  background: '#12161E',
  border: '1px solid rgba(255,255,255,0.07)',
  color: '#E4E8F0',
  borderRadius: 6,
}}>
```

For the brand color specifically, use `var(--brand-primary)` (or its alias `var(--accent-orange)`) so brand changes flow through.

---

## 4. Known leftovers (intentional)

These places look like rebrand targets but are intentionally hardcoded:

| File | What | Why leave it |
|---|---|---|
| [EvidenceExportModal.tsx](../src/components/operator/EvidenceExportModal.tsx) | `font-family: 'Segoe UI'` + `#1a1a2e` text on white bg, inside an injected print HTML | That's a print-report template — dark-on-white by design, Segoe is the standard Windows print sans-serif. Don't theme it. |
| [portal.css](../src/app/portal/portal.css) light-mode `--accent: #c84b2f` | Slightly more terracotta than the brand orange | Tuned for contrast against the warm cream surface — if the brand is drastically rechosen, this should be re-tuned by hand. |
| [Logo.tsx](../src/components/shared/Logo.tsx) `fill="#B0B4BA"` | Letter color on the wordmark | Neutral grey chosen to work on both dark and light backgrounds without reading as "branded". |

---

## 5. Running the build

After any branding or theme change:

```bash
cd frontend
npx tsc --noEmit    # type-check
npx next build      # production build
```

Both should pass clean. If the build complains about `manifest.webmanifest`, you probably accidentally re-introduced the static `public/manifest.json` — delete it; the dynamic `src/app/manifest.ts` is authoritative.
