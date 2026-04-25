# Disaster Recovery Runbook

This is the operating procedure for restoring Ironsight after data
loss, host failure, or cluster compromise. Read in conjunction with
`IncidentResponse.md` (which covers the incident-classification and
notification side) — this runbook is specifically about getting the
platform back online with intact data.

**Audience:** the on-call engineer; the platform owner during
business hours.

**Recovery objectives we design for:**

| Objective | Target | What it means |
|---|---|---|
| **RPO** (Recovery Point Objective) | 24 hours | Worst-case data loss is one day of activity. Driven by the nightly backup cadence. |
| **RTO** (Recovery Time Objective) | 4 hours | Time from "we have decided to restore" to "the platform is serving requests on the recovered DB." |

If a customer requires tighter numbers, increase the backup
cadence (see §3.4) and stand up a warm replica (see §3.6).

---

## 1. What is and is not protected

| Asset | Backup mechanism | RPO | Notes |
|---|---|---|---|
| **Postgres / TimescaleDB data** (users, sites, cameras, security_events, segments metadata, audit_log, support_tickets, etc.) | Nightly `pg_dump` via `scripts/backup-db.sh` with daily/weekly/monthly rotation | 24 h | Logical dump — restorable into any compatible Postgres instance. |
| **Recording files on disk** (`segments` payloads — the actual MP4/HLS) | Filesystem snapshots (deployment-dependent) or off-site sync | Site-dependent | Recordings can be regenerated from cameras if SD/edge storage is enabled. The platform DB tracks segment metadata; missing files surface as gaps in `recording_health`. |
| **Configuration & secrets** (`.env`, TLS certs) | Treat as code: encrypted store, version-controlled deployment manifests | n/a | Not part of the DB. Lose-and-rebuild risk if the host is destroyed without a manifest backup. |
| **Code** | Git remote (GitHub) | n/a | Recovery is `git clone`. |
| **Container images** | Container registry | n/a | Pinned by tag in `docker-compose.yml`. |

What is **not** auto-recovered:
- Custom per-customer signage, playbook content, or onboarding state
  not stored in the DB.
- Live HLS streams in flight (cameras reconnect; transient gap).
- Active user sessions (JWTs become invalid; users log in again).

---

## 2. Routine: what should already be happening

**Every host running Ironsight in production must have:**

1. `scripts/backup-db.sh` scheduled nightly (default 02:00 local).
2. `scripts/verify-backup.sh` scheduled weekly.
3. Off-site copy of the `backups/` directory (S3, B2, or another
   provider — see §3.3 for a sample wrapper).
4. Documented location of the last-known-good `.env` file.
5. The deployment owner subscribed to the platform's status page
   and the backup cron's mail output.

**Cron snippet (Linux/WSL host):**

```cron
# 02:00 — nightly logical backup of the platform DB.
0 2 * * *  /opt/ironsight/scripts/backup-db.sh >> /var/log/ironsight-backup.log 2>&1

# 03:30 Monday — verify last week's backup actually round-trips.
30 3 * * 1 /opt/ironsight/scripts/verify-backup.sh >> /var/log/ironsight-verify.log 2>&1
```

**Windows Task Scheduler equivalent** runs `bash.exe scripts/backup-db.sh`
on the same cadence. Confirm Docker Desktop is allowed to start
automatically on boot — otherwise the cron job runs against a
stopped daemon.

---

## 3. Operations

### 3.1 Take a manual backup

```bash
./scripts/backup-db.sh
```

Lands a new `.dump` in `backups/daily/`. On Sundays this also
appears in `backups/weekly/`; on the 1st of the month, also in
`backups/monthly/`.

### 3.2 Verify a specific backup

```bash
./scripts/verify-backup.sh                                  # latest daily
./scripts/verify-backup.sh ./backups/monthly/ironsight-…    # specific file
```

Spins up a throwaway TimescaleDB container, restores into it, runs
the table-presence sanity check, tears down. Does **not** touch the
production database.

### 3.3 Off-site copy (sample S3 wrapper)

```bash
# /opt/ironsight/scripts/offsite-push.sh — chained after backup-db.sh
aws s3 sync ./backups/ s3://your-bucket/ironsight-backups/ \
  --storage-class STANDARD_IA --delete
```

**Encryption at rest is the bucket's job, not this script's.** Enable
SSE-S3 or SSE-KMS on the bucket policy. Versioning + Object Lock
turns the bucket into a ransomware-resistant snapshot.

### 3.4 Tighter RPO

To shrink RPO below 24 hours, pick one:

- **Continuous archiving (PITR).** Configure WAL archiving to
  off-site storage. Restore = base backup + replay WAL to a
  point-in-time. RPO = WAL ship interval (often <5 minutes).
- **Streaming replica.** Run a second Postgres instance receiving
  WAL from the primary in real time. RPO ≈ replication lag
  (seconds). Failover = promote the replica.

Both add operational complexity. Default deployment ships with
neither; we recommend logical backup as the floor and add WAL
archiving when a customer's RPO requirement justifies it.

### 3.5 Tighter RTO

Most of the 4-hour RTO is human decision-making + DNS + restart
ceremony. Ways to shrink:

- Pre-staged restore host — a second host with the stack already
  installed and a recent backup pre-restored. Cut DNS to it on
  failure.
- Automated health probes that page a human within 5 minutes of an
  outage rather than relying on a customer to notice.

### 3.6 Warm replica (read-only standby)

For customers requiring HA: run a second Postgres instance as a
streaming replica and a second API host pointed at it in read-only
fallback mode. Promotion is `pg_ctl promote` on the replica + a
config flip on the API. Document the promotion command in the
deployment's site-specific runbook — the steps are the same shape
but the hostnames are not.

---

## 4. Restore procedures

### 4.1 Restore into the running stack (in-place)

**Use case:** the database is corrupt, dropped, or accidentally
modified. The host and stack are otherwise healthy.

1. **Stop the API and worker services** so they don't write during
   the restore:
   ```bash
   docker compose stop api worker
   ```
2. **Choose the backup file.** Latest known-good is usually the
   right answer. Verify it round-trips first if you have not done
   so recently:
   ```bash
   ./scripts/verify-backup.sh ./backups/daily/ironsight-….dump
   ```
3. **Run the restore.** Confirms the database name interactively
   before destroying anything:
   ```bash
   ./scripts/restore-db.sh ./backups/daily/ironsight-….dump
   ```
4. **Bring services back up:**
   ```bash
   docker compose start api worker
   ```
5. **Smoke check:**
   ```bash
   curl -s http://localhost:8080/api/health
   curl -s -X POST http://localhost:8080/auth/login \
     -H "Content-Type: application/json" \
     -d '{"username":"admin","password":"<from .env>"}'
   ```
6. **Notify users.** Their JWT sessions are invalid; they will need
   to log in again. Send a brief in-product or email notice.
7. **Audit the recovery.** Run a `SELECT count(*) FROM audit_log;`
   and compare to the value just before the restore (write that
   value down before kicking off step 1, ideally — it's a quick
   sanity number).

### 4.2 Restore onto a new host (host loss)

**Use case:** the original host is gone — disk failure,
ransomware, fire, lost cloud account.

1. **Stand up the new host** with Docker Engine 24+ and Compose v2.
2. **Recover code:**
   ```bash
   git clone https://github.com/spaeny10/bvplatform.git /opt/ironsight
   cd /opt/ironsight
   ```
3. **Recover the `.env`** from your secret store. If you have lost
   `.env`, you will be regenerating credentials — see §4.4.
4. **Recover the latest `backups/` directory** from off-site
   storage. Place at `/opt/ironsight/backups/`.
5. **Bring up the DB only first** so its init.sql doesn't race
   with anything:
   ```bash
   docker compose up -d db
   docker compose ps db   # wait for healthy
   ```
6. **Run the restore:**
   ```bash
   ./scripts/restore-db.sh ./backups/daily/<latest>.dump
   ```
7. **Bring the rest of the stack up:**
   ```bash
   docker compose up -d
   ```
8. **DNS / load balancer cutover** to the new host. If the FQDN
   has not changed, just flip the A record TTL.
9. **Verify** with the smoke check from §4.1 step 5.
10. **Recordings.** The DB knows about recordings the old host had,
    by path. Those paths point at storage you may not have on the
    new host. You will see gaps in the recording timeline until
    storage is reattached or the rows are pruned. Document this in
    the customer-facing incident notice.

### 4.3 Point-in-time recovery (if WAL archiving is enabled)

Out of scope for the default deployment. If you have configured
WAL archiving, follow Postgres's own procedure for `pg_basebackup`
+ `recovery_target_time`. Document the restore target time and the
WAL archive location at the top of the recovery channel.

### 4.4 Lost-`.env` recovery

If you have lost `.env`:

- **`POSTGRES_PASSWORD`** — set a new one in the new `.env`. The
  restore script creates the database; it does not need to match
  the original.
- **`JWT_SECRET`** — set a new one. All existing JWTs become
  invalid. Users log in again. This is acceptable.
- **`EVIDENCE_HMAC_KEY`** — **caution**. Old evidence signatures
  cannot be verified against a new key. Decide explicitly:
  - For ongoing incidents, this is acceptable; new exports will be
    signed with the new key.
  - For chain-of-custody on previously-exported evidence, the old
    key was already on the export itself (HMAC is keyed); the
    verifier needs the old key to validate, and you've lost it.
    Document this in the incident report.
- **`SMTP_*`, `TWILIO_*`** — re-issue from the provider consoles.

After regenerating, store the new `.env` in your secret manager
**before** bringing the stack up. Don't repeat the loss.

---

## 5. Decision tree

```
Outage detected
   │
   ├── Database corruption / data loss only
   │      → §4.1 (restore into running stack)
   │
   ├── Host unreachable / destroyed
   │      → §4.2 (restore onto new host)
   │
   ├── Suspected compromise (ransomware, intrusion)
   │      → IncidentResponse.md §5 (contain) FIRST,
   │        then §4.2 onto a clean host with rotated secrets
   │
   └── Recording-storage failure only (DB intact)
          → Replace storage; mark affected segments as
            unrecoverable; notify affected customers
```

---

## 6. Drills

A backup that has only ever been written has a 50/50 chance of
working when you need it. The verify-backup.sh weekly cron is the
floor. On top of that, run a full **DR drill** at least every six
months:

1. Pick a non-production environment.
2. Pretend the primary host is gone.
3. Walk through §4.2 from a cold start, timing each step.
4. Compare elapsed time against the 4-hour RTO.
5. Write down what was confusing, broken, or missing.
6. File issues against this runbook so the next drill is faster.

A drill report goes to `Documents/drills/<YYYY-MM-DD>-dr-drill.md`.

---

## 7. Document maintenance

This runbook is reviewed at least annually and after any disaster
recovery event (drill or real). Material changes are reflected in
`Documents/CHANGELOG.md`.

| Date | Change | By |
|---|---|---|
| 2026-04-25 | Initial runbook. RPO/RTO targets set at 24h/4h. | Shawn / Claude |
