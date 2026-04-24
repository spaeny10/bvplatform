/**
 * Brand identity — single source of truth for everything customer-visible
 * that names or themes the product. Change these constants to rebrand the
 * entire app. CSS tokens in globals.css mirror BRAND.colors so the same
 * change flows to component styling.
 *
 * What lives here:
 *   - Product name + taglines + descriptions
 *   - Brand colors (the 3 that actually drive the identity)
 *   - Contact / support strings
 *
 * What does NOT live here:
 *   - File paths, module names, Go package names, DB names — those are
 *     internal identifiers with zero user impact; renaming them is churn.
 *   - Route paths like /admin, /operator — those are product concepts, not
 *     brand decisions.
 *
 * See frontend/Documents/HOUSEKEEPING.md for the full rename checklist.
 */

export const BRAND = {
    // ── Identity ───────────────────────────────────────────────
    /** Full product name. Shown in page titles, headers, footers. */
    name: 'Ironsight',

    /** Short form used where space is tight — tab titles, mobile, PWA short_name. */
    shortName: 'Ironsight',

    /** One-line positioning statement. Shown under the logo on the login + evidence pages. */
    tagline: 'Construction Intelligence Platform',

    /**
     * Long-form description. Goes into the HTML <meta name="description">,
     * PWA manifest description, and the OG card if we ever add social sharing.
     */
    description:
        'Real-time construction site security monitoring, safety compliance, and AI-powered video intelligence.',

    /** Marketing homepage — where the logo links when clicked from a public page. */
    homeUrl: 'https://ironsight.ai',

    // ── Support / contact ──────────────────────────────────────
    /** Generic support email. Used in error pages, evidence downloads, "Contact us" footers. */
    supportEmail: 'support@ironsight.ai',

    /** Used in auto-generated emails and demo seed data. */
    noreplyEmail: 'noreply@ironsight.ai',

    // ── Colors ─────────────────────────────────────────────────
    // These three drive the visual identity. Change them and the rest of
    // the design system (bg, text, neutrals) is unaffected — the accent
    // highlights are what the eye reads as "the brand".
    colors: {
        /** Primary brand accent — CTAs, active nav, focus rings. */
        primary: '#E8732A', // orange

        /** Secondary — complements primary for two-tone elements (logo dots, gradients). */
        secondary: '#B22234', // crimson

        /** Tertiary — rounds out the logo palette, also used for warnings. */
        tertiary: '#E89B2A', // amber
    },

    // ── Assets ─────────────────────────────────────────────────
    /**
     * Logo SVG is rendered by <Logo> component so the word stays driven by
     * BRAND.name. Only change `logoSrc` if you want to swap for a static
     * image (e.g., a custom customer logo). See components/shared/Logo.tsx.
     */
    logoSrc: null as string | null, // null = use the TSX component

    /** PWA manifest + favicon assets. These files live in frontend/public/. */
    icons: {
        favicon: '/favicon.ico',
        icon192: '/icon-192.png',
        icon512: '/icon-512.png',
    },
} as const;

/**
 * Document title helper — standardises how we format per-page titles.
 * Usage in layout.tsx: title: pageTitle('Admin')  →  "Admin — Ironsight"
 */
export function pageTitle(page: string): string {
    return `${page} — ${BRAND.name}`;
}

/**
 * CSS custom-property string injected into <html> at render time. Keeps
 * the three brand colors reachable from any CSS / inline style without
 * reimporting the TS module. See layout.tsx where this is applied.
 */
export function brandCssVars(): string {
    return [
        `--brand-primary: ${BRAND.colors.primary};`,
        `--brand-secondary: ${BRAND.colors.secondary};`,
        `--brand-tertiary: ${BRAND.colors.tertiary};`,
    ].join(' ');
}
