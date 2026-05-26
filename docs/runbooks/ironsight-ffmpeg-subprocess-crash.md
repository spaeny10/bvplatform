# Alert: IronsightFFmpegMismatch

## Severity
warning

## What fired

`ironsight_recording_active_cameras != ironsight_recording_ffmpeg_subprocesses`
for 5 minutes. The number of cameras with active sessions differs from the
number of live FFmpeg child processes.

## Immediate actions

1. A mismatch is EXPECTED when gortsplib cameras are active — they count toward
   active cameras but not FFmpeg processes. Check `GORT_CAMERAS` env var.
2. If the gap is larger than the gortsplib camera count:
   `docker exec ironsight ps aux | grep ffmpeg` — count the FFmpeg processes.
3. Compare with `ironsight_recording_ffmpeg_subprocesses` in Grafana.
4. If an FFmpeg process is missing, check recording logs for crash/restart loops:
   `docker logs ironsight --tail 500 2>&1 | grep "FFmpeg error"`.

## Likely causes

- An FFmpeg process crashed and is in its 5s restart backoff
- A gortsplib camera was started (expected mismatch — normal)
- An orphaned FFmpeg process from a previous session

## Resolution

Alert auto-resolves when the counts re-converge. In normal operation this
happens within one restart cycle (5–30 seconds). Sustained mismatch beyond
5 minutes indicates a recurring crash.

## Escalation

If FFmpeg is crash-looping on a specific camera for more than 30 minutes,
check the camera's RTSP stream health and consult the FFmpeg stderr log at
`/mnt/recordings/<camera-uuid>/ffmpeg_stderr.log` on fred.
