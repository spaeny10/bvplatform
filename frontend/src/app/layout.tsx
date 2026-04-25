import './globals.css';
import './premium-animations.css';
import type { Metadata } from 'next';
import { AuthProvider } from '@/contexts/AuthContext';
import { allFontClasses } from '@/lib/fonts';
import { QueryProvider } from '@/lib/query-provider';
import { I18nProvider } from '@/hooks/useI18n';
import { SkipToContent } from '@/hooks/useAccessibility';
import SessionWarningWrapper from '@/components/shared/SessionWarningWrapper';
import ErrorBoundary from '@/components/shared/ErrorBoundary';
import PWAManager from '@/components/shared/PWAManager';
import { BRAND, brandCssVars } from '@/lib/branding';

// All user-visible branding flows from src/lib/branding.ts. The page title,
// meta description, PWA manifest (src/app/manifest.ts), and the --brand-*
// CSS vars injected below all read from the same BRAND constant. Rebranding
// is a one-file change.
export const metadata: Metadata = {
    title: { default: `${BRAND.name} — ${BRAND.tagline}`, template: `%s — ${BRAND.name}` },
    description: BRAND.description,
    applicationName: BRAND.name,
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
    return (
        <html lang="en" className={allFontClasses} style={{ [brandCssVarKey()]: 'initial' } as React.CSSProperties}>
            <head>
                <meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no" />
                {/* Theme-color matches the dark operator shell — the portal overrides with its warm palette via portal.css. */}
                <meta name="theme-color" content="#0A0C10" />
                {/* Brand CSS vars — read by globals.css and all scoped stylesheets.
                    Injecting here so a rebrand (edit BRAND.colors in branding.ts)
                    flows through to every `var(--brand-*)` reference without a
                    component-by-component rewrite. */}
                <style
                    // eslint-disable-next-line react/no-danger
                    dangerouslySetInnerHTML={{ __html: `:root { ${brandCssVars()} }` }}
                />
                {/* Modern spelling + Apple legacy spelling so iOS home-screen installs behave. */}
                <meta name="mobile-web-app-capable" content="yes" />
                <meta name="apple-mobile-web-app-capable" content="yes" />
                <meta name="apple-mobile-web-app-status-bar-style" content="black-translucent" />
                <meta name="apple-mobile-web-app-title" content={BRAND.shortName} />
            </head>
            <body>
                <QueryProvider>
                    <AuthProvider>
                        <I18nProvider>
                            <ErrorBoundary>
                                <SkipToContent />
                                <main id="main-content">
                                    {children}
                                </main>
                                <SessionWarningWrapper />
                                {/* PWAManager registers the service worker and
                                    surfaces a brand-styled install prompt on
                                    eligible mobile viewports. Renders nothing
                                    on desktop / installed-PWA / dismissed. */}
                                <PWAManager />
                            </ErrorBoundary>
                        </I18nProvider>
                    </AuthProvider>
                </QueryProvider>
            </body>
        </html>
    );
}

// Satisfy TypeScript's CSSProperties indexer for the custom-property placeholder
// on <html>. The real values come from the inline <style> block above; this
// empty placeholder just exists so React doesn't complain about the attribute.
function brandCssVarKey(): string {
    return '--brand-initialized';
}
