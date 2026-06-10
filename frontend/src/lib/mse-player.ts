// Phase 1a low-latency live view (low-latency-live-view-go2rtc.md):
// MSE-over-WebSocket client for the go2rtc sidecar, proxied through
// /api/live2/{cameraID}/ws (auth = session cookie, same as the HLS path).
//
// Protocol (go2rtc MSE mode):
//   1. Open the WS. go2rtc sends a JSON control frame announcing the
//      stream; the first message we care about is {type:"mse", value:"<codecs string>"}.
//   2. We create a MediaSource + SourceBuffer with that exact codecs
//      string, reply {type:"mse", value:"<codecs we support>"} is NOT
//      required — go2rtc starts the fMP4 byte stream once the WS is open
//      with ?src=. Binary frames after the init segment are appended to
//      the SourceBuffer in order.
//   3. The first binary frame is the fMP4 init segment (ftyp+moov);
//      subsequent frames are media (moof+mdat).
//
// HEVC / hvcC note (mirrors live_proxy.go patchHVCCCompleteness): the HLS
// path byte-patches the hvcC box's array_completeness bit because Chromium
// MSE rejects hvc1 sample entries with incomplete NAL arrays. go2rtc's MSE
// muxer is generally believed to emit complete arrays, but this is NOT yet
// verified on the Milesight HEVC substream on real hardware.
// TODO(bench): on bob, if Chromium logs
// `manifestIncompatibleCodecsError`/`bufferIncompatibleCodecs` on the first
// init segment, apply the same array_completeness patch to the init
// segment here (see patchInitSegmentHvcc below — wired but a no-op until a
// bench run confirms it's needed).

export interface MsePlayerHandle {
  /** Tear down the WS + MediaSource and detach from the <video>. */
  close: () => void;
}

export interface MsePlayerOptions {
  /** Called on a fatal error (WS closed unexpectedly, MSE rejected codec). */
  onError?: (message: string, detail?: string) => void;
  /** Called once the first media has been appended and playback can start. */
  onPlaying?: () => void;
}

/**
 * isMseSupported reports whether this browser can run the MSE-over-WS path
 * at all. The caller should fall back to hls.js when this is false.
 */
export function isMseSupported(): boolean {
  return (
    typeof window !== 'undefined' &&
    'MediaSource' in window &&
    typeof MediaSource.isTypeSupported === 'function'
  );
}

/**
 * startMsePlayer opens /api/live2/{cameraId}/ws and feeds the go2rtc fMP4
 * byte stream into a MediaSource attached to `video`. Returns a handle whose
 * close() tears everything down. The session cookie auths the WS (same-origin
 * upgrade), so no token plumbing is needed — identical posture to the HLS path.
 */
export function startMsePlayer(
  video: HTMLVideoElement,
  cameraId: string,
  opts: MsePlayerOptions = {},
): MsePlayerHandle {
  let closed = false;
  let ws: WebSocket | null = null;
  let mediaSource: MediaSource | null = null;
  let sourceBuffer: SourceBuffer | null = null;
  // Pending media chunks waiting for the SourceBuffer to finish its previous
  // appendBuffer (updateend). SourceBuffer.appendBuffer throws if called while
  // updating, so we queue and drain.
  const queue: ArrayBuffer[] = [];
  let initAppended = false;

  const fail = (message: string, detail?: string) => {
    if (closed) return;
    opts.onError?.(message, detail);
    handle.close();
  };

  // Same-origin WS: ws(s)://<host>/api/live2/{id}/ws. wss when the page is
  // https so the upgrade rides the same TLS the rest of the app uses.
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${proto}//${window.location.host}/api/live2/${cameraId}/ws`;

  // Live fMP4 fragments carry non-zero (wall-clock-derived) timestamps, so
  // the buffered range sits ahead of the element's currentTime (0) and
  // playback never starts — the video stalls at HAVE_METADATA with a gap.
  // After the first media lands, jump currentTime to the live edge so the
  // element has data at the playhead; thereafter let it run.
  let seekedToLive = false;
  const seekToLiveEdge = () => {
    if (seekedToLive || !sourceBuffer) return;
    const b = video.buffered;
    if (b.length === 0) return;
    // Jump just inside the trailing edge (live), not the start, to minimise
    // latency — the whole point of this path.
    const edge = b.end(b.length - 1);
    if (edge > 0 && (video.currentTime < b.start(0) || video.currentTime === 0)) {
      video.currentTime = Math.max(b.start(0), edge - 0.3);
      seekedToLive = true;
      video.play().catch(() => { /* gesture; 'playing' listener covers it */ });
    }
  };

  const drainQueue = () => {
    if (!sourceBuffer || sourceBuffer.updating || queue.length === 0) {
      // Even with nothing to append, the first buffered range may have just
      // become available — try to start playback at the live edge.
      seekToLiveEdge();
      return;
    }
    const chunk = queue.shift()!;
    try {
      sourceBuffer.appendBuffer(chunk);
    } catch (e: any) {
      fail('Playback error in browser', `appendBuffer: ${e?.message ?? e}`);
    }
  };

  const setupSourceBuffer = (codecs: string) => {
    if (closed || !mediaSource) return;
    // go2rtc's {type:"mse"} reply value is the FULL mime
    // (`video/mp4; codecs="hvc1.1.6.L153.B0"`), not a bare codec string —
    // use it directly. Only wrap if we were handed a bare codec list
    // (defensive, in case a future go2rtc changes the shape).
    const mime = /codecs\s*=/.test(codecs) ? codecs : `video/mp4; codecs="${codecs}"`;
    if (!MediaSource.isTypeSupported(mime)) {
      fail('Browser cannot decode this stream (codec unsupported)', mime);
      return;
    }
    try {
      sourceBuffer = mediaSource.addSourceBuffer(mime);
    } catch (e: any) {
      fail('Browser cannot decode this stream', `addSourceBuffer: ${e?.message ?? e}`);
      return;
    }
    // 'segments' mode: each appended fMP4 segment carries its own timing;
    // we do not set timestampOffset. Drain the queue whenever the buffer
    // finishes an append.
    sourceBuffer.mode = 'segments';
    sourceBuffer.addEventListener('updateend', drainQueue);
  };

  const onBinary = (buf: ArrayBuffer) => {
    if (closed) return;
    const chunk = !initAppended ? patchInitSegmentHvcc(buf) : buf;
    initAppended = true;
    queue.push(chunk);
    drainQueue();
  };

  const onControl = (data: string) => {
    if (closed) return;
    let msg: any;
    try {
      msg = JSON.parse(data);
    } catch {
      return; // non-JSON text frame — ignore
    }
    // go2rtc announces the MSE codec string in {type:"mse", value:"<codecs>"}.
    if (msg?.type === 'mse' && typeof msg.value === 'string' && !sourceBuffer) {
      setupSourceBuffer(msg.value);
    }
  };

  mediaSource = new MediaSource();
  video.src = URL.createObjectURL(mediaSource);

  mediaSource.addEventListener('sourceopen', () => {
    if (closed) return;
    ws = new WebSocket(wsUrl);
    ws.binaryType = 'arraybuffer';

    ws.onopen = () => {
      // go2rtc's /api/ws MSE protocol REQUIRES the client to announce the
      // codecs it can decode before go2rtc starts streaming — without this
      // handshake go2rtc stays silent (bench 2026-06-10: WS opened, 0 frames).
      // Mirror go2rtc's own video-rtc.js: filter a candidate list by
      // MediaSource.isTypeSupported and send {type:"mse", value:"<codecs>"}.
      // go2rtc replies with the negotiated {type:"mse", value:"<mime codecs>"}
      // (handled in onControl) then streams binary fMP4.
      // HEVC first: the fleet records H.265, so offer hvc1 ahead of avc1 to
      // match the source without transcode. (When the substream is H.264 —
      // Phase 1b — avc1 matches instead.)
      const candidates = [
        'hvc1.1.6.L153.B0', 'hvc1.1.6.L120.90',      // HEVC main (fleet codec)
        'avc1.640029', 'avc1.64002A', 'avc1.640033', // H.264 high
        'mp4a.40.2', 'mp4a.40.5',                     // AAC
      ];
      const supported = candidates.filter(c =>
        MediaSource.isTypeSupported(`video/mp4; codecs="${c}"`),
      );
      if (supported.length === 0) {
        fail('Browser cannot decode this stream (HEVC support missing)', 'no MSE codecs supported');
        return;
      }
      // Join with bare commas, NOT ", ": go2rtc splits the value on "," and
      // does NOT trim, so a leading space makes every codec after the first
      // unparseable — go2rtc then sees only the first codec and fails to
      // match an H.265 source (bench 2026-06-10: ", " → "codecs not matched
      // video:H265 => video:H264").
      ws!.send(JSON.stringify({ type: 'mse', value: supported.join(',') }));
    };
    ws.onmessage = (ev) => {
      if (typeof ev.data === 'string') {
        onControl(ev.data);
      } else {
        onBinary(ev.data as ArrayBuffer);
      }
    };
    ws.onerror = () => fail('Stream unavailable', 'websocket error');
    ws.onclose = (ev) => {
      // 1000/1005 = normal/no-status close (e.g. our own teardown). Anything
      // else mid-stream is a fault worth surfacing.
      if (!closed && ev.code !== 1000 && ev.code !== 1005) {
        fail('Stream unavailable', `websocket closed (${ev.code})`);
      }
    };

    video.play().then(() => opts.onPlaying?.()).catch(() => {
      // Autoplay can reject before the first frame; onPlaying still fires
      // from the 'playing' listener below if/when playback actually starts.
    });
  });

  video.addEventListener('playing', () => opts.onPlaying?.(), { once: true });

  const handle: MsePlayerHandle = {
    close: () => {
      if (closed) return;
      closed = true;
      if (ws) {
        ws.onmessage = null;
        ws.onerror = null;
        ws.onclose = null;
        try {
          ws.close();
        } catch {
          /* already closing */
        }
        ws = null;
      }
      if (sourceBuffer) {
        sourceBuffer.removeEventListener('updateend', drainQueue);
        sourceBuffer = null;
      }
      if (mediaSource && mediaSource.readyState === 'open') {
        try {
          mediaSource.endOfStream();
        } catch {
          /* nothing to end */
        }
      }
      mediaSource = null;
      queue.length = 0;
      // Revoke the object URL and detach.
      if (video.src.startsWith('blob:')) {
        URL.revokeObjectURL(video.src);
      }
      video.removeAttribute('src');
      video.load();
    },
  };

  return handle;
}

// PATCH_INIT_HVCC gates the array_completeness fixup on the fMP4 init
// segment (see applyHvccPatch). Default false: the patch is only KNOWN to be
// needed for mediamtx's mediacommon HLS muxer (live_proxy.go), and go2rtc's
// MSE muxer is believed to emit complete NAL arrays. Kept as a flag, not a
// dead branch, so both functions stay live for the type checker.
//
// TODO(bench): on the bob bench, if Chromium throws
// manifestIncompatibleCodecsError / bufferIncompatibleCodecs on the HEVC
// substream's first init segment, flip this to true.
const PATCH_INIT_HVCC = false;

// patchInitSegmentHvcc applies (or skips) the array_completeness fixup on an
// fMP4 init segment depending on PATCH_INIT_HVCC. Mirrors the server-side
// patchHVCCCompleteness in internal/api/live_proxy.go that the HLS path uses.
function patchInitSegmentHvcc(buf: ArrayBuffer): ArrayBuffer {
  return PATCH_INIT_HVCC ? applyHvccPatch(buf) : buf;
}

// applyHvccPatch is the byte-level array_completeness fixup. Walks to the
// hvcC box and ORs 0x80 onto each NAL array's flag byte. Mirrors
// patchHVCCCompleteness in live_proxy.go.
function applyHvccPatch(buf: ArrayBuffer): ArrayBuffer {
  const bytes = new Uint8Array(buf.slice(0));
  // Find the 'hvcC' box marker.
  let start = -1;
  for (let i = 0; i + 4 <= bytes.length; i++) {
    if (bytes[i] === 0x68 && bytes[i + 1] === 0x76 && bytes[i + 2] === 0x63 && bytes[i + 3] === 0x43) {
      start = i;
      break;
    }
  }
  if (start < 0) return buf;
  // num_of_arrays sits at start+26 (skip 'hvcC' + 22 fixed-config bytes).
  let off = start + 26;
  if (off >= bytes.length) return buf;
  const numArrays = bytes[off];
  off++;
  for (let i = 0; i < numArrays && off < bytes.length; i++) {
    bytes[off] |= 0x80; // set array_completeness
    off++;
    if (off + 2 > bytes.length) return bytes.buffer;
    const numNalus = (bytes[off] << 8) | bytes[off + 1];
    off += 2;
    for (let j = 0; j < numNalus && off < bytes.length; j++) {
      if (off + 2 > bytes.length) return bytes.buffer;
      const naluLen = (bytes[off] << 8) | bytes[off + 1];
      off += 2 + naluLen;
    }
  }
  return bytes.buffer;
}
