// ── Ironsight Font Registry ──
// Unified font stack: Inter (body) + JetBrains Mono (data/monospace)

import { Inter, JetBrains_Mono } from 'next/font/google';

export const inter = Inter({
  subsets: ['latin'],
  weight: ['300', '400', '500', '600', '700', '800'],
  display: 'swap',
  variable: '--font-inter',
});

export const jetbrainsMono = JetBrains_Mono({
  subsets: ['latin'],
  weight: ['300', '400', '500', '600'],
  display: 'swap',
  variable: '--font-jetbrains',
});

// All fonts combined (for root layout)
export const allFontClasses = [
  inter.variable,
  jetbrainsMono.variable,
].join(' ');
