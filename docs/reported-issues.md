# Reported Issues & Requests — Intake Log

> Durable log of bugs and feature requests reported during testing. These come
> in as direct reports (not GitHub Issues), so this file is the system of record.
> Shipped items also land in [CHANGELOG.md](../CHANGELOG.md); open items feed the
> backlog ([ROADMAP.md](ROADMAP.md)). Add a row when something is reported; flip
> its status when it ships.

**Status:** 🟢 shipped · 🟡 in progress · 🔴 open · ⚪ answered (no code change)

---

## 2026-06-10 / 06-11 — playback & camera testing pass (reporter: Caleb)

### Bugs

| ID | Reported | Bug | Status | Resolution |
|----|----------|-----|--------|------------|
| B-01 | 06-10 | Recording timeline has obvious footage gaps | 🟢 | **PR #67** — record the H.264 sub directly, tighten the stall watchdog, fix a segment-ingest race (gapless verified) |
| B-02 | 06-10 | Editing a camera name doesn't save — modal has only a Close button | 🟢 | **PR #68** — added a "Save & Close" button (kept onBlur autosave) |
| B-03 | 06-10 → 06-11 | "Camera VCA (on-device)" tab loads the Ironsight NVR app instead of the camera UI; stale "works only on the camera LAN" copy | 🟢 | **PR #69** — the firmware is a JS SPA that escapes a same-origin iframe; now opens the camera's web UI in a new tab at its own reachable address; copy corrected |
| B-04 | 06-10 | Newly-added cameras stuck on "Connecting" (504 extra cams + 5001 layout) | 🟢 | **PR #68** sync mediamtx register on create/update + a `docker restart ironsight-test-mediamtx`. Root cause is **O-01** (mediamtx config-API hang) |
| B-05 | 06-10 | 504 right-PTZ not recording | 🟢 | DB fix — wrong RTSP URL (raw IP) → `504.bigview.ai:557` (recording verified). Root cause is **O-02** |
| B-06 | 06-10 → 06-11 | Playback shows the same feed for front & back cameras | 🟢 | **PR #68** (cell keying) **+** DB fix: 504 & 5001 "back" had **duplicate RTSP URLs** (both `:554/channel1`) → corrected to `:555/channel1`. Root cause is **O-02** |
| B-07 | 06-10 | Site assignment doesn't persist (5001 → "Ironclad HQ Yard 5001" shows unassigned) | 🟢 | **PR #68** — a CSRF-token 403 was being silently swallowed by the assign modal; `authFetch` now bootstraps/retries the token and the modal surfaces errors |
| B-08 | 06-11 | Events from other cameras (504) show on the 5001 timeline | 🟢 | **PR #70** — timeline & events now scope to the active grid layout (CameraGrid→page bridge); empty-filter no longer means "all cameras" (e2e-proven) |
| B-09 | 06-11 | Event/alert feed flooded with thousands of bogus events from 504 right-PTZ (8386 on one camera, ~6.5/min — "tons of events I didn't configure") | 🟢 | ONVIF `PropertyOperation` was parsed from the wrong element (outer `wsnt:Message` wrapper vs inner `tt:Message`), so it always decoded as `""` and the LOCAL-05 `Initialized`-state filter was bypassed — every subscription-renewal no-event-state snapshot was recorded as an alert. Fixed the parse (`internal/onvif/events.go`, nested inner-element decode) + parse-level regression test; purged 8446 noise rows on the test DB. Verified flat after deploy. |
| B-10 | 06-11 | ONVIF PullPoint subscription leak/churn — a new subscription created every ~60s of idle + never unsubscribed, exhausting the camera's pool ("Maximum number of Subscribe reached") | 🟢 | `internal/onvif/events.go` `pullLoop` "renewed" by calling `CreatePullPointSubscription` after 20 empty polls (~60s) **and** on the proactive-renewal branch — spinning up a brand-new subscription (+ a full Initialized rule-state dump) every ~60-73s and never releasing the old one, so the camera's small subscription pool drained. **Fix:** stop re-subscribing on idle (no-events is normal — keep polling the same sub; empty counter is logging-only); use a real WS-BaseNotification `wsnt:Renew` for proactive renewal (extends the SAME sub's TTL, no new slot/snapshot); add `wsnt:Unsubscribe` to release the camera slot before any replace (renew-failed fallback + maxErrors-died path) and on `Stop()`/shutdown. Cap-error 5-min backoff + genuine dead-sub recreate preserved; Renew faults handled gracefully (→ unsubscribe-old + recreate). Renew/Unsubscribe SOAP-body builders + an idle-no-resubscribe loop test (create-count stays 1 across 500 empty polls) added. (Surfaced while fixing B-09.) |
| B-11 | 06-11 | Only 504 right-ptz delivered events — Milesight WS driver didn't map `vcaAdvancedMotion` (basic motion) to events, so the 5 cameras on the WS path produced nothing | 🟢 | `internal/milesight/websocket.go` `parseMilesightTrack`: the flag list mapped every VCA rule (intrusion/linecross/loitering/human/object/tamper) **except** basic motion, but the WS cameras report motion as `vcaAdvancedMotion:1` (and `aiMotion:1` on AI models) in the trackList — so motion was decoded and silently dropped. **Fix:** added `{vcaAdvancedMotion → "motion"}` + `{aiMotion → "motion"}` to the flag list (both share the existing `trackID:motion` edge-detect key → one motion event per 0→1 transition, no double-count, no per-frame re-fire); unit tests added (`websocket_test.go`). Verified on bob: `504 left ptz` (the only WS camera emitting JSON `trackList` frames) went 0 → 129 motion events in 5 min (~25.8/min) after deploy. **Caveat — surfaced during verification:** the other 4 WS cameras (5001 front/back, 504 front/back) emit a **binary** `/webstream/track` envelope (fixed 308/156-byte little-endian packed frames), **not** JSON, so they never reach `parseMilesightTrack` and still produce 0 events. This fix is correct & necessary (unblocks the JSON path) but the binary-frame decode is a separate item — see **O-07**. |

### Feature requests

| ID | Reported | Request | Status | Resolution |
|----|----------|---------|--------|------------|
| F-01 | 06-11 | Seek/play bar: tick marks (sec/min/hr), clearer/clickable event markers, fast-forward / rewind / speed up / slow down | 🟢 | **PR #72** — zoom-aware tick ruler, interactive markers (tooltip + click-to-seek), playback speed 0.5/1/2/4×, skip ±10s/±30s + frame-step |
| F-02 | 06-11 | Alert feed: show what object was detected, snapshot with bounding box, camera-vs-server source, group by active grid, better sort + multi-select | 🟢 | **PR #71** — object/rule/confidence chips, snapshot + bbox overlay, 📷 Camera-VCA / 🧠 Server-AI source badge, group-by-active-grid + multi-select + sort |

### Questions / no code change

| ID | Reported | Question | Disposition |
|----|----------|----------|-------------|
| Q-01 | 06-10 | The incoming alert events — camera-side or server-side? I didn't set them up. | ⚪ **Camera-side VCA** — the cameras' own DSP emits ONVIF rule events (intrusion, line-cross, loitering, tamper, motion, object, human). Tune/disable in each camera's web UI. |

---

## Open backlog (reported or surfaced, not yet started)

| ID | Type | Item | Notes |
|----|------|------|-------|
| O-01 | bug / infra | mediamtx **v1.19.0 `/v3/config/*` API hangs** (15s timeout, even read-only); YAML not hot-reloaded → camera add/edit needs a manual `docker restart ironsight-test-mediamtx` | Root cause behind **B-04**. The PR #68 sync-register is correct but inert on this instance. Options: pin a known-good mediamtx, or have the app trigger a reload. |
| O-02 | bug / hardening | **Camera-add ONVIF probe** mis-populates RTSP URLs/ports (duplicate `channel1`, raw IP, wrong port) | Root cause behind **B-04/B-05/B-06** — fixing it at the source stops the recurring per-camera DB hand-fixes. |
| O-03 | feature | PTZ live latency still ~2–3s (target sub-1s) | go2rtc `live2` path; GOP + live-edge slack. |
| O-04 | infra | Low-latency live (`live2`) production rollout | Requires all prod cameras on an H.264 substream first. |
| O-05 | feature | Playback timeline **footage-gap indicator** | Show recorded-vs-gap on the scrubber. |
| O-06 | bug | Live-session-recovery 401 | Carryover from earlier review. |
| O-07 | bug | 4 of 5 Milesight WS cameras (5001 front/back, 504 front/back) emit a **binary** `/webstream/track` envelope, not JSON, so no events decode | Surfaced verifying **B-11**. Frames are fixed-size little-endian packed records (e.g. 308-byte frames carrying 9-int32 track records: trackID, x, y, w, h, Class, …; magic header `33 22 11 00 37`). `parseMessage`'s `findJSONStart` fallback finds no JSON → dropped. Only `504 left ptz` sends JSON `trackList` (and now works). Needs a binary `/webstream/track` decoder that yields the same `msTrackFrame.TrackList` shape, after which the B-11 motion mapping covers it. Likely a firmware/model-variant difference in the WS payload format. |
| O-08 | bug / frontend | Alert-feed bbox overlay normalizes Milesight WS boxes by a **0-10000 grid** — wrong; real track coords are small (x ≤ ~320, y ≤ ~100) | Surfaced verifying **B-11**. `EventListPanel.tsx` `getNormalizedBoxes` divides milesight_ws box x/y/w/h by `MILESIGHT_GRID = 10000`, citing the ONVIF rule-geometry `pt.X*10000` region space — but the `/webstream/track` runtime track bboxes are a **different, much smaller** analytics-frame space (observed across 161 real events: x 30–319, y 54–99, w 5–56, h 6–25 → consistent with a ~320×N analytics frame, **not** 0-10000). Dividing by 10000 collapses every box to a top-left dot. Recalibrate the milesight_ws normalization to the real analytics-frame resolution. |

---

*Maintained as reports come in. When an open item ships, move it up into the dated section with its PR.*
