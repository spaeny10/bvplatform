import type { MetadataRoute } from 'next';
import { BRAND } from '@/lib/branding';

/**
 * PWA manifest driven by the branding module. A rebrand updates name,
 * short_name, description, and theme color in one place. When Next.js
 * serves /manifest.webmanifest this generates at build time — no runtime
 * cost, no chance of going stale like the old static manifest.json did.
 */
export default function manifest(): MetadataRoute.Manifest {
    return {
        name: BRAND.name,
        short_name: BRAND.shortName,
        description: BRAND.description,
        start_url: '/',
        display: 'standalone',
        // Dark background matches the operator console defaults. Light-theme
        // splash (if we ever add one) would override via the `background_color`
        // read from branding, but for now security-first dark is the sane default.
        background_color: '#0A0C10',
        theme_color: BRAND.colors.primary,
        orientation: 'any',
        icons: [
            { src: BRAND.icons.icon192, sizes: '192x192', type: 'image/png', purpose: 'maskable' },
            { src: BRAND.icons.icon512, sizes: '512x512', type: 'image/png', purpose: 'maskable' },
        ],
        categories: ['business', 'security'],
    };
}
