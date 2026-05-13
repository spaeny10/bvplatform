'use client';

import { useRef, useEffect, useState } from 'react';
import Hls from 'hls.js';
import { resolveMediaURL, createMediaRefresher } from '@/lib/media';

interface Props {
  src?: string;
  poster?: string;
  autoPlay?: boolean;
  muted?: boolean;
  style?: React.CSSProperties;
  onError?: () => void;
}

// matchLegacyHLS recognises the historical `/hls/<cam>/<file.m3u8>` URL
// shape used by callers that haven't been ported to mintMediaToken yet.
// Returning { cam, file } triggers the auto-refresh path that re-mints
// the playlist token every ~4 minutes (TTL is 5 min). Anything else is
// returned as a plain string and used as-is.
function matchLegacyHLS(src: string): { cam: string; file: string } | null {
  const m = src.match(/^\/hls\/([^/]+)\/([^/?#]+)$/);
  if (!m) return null;
  return { cam: m[1], file: m[2] };
}

export default function HLSVideoPlayer({ src, poster, autoPlay = true, muted = true, style, onError }: Props) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const hlsRef = useRef<Hls | null>(null);
  const [status, setStatus] = useState<'loading' | 'playing' | 'error' | 'no-src'>(!src ? 'no-src' : 'loading');
  // resolvedSrc is the actual /media/v1/<token> URL we feed into the
  // video element / hls.js. For legacy /hls/ inputs it's re-minted on a
  // 4-min cadence so the parent playlist token never expires mid-stream.
  const [resolvedSrc, setResolvedSrc] = useState<string>('');

  // Resolve + (optionally) auto-refresh the source URL.
  useEffect(() => {
    if (!src) {
      setResolvedSrc('');
      return;
    }
    let cancelled = false;
    const legacy = matchLegacyHLS(src);
    if (legacy) {
      // Auto-refresh path. createMediaRefresher schedules its own
      // ticks; the first onRefresh fires synchronously after the
      // first mint completes.
      const refresher = createMediaRefresher(
        { camera_id: legacy.cam, kind: 'hls', path: legacy.file },
        (url) => { if (!cancelled) setResolvedSrc(url); },
      );
      refresher.start();
      return () => { cancelled = true; refresher.dispose(); };
    }
    // Non-/hls/ input — single-shot resolve. resolveMediaURL handles
    // both already-signed /media/v1/ URLs (passthrough) and legacy
    // /recordings/ / /snapshots/ URLs (one-shot mint).
    resolveMediaURL(src).then(u => { if (!cancelled) setResolvedSrc(u); });
    return () => { cancelled = true; };
  }, [src]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !resolvedSrc) {
      setStatus(src ? 'loading' : 'no-src');
      return;
    }

    setStatus('loading');

    // Native HLS support (Safari)
    if (video.canPlayType('application/vnd.apple.mpegurl')) {
      video.src = resolvedSrc;
      video.addEventListener('loadedmetadata', () => {
        if (autoPlay) video.play().catch(() => {});
        setStatus('playing');
      });
      video.addEventListener('error', () => {
        setStatus('error');
        onError?.();
      });
      return;
    }

    // hls.js for other browsers
    if (Hls.isSupported()) {
      const hls = new Hls({
        enableWorker: true,
        lowLatencyMode: true,
        backBufferLength: 30,
      });
      hlsRef.current = hls;

      hls.loadSource(resolvedSrc);
      hls.attachMedia(video);

      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        if (autoPlay) video.play().catch(() => {});
        setStatus('playing');
      });

      hls.on(Hls.Events.ERROR, (_event, data) => {
        if (data.fatal) {
          setStatus('error');
          onError?.();
          if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
            hls.startLoad();
          } else {
            hls.destroy();
          }
        }
      });

      return () => {
        hls.destroy();
        hlsRef.current = null;
      };
    }

    setStatus('error');
  }, [resolvedSrc, autoPlay, onError, src]);

  return (
    <div style={{ position: 'relative', width: '100%', height: '100%', background: '#000', ...style }}>
      <video
        ref={videoRef}
        muted={muted}
        playsInline
        poster={poster}
        style={{ width: '100%', height: '100%', objectFit: 'cover', display: status === 'no-src' ? 'none' : 'block' }}
      />
      {status === 'no-src' && (
        <div style={{
          position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center',
          color: '#4A5268', fontSize: 10, fontFamily: "'JetBrains Mono', monospace",
        }}>
          No stream available
        </div>
      )}
      {status === 'loading' && (
        <div style={{
          position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center',
          background: 'rgba(0,0,0,0.5)',
        }}>
          <div style={{
            width: 24, height: 24, border: '2px solid rgba(0,212,255,0.2)',
            borderTopColor: '#E8732A', borderRadius: '50%',
            animation: 'spin 0.8s linear infinite',
          }} />
        </div>
      )}
      {status === 'error' && (
        <div style={{
          position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center',
          flexDirection: 'column', gap: 8, padding: 20, textAlign: 'center',
        }}>
          <span style={{ fontSize: 28, opacity: 0.6 }}>📡</span>
          <div style={{ fontSize: 11, fontWeight: 600, color: '#E89B2A' }}>
            Site Connection Lost
          </div>
          <div style={{ fontSize: 10, color: '#4A5268', lineHeight: 1.5, maxWidth: 240 }}>
            Evidence is safely bookmarked on the local NVR and will be viewable when connection is restored.
          </div>
          <button
            onClick={() => { if (src) { setStatus('loading'); /* triggers re-render with useEffect */ } }}
            style={{
              marginTop: 4, padding: '5px 14px', borderRadius: 4, fontSize: 10, fontWeight: 600,
              background: 'rgba(0,212,255,0.08)', border: '1px solid rgba(0,212,255,0.2)',
              color: '#E8732A', cursor: 'pointer', fontFamily: 'inherit',
            }}
          >
            ↻ Retry Connection
          </button>
        </div>
      )}
    </div>
  );
}
