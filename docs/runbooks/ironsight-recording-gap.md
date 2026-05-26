# Alert: IronsightRecordingStalled

## Severity
critical

## What fired

`ironsight_recording_active_cameras > 0` but
`rate(ironsight_recording_segments_written_total[5m]) == 0` for 5 minutes.
At least one camera has an active recording session but no segments are being
written to the database.

## Immediate actions

1. SSH to fred: `ssh fred`
2. Check FFmpeg processes: `docker exec ironsight ps aux | grep ffmpeg`
   — if none, FFmpeg crashed and the stall watchdog may not have fired yet.
3. Check recording logs: `docker logs ironsight --tail 200 2>&1 | grep "REC\]"`
   — look for "Stream stall detected", "FFmpeg error", "Failed to register segment".
4. Check disk space: `df -h /mnt/recordings` — if full, segments cannot be written.
5. Check DB connectivity: `docker exec ironsight-db psql -U ironsight -c "SELECT NOW();"`.

## Likely causes

- Disk full on the recording storage path
- FFmpeg stall watchdog fired but camera auto-restart is failing
- Database connection issue preventing `InsertSegment`
- `watchSegments` goroutine panic (check for "PANIC in" log lines)

## Resolution

Alert resolves when segments start accumulating again. In Grafana, watch
`rate(ironsight_recording_segments_written_total[1m])`. A non-zero rate
means recording has resumed.

## Escalation

If recording is stalled for more than 30 minutes with cameras online, contact
Caleb (caleb@jetstreamsys.com). Data loss window equals the stall duration.
