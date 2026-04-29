# Sense / Push-Only Cameras

Battery-powered, PIR-triggered, solar-charged perimeter cameras (the Milesight **SC4xx** series, e.g. SC411 "Perimeter Sensing Camera") **cannot be integrated like a normal IP camera**. They sleep most of the day to conserve battery, wake on PIR, capture a snapshot, and **POST** the event to a configured "Alarm Server" URL. They:

- Don't accept continuous RTSP pulls (would drain battery in days).
- Don't accept ONVIF event subscriptions (camera is asleep).
- Don't run a long-lived analytics WebSocket.

So Ironsight integrates them inbound: the platform exposes a **per-camera webhook URL**, and the camera POSTs to it on every triggered event.

---

## 1. Lifecycle

```
                ┌──────────────────────┐
                │  PIR / human / motion│ ── camera wakes
                │  detected on camera  │
                └──────────┬───────────┘
                           │
                ┌──────────▼───────────┐
                │  camera POSTs JSON   │  multipart/form-data
                │  + JPEG snapshot to  │
                │  /api/integrations/  │  HTTPS, public-internet routable
                │  milesight/sense/    │
                │  {token}             │
                └──────────┬───────────┘
                           │
              ┌────────────▼────────────┐
              │  Ironsight api          │
              │   ↓                     │
              │   1. validate token     │  GetCameraBySenseToken
              │   2. save snapshot      │  /snapshots/<cam_id>/sense-<ts>.jpg
              │   3. insert event row   │  events hypertable
              │   4. create active alarm│  active_alarms
              │   5. WS broadcast       │  to operators
              └────────────┬────────────┘
                           │
                ┌──────────▼───────────┐
                │  SOC operator sees   │
                │  alarm + snapshot    │
                │  on the console      │
                └──────────────────────┘
```

The camera goes back to sleep immediately after the POST. It never holds a long-lived connection.

---

## 2. Adding a sense camera

In the admin UI, click **➕ Add Camera** and:

1. Set **Camera Type** → **Sense / push-only (Milesight SC4xx, PIR/solar)**.
2. Fill in **Name**, **ONVIF Address** (`https://<camera-ip>`), **Username**, **Password**.
3. Click **Add Camera**.

The platform does a one-shot ONVIF probe to capture identity (manufacturer / model / firmware), mints a 256-bit URL-safe token, and stores the camera with `device_class = sense_pushed`. It **does not**:
- Pull RTSP
- Subscribe to ONVIF events
- Register the camera with the recording engine
- Add a stream to mediamtx

A confirmation modal then shows the **webhook URL split into the four fields the camera's Alarm Server form expects**:

| Camera field | Value |
|---|---|
| Protocol Type | `HTTPS` (or `HTTP` if behind a reverse proxy without TLS) |
| Destination IP/Host Name | `<your-public-host>` (just the hostname — no scheme, no path) |
| Port | `443` for HTTPS, `80` for HTTP |
| Path | `/api/integrations/milesight/sense/<token>` |
| User Name / Password | **leave blank** — the path token is the auth |

The exact same fields are also visible later under **Camera Settings → General** for as long as the camera exists, in case the camera's config gets reset.

---

## 3. Configuring the camera side

On the camera's web UI:

1. **Event → Alarm Settings → Alarm Server → Add**.
2. Fill in the four fields from step 2 above.
3. **HTTP Method**: `POST`.
4. **File Push Method**: `General`.
5. **Encoding Type**: `Binary` (preferred — smaller payload) or `Base64` (both are accepted by the receiver).
6. Click **Test** before **Save**. The receiver returns 200 on a valid auth + payload, 401 on a wrong token. Either response confirms the URL is reachable; only the parsed-payload error proves the receiver is parsing correctly.

For event-type triggers (motion detection, intrusion, line-cross), make sure the rule's **Alarm Action → Data Push** has `Enable` checked and the new Alarm Server selected.

---

## 4. Public URL — a hard requirement

The camera lives on the public cellular WAN, but our api in dev defaults to `localhost:8080`. **The camera cannot POST to localhost.** You need a public URL that the camera's HTTPS client can reach.

Two patterns:

### Dev / testing
Run a tunnel and use whatever public hostname it gives you:

```bash
# Cloudflare (free, stable, no signup for trycloudflare.com)
cloudflared tunnel --url http://localhost:8080

# ngrok (free tier rotates the URL)
ngrok http 8080
```

Take the URL (e.g. `https://amber-fox.trycloudflare.com`) and use *that* host in the camera's Alarm Server config, with the same path token.

### Production
Run the api behind a reverse proxy (Caddy / nginx / Traefik) at a real domain, with a valid TLS cert. The camera will accept any cert that chains to a public CA, plus self-signed if you disable cert verification on its side.

Set `PUBLIC_BASE_URL` in `.env` to your public origin. (Implementation note: the frontend currently builds the URL from `window.location.origin`, which is correct when an admin operates from the same host the camera reaches. If your admins log in from a different origin than the camera reaches, this is a future improvement to thread.)

---

## 5. Payload format

The Milesight Alarm Server "General + Binary" combo POSTs `multipart/form-data` with:

1. A JSON form field named `data` (or `metadata` / `json` — varies by firmware), containing the Data-Edit template the user configured. Default fields:

   | Field | Type | Notes |
   |---|---|---|
   | `event_id` | string | Camera-side event UUID |
   | `event_type` | string | e.g. `humanDetect`, `vehicleDetect`, `motion`, `lineCross` |
   | `device_name` | string | What the user named the camera in firmware |
   | `mac_address`, `sn` | string | Hardware identity |
   | `latitude`, `longitude`, `altitude` | string | GPS fix at trigger time |
   | `time`, `time_msec` | string | Camera-side timestamps |
   | `detection_region` | int | Numeric region ID |
   | `detection_region_name` | string | Operator-readable region label |
   | `resolution_width`, `resolution_height` | int | Snapshot dimensions |
   | `coordinate_x1`, `y1`, `x2`, `y2` | float | Bounding box (camera pixels) |

2. One or more file parts containing the snapshot JPEG. The receiver keeps only the first.

If the camera is set to `Encoding Type: Base64`, it instead POSTs `application/json` with the snapshot inline as a base64 string under key `snapshot` / `snapshot_list` / `image`. Both encodings work.

The receiver maps `event_type` to the platform's existing taxonomy (`human` / `vehicle` / `intrusion` / `linecross` / `motion` / etc.) so downstream alarm rules and AI prompts treat sense-camera events identically to ONVIF-driven events.

---

## 6. Security model

- **The token IS the authentication.** No JWT, no IP allow-list, no client-cert. Cameras can't carry any of those.
- **256-bit random token** (`crypto/rand`) → 43-char URL-safe base64. Unguessable.
- **Stored once** in `cameras.sense_webhook_token` with a unique index. Never echoed in list endpoints; only returned by the per-camera GET inside the admin Settings modal.
- **If the URL leaks**: delete the camera, re-add it. A new token is minted; the old URL becomes invalid.
- **Camera reboot does not change the token.** Only camera deletion does.
- **The receiver never echoes the token in error responses** — it returns generic 401 / 400 so an attacker scanning random tokens can't tell whether they're close.

---

## 7. AI pipeline

Sense-camera events go through the same `aiClient.Analyze(ctx, jpegFrame, siteID, siteContext)` call that ONVIF events use. Per-camera AI in-flight gate, motion-bucket cooldown, and per-site usage tracking all apply identically.

Important: **YOLO/Qwen run on the snapshot the camera sends**, not on a frame the platform pulled. If the camera's PIR was a false positive (squirrel, branch in wind), the snapshot reflects that — Qwen will most likely return `threat_level: none`, `false_positive_pct: high`, and the operator console shows it as filtered.

---

## 8. What it does NOT do (yet)

- **No video clip.** Sense cameras don't stream — only the single trigger snapshot is available. A future enhancement could ask the camera to record a short clip to its SD card and upload it on the next wake.
- **No live preview.** Operators can't pull a live frame from a sleeping camera; they see the most recent triggered snapshot only.
- **No two-way audio.** Hardware-dependent and out of scope for the SC4xx perimeter line.
- **No PTZ.** SC411 is fixed-pose by design.

---

## 9. Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| Camera's `Test` button fails with timeout | The camera can't reach your public URL | Verify the URL works from anywhere on the public internet. Test with `curl -v <url>`. |
| Test returns 401 | Token mismatch | Re-copy from the camera-settings modal; tokens are case-sensitive. |
| Test returns 400 with "invalid payload" | Camera Test sends an empty body | This is **expected** for Test. The endpoint is reachable; that's the goal. Save and trigger a real event to verify. |
| Real event fires but no alarm appears | api isn't running, or the camera's clock is wildly off | Check `podman logs ironsight-api | grep SENSE`. Verify the camera's NTP. |
| Snapshot is missing on the alarm card | Camera firmware skipped the file part, or the JPEG was 0 bytes | api logs `(0-byte snapshot)` for these. Check the Alarm Server's Encoding Type is `Binary` and File Push is `General`. |
| Camera fires constantly through the night | PIR sensitivity is too high or motion-detection rules are too broad | Tune the rule's region + sensitivity in the camera's web UI. The Ironsight side has a 60-s motion-bucket cooldown, but the PIR / camera burning battery is upstream of that. |

---

## 10. Backend file map

For maintainers:

- [`internal/api/sense_camera.go`](../../internal/api/sense_camera.go) — camera-create flow for `device_class = sense_pushed`. Skips RTSP/event subscription, mints token.
- [`internal/api/sense_webhook.go`](../../internal/api/sense_webhook.go) — `POST /api/integrations/milesight/sense/{token}` receiver. Multipart parsing, payload mapping, alarm dispatch.
- [`internal/api/router.go`](../../internal/api/router.go) — public route registration (no auth middleware on this path; the token IS the auth).
- [`internal/api/cameras.go`](../../internal/api/cameras.go) — `HandleCreateCamera` short-circuits to `createSenseCamera` when `input.DeviceClass == "sense_pushed"`.
- [`internal/database/db.go`](../../internal/database/db.go) — `GetCameraBySenseToken` lookup; `CreateCamera` writes the new columns.
- [`cmd/server/main.go`](../../cmd/server/main.go) — schema migration for `device_class`, `sense_webhook_token`.
- [`frontend/src/components/CameraManager.tsx`](../../frontend/src/components/CameraManager.tsx) — Add Camera dialog dropdown + post-create webhook panel + settings-tab persistent display.
