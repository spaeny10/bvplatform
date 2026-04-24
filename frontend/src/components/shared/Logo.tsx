'use client';

import { BRAND } from '@/lib/branding';

/**
 * Brand wordmark. The letters come from BRAND.name (so renaming the product
 * renames the logo), the three dots come from BRAND.colors. Designed as
 * inline SVG for crisp scaling at any height.
 *
 * If you ever need a fully custom logo that isn't just the product name as
 * text, set BRAND.logoSrc in branding.ts to a /public/* image path and
 * this component falls back to <img>.
 */
interface Props {
    height?: number;
    style?: React.CSSProperties;
}

export default function Logo({ height = 24, style }: Props) {
    // Static image path overrides the generated wordmark.
    if (BRAND.logoSrc) {
        return <img src={BRAND.logoSrc} alt={BRAND.name} height={height} style={style} />;
    }

    // Generated wordmark: brand name as bold text + three accent dots floating
    // above the characters. The split point is the first uppercase letter
    // after the first two characters — matches the old "IRON | Sight" rhythm
    // without hardcoding anything. If BRAND.name has no such split, dots sit
    // above the last quarter of the word.
    const name = BRAND.name;
    const splitPoint = findSplitPoint(name);
    const before = name.slice(0, splitPoint);
    const after = name.slice(splitPoint);

    // Rough char-width metrics — inline SVG doesn't give us real text
    // metrics, so we approximate from the 260×44 viewBox that the old logo
    // used. Tuned so names from 6 to 12 chars all render cleanly.
    const charWidth = 22;
    const totalWidth = name.length * charWidth + 40; // +40 padding for dots
    const scale = height / 44;
    const width = Math.round(totalWidth * scale);

    // Dot column sits just above the split — floats over the first
    // character of `after` for the classic IRON|Sight effect.
    const dotX = before.length * charWidth + 6;

    return (
        <svg
            width={width}
            height={height}
            viewBox={`0 0 ${totalWidth} 44`}
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
            style={style}
            aria-label={BRAND.name}
        >
            <text
                x="0"
                y="36"
                fontFamily="'Inter', 'Arial Black', sans-serif"
                fontWeight="800"
                fontSize="38"
                letterSpacing="-1"
                fill="#B0B4BA"
            >
                {before}
            </text>
            <text
                x={before.length * charWidth}
                y="36"
                fontFamily="'Inter', 'Arial Black', sans-serif"
                fontWeight="800"
                fontSize="38"
                letterSpacing="-1"
                fill="#B0B4BA"
            >
                {after}
            </text>

            {/* Three brand dots. Colors come from BRAND so a rebrand sweeps them. */}
            <circle cx={dotX} cy="4.5" r="4.5" fill={BRAND.colors.secondary} />
            <circle cx={dotX + 10} cy="4.5" r="4.5" fill={BRAND.colors.primary} />
            <circle cx={dotX + 20} cy="4.5" r="4.5" fill={BRAND.colors.tertiary} />
        </svg>
    );
}

/**
 * LogoIcon — compact square for favicon-sized slots (mobile nav, badges).
 * Uses the first letter of BRAND.shortName inside a gradient tile.
 */
export function LogoIcon({ size = 28, style }: { size?: number; style?: React.CSSProperties }) {
    const c = BRAND.colors;
    return (
        <div
            style={{
                width: size,
                height: size,
                borderRadius: size * 0.2,
                background: `linear-gradient(135deg, ${c.primary}, ${c.tertiary})`,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                boxShadow: `0 0 12px ${c.primary}4d`,
                position: 'relative',
                ...style,
            }}
        >
            <div
                style={{
                    display: 'flex',
                    gap: size * 0.06,
                    position: 'absolute',
                    top: size * 0.12,
                }}
            >
                <div
                    style={{
                        width: size * 0.12,
                        height: size * 0.12,
                        borderRadius: '50%',
                        background: c.secondary,
                    }}
                />
                <div
                    style={{
                        width: size * 0.12,
                        height: size * 0.12,
                        borderRadius: '50%',
                        background: '#fff',
                    }}
                />
                <div
                    style={{
                        width: size * 0.12,
                        height: size * 0.12,
                        borderRadius: '50%',
                        background: c.tertiary,
                    }}
                />
            </div>
        </div>
    );
}

/**
 * findSplitPoint looks for an internal capital letter (camel-case style)
 * so names like "SiteGuard" or "IronSight" split at the seam. For lowercase
 * or single-word names, it splits at ¾ of the length to keep dots visible.
 */
function findSplitPoint(name: string): number {
    for (let i = 1; i < name.length; i++) {
        const ch = name[i];
        if (ch >= 'A' && ch <= 'Z') {
            return i;
        }
    }
    return Math.max(1, Math.floor(name.length * 0.5));
}
