# Alert: IronsightCameraDropoff

## Severity
warning

## What fired

`delta(ironsight_recording_active_cameras[10m]) <= -2` — at least 2 cameras
dropped from the recording engine within a 10-minute window.

## Immediate actions

1. Check which cameras dropped: look for "Stopped recording" log lines in
   `docker logs ironsight --tail 200 2>&1 | grep "REC\]"`.
2. Check the trailer LAN (`192.168.50.0/24`): ping cameras from fred.
3. Check Peplink bonded router status for the affected trailer.
4. If cameras are reachable, trigger a manual reconnect via the Ironsight UI
   (Settings → Cameras → reconnect).

## Likely causes

- Trailer network outage (Peplink LTE bonding dropped)
- Camera power cycle or firmware update
- Switch failure on the trailer LAN

## Resolution

Alert resolves when `ironsight_recording_active_cameras` rises back toward
expected count. Check Grafana for the camera count gauge trending upward.

## Escalation

If cameras remain offline for more than 1 hour and the trailer is confirmed
powered, contact the trailer technician for on-site inspection.
