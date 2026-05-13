// P1-A-03 — a thin <img> wrapper that auto-resolves legacy media URL
// shapes (/snapshots/<cam>/<file>, /recordings/<cam>/<file>) through
// the /api/media/mint endpoint before rendering. UI surfaces that
// render a stored snapshot_url / clip_url field from the API just
// swap `<img src={...}>` for `<SignedImage src={...} />` and the
// component handles the rest.
//
// Pass-through behaviour: already-signed /media/v1/ URLs and absolute
// https:// URLs are used as-is.

'use client';

import { useEffect, useState } from 'react';
import { resolveMediaURL } from '@/lib/media';

interface Props extends Omit<React.ImgHTMLAttributes<HTMLImageElement>, 'src'> {
  src?: string | null;
  /** When the resolve fails (cross-tenant 404, expired token, etc.),
   *  the component hides itself. Set this to render a fallback node
   *  in that slot instead. */
  fallback?: React.ReactNode;
}

export default function SignedImage({ src, fallback = null, ...rest }: Props) {
  const [resolved, setResolved] = useState<string>('');
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    if (!src) {
      setResolved('');
      setFailed(true);
      return;
    }
    let cancelled = false;
    setFailed(false);
    resolveMediaURL(src).then(u => {
      if (cancelled) return;
      if (!u) {
        setFailed(true);
        return;
      }
      setResolved(u);
    }).catch(() => { if (!cancelled) setFailed(true); });
    return () => { cancelled = true; };
  }, [src]);

  if (failed || !resolved) {
    return <>{fallback}</>;
  }
  return <img src={resolved} {...rest} />;
}
