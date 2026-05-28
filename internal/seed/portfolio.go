package seed

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"ironsight/internal/database"
)

// seedPortfolio creates a believable customer portfolio so the
// portal's "what we handled for you" panel and incident-detail
// "How the SOC handled this" block render with real numbers
// instead of zeros on a clean install.
//
// Three orgs, five sites, a 30-day spread of dispositioned events
// weighted toward false positives — that's the actual story the SOC
// tells the customer ("most events are noise; we filtered them so
// you didn't have to"). Operator callsigns match the SOC users that
// seedUsers creates so the same names appear consistently
// across the portal and the operator console.
//
// Idempotent: bails out if the first demo org already exists, so
// re-running doesn't append duplicate events.
func seedPortfolio(ctx context.Context, db *database.DB) error {
	var existing string
	_ = db.Pool.QueryRow(ctx, `SELECT id FROM organizations WHERE id='co-alpha001'`).Scan(&existing)
	if existing != "" {
		log.Printf("[SEED] portfolio already seeded (co-alpha001 exists); skipping")
		return nil
	}

	type orgSeed struct {
		ID           string
		Name         string
		Plan         string
		ContactName  string
		ContactEmail string
	}
	type siteSeed struct {
		ID      string
		OrgID   string
		Name    string
		Address string
		Lat     float64
		Lng     float64
	}

	orgs := []orgSeed{
		{"co-alpha001", "Apex Construction Group", "enterprise", "Sandra Pierce", "spierce@apexcg.com"},
		{"co-beta002", "Meridian Development Ventures", "professional", "Priya Sharma", "priya@meridiandv.com"},
		{"co-gamma003", "Ironclad Site Services", "professional", "Derek Lawson", "dlawson@ironcladsites.com"},
	}
	sites := []siteSeed{
		{"ACG-301", "co-alpha001", "Apex Tower — Phase 2", "1450 N Wells St, Chicago, IL", 41.9089, -87.6342},
		{"ACG-302", "co-alpha001", "Apex Riverside Plaza", "200 W Wacker Dr, Chicago, IL", 41.8870, -87.6350},
		{"MDV-501", "co-beta002", "Meridian Industrial Park", "5500 E Riverside Dr, Austin, TX", 30.2389, -97.7197},
		{"MDV-502", "co-beta002", "Meridian Logistics Hub", "10000 W Cesar Chavez St, Austin, TX", 30.2562, -97.7986},
		{"ISS-201", "co-gamma003", "Ironclad HQ Yard", "4400 N 32nd St, Phoenix, AZ", 33.4938, -112.0118},
	}

	// Insert orgs and sites.
	for _, o := range orgs {
		if _, err := db.Pool.Exec(ctx, `
			INSERT INTO organizations (id, name, plan, contact_name, contact_email)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (id) DO NOTHING`,
			o.ID, o.Name, o.Plan, o.ContactName, o.ContactEmail,
		); err != nil {
			log.Printf("[SEED] org %s: %v", o.ID, err)
		}
	}
	for _, s := range sites {
		if _, err := db.Pool.Exec(ctx, `
			INSERT INTO sites (id, name, address, organization_id, latitude, longitude, status)
			VALUES ($1,$2,$3,$4,$5,$6,'active')
			ON CONFLICT (id) DO NOTHING`,
			s.ID, s.Name, s.Address, s.OrgID, s.Lat, s.Lng,
		); err != nil {
			log.Printf("[SEED] site %s: %v", s.ID, err)
		}
	}
	log.Printf("[SEED] %d demo orgs and %d sites created", len(orgs), len(sites))

	// Generate 30 days of dispositioned events. The distribution is
	// shaped to match what a real SOC tells customers: most alarms
	// are noise (animals, weather, lighting), a smaller slice are
	// activity-without-action (workers on schedule), and a thin
	// minority are verified threats. Response times follow a
	// log-normal-ish curve clamped to reasonable SOC SLAs.
	type dispMix struct {
		code   string
		label  string
		weight int
	}
	dispositions := []dispMix{
		{"false-positive-shadow", "False Positive — Shadow / Light", 22},
		{"false-positive-animal", "False Positive — Animal", 18},
		{"false-positive-weather", "False Positive — Weather / Foliage", 14},
		{"false-positive-vehicle", "False Positive — Authorized Vehicle", 8},
		{"no-action-scheduled-activity", "Activity Within Schedule", 12},
		{"activity-logs", "Logged Activity — No Action", 8},
		{"verified-threat-trespasser", "Verified — Trespasser, Deterrence Fired", 8},
		{"verified-threat-attempted-entry", "Verified — Attempted Entry, Police Dispatched", 4},
		{"verified-threat-vandalism", "Verified — Vandalism in Progress", 3},
		{"test-system-test", "Test — System Verification", 3},
	}
	totalWeight := 0
	for _, d := range dispositions {
		totalWeight += d.weight
	}
	pickDisp := func(r *rand.Rand) dispMix {
		n := r.Intn(totalWeight)
		for _, d := range dispositions {
			if n < d.weight {
				return d
			}
			n -= d.weight
		}
		return dispositions[0]
	}

	severityFor := func(code string, r *rand.Rand) string {
		switch {
		case strings.HasPrefix(code, "verified"):
			if r.Intn(3) == 0 {
				return "critical"
			}
			return "high"
		case strings.HasPrefix(code, "activity-logs"), strings.HasPrefix(code, "no-action"):
			return "low"
		default:
			// false-positives skew low/medium
			if r.Intn(4) == 0 {
				return "high"
			}
			if r.Intn(2) == 0 {
				return "medium"
			}
			return "low"
		}
	}

	eventTypes := []string{"intrusion", "linecross", "person", "vehicle", "loitering"}
	operators := []struct {
		ID, Callsign string
	}{
		{"op-001", "JHAYES"},
		{"op-002", "CTORRES"},
		{"op-003", "RMORGAN"},
	}

	r := rand.New(rand.NewSource(42)) // deterministic seed → stable demo
	now := time.Now()
	startWindow := now.Add(-30 * 24 * time.Hour)

	// Roughly 6 events per site per day, more during business hours.
	// Total: 5 sites × 30 days × ~6 = ~900 events. Plenty for the
	// "we handled X events" panel to show convincing numbers.
	eventCount := 0
	alarmCount := 0
	for _, s := range sites {
		// Per-site volume varies — bigger sites get more events.
		dailyAvg := 4 + r.Intn(5) // 4-8 events/day per site
		for day := 0; day < 30; day++ {
			eventsToday := dailyAvg + r.Intn(4) - 2 // ±2 jitter
			if eventsToday < 0 {
				eventsToday = 0
			}
			for i := 0; i < eventsToday; i++ {
				// Time of day: bias toward 18:00-06:00 (active monitoring window).
				// 70% events at night, 30% during the day.
				var hour int
				if r.Intn(10) < 7 {
					// Night hours: 18-23 or 0-5
					if r.Intn(2) == 0 {
						hour = 18 + r.Intn(6)
					} else {
						hour = r.Intn(6)
					}
				} else {
					hour = 6 + r.Intn(12) // 6-17
				}
				dayBase := startWindow.Add(time.Duration(day) * 24 * time.Hour)
				eventTime := time.Date(dayBase.Year(), dayBase.Month(), dayBase.Day(),
					hour, r.Intn(60), r.Intn(60), 0, time.UTC)
				if eventTime.After(now) {
					continue // don't seed events in the future
				}

				disp := pickDisp(r)
				sev := severityFor(disp.code, r)
				op := operators[r.Intn(len(operators))]
				evType := eventTypes[r.Intn(len(eventTypes))]

				// Response time: log-ish curve. Most under 60s; tail to ~5min.
				// Verified threats get faster response; informational slower.
				baseSec := 20 + r.Intn(60)
				if strings.HasPrefix(disp.code, "verified") {
					baseSec = 8 + r.Intn(25) // 8-33s
				} else if strings.HasPrefix(disp.code, "test") {
					baseSec = 3 + r.Intn(8)
				}
				// Long-tail: 10% of responses run 2-5min
				if r.Intn(10) == 0 {
					baseSec = 120 + r.Intn(180)
				}
				ackTime := eventTime.Add(time.Duration(baseSec) * time.Second)

				// Resolved-at trails ack by another 30-180s while the
				// operator finishes notes / disposition.
				resolvedAt := ackTime.Add(time.Duration(30+r.Intn(150)) * time.Second)

				alarmID := fmt.Sprintf("ALM-%s-%04d", eventTime.Format("060102"), eventCount+1)
				eventID := fmt.Sprintf("EVT-%s-%04d", eventTime.Format("060102"), eventCount+1)
				camID := fmt.Sprintf("%s-CAM-%02d", s.ID, 1+r.Intn(6))

				// Active alarm row — needed for the response-time
				// metrics (acknowledged_at - ts).
				_, err := db.Pool.Exec(ctx, `
					INSERT INTO active_alarms
					  (id, alarm_code, site_id, site_name, camera_id, camera_name,
					   severity, type, description, ts, acknowledged, acknowledged_at,
					   acknowledged_by_callsign, sla_deadline_ms, organization_id, created_at)
					VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,true,$11,$12,$13,$14,$15)
					ON CONFLICT (id) DO NOTHING`,
					alarmID,
					fmt.Sprintf("ALM-%s-%04d", eventTime.Format("060102"), alarmCount+1),
					s.ID, s.Name,
					camID, fmt.Sprintf("%s Camera %d", s.Name, 1+r.Intn(6)),
					sev, evType,
					fmt.Sprintf("%s detected at %s", evType, s.Name),
					eventTime.UnixMilli(),
					ackTime,
					op.Callsign,
					eventTime.UnixMilli()+90_000, // 90s SLA
					s.OrgID,
					eventTime,
				)
				if err != nil {
					log.Printf("[SEED] active_alarm: %v", err)
					continue
				}

				// Action log: 2-4 entries per event so the timeline panel renders.
				actionLog := []map[string]interface{}{
					{"ts": eventTime.UnixMilli(), "text": "Alarm received", "auto": true},
					{"ts": ackTime.Add(-2 * time.Second).UnixMilli(), "text": fmt.Sprintf("Operator %s engaged", op.Callsign), "auto": false},
					{"ts": ackTime.UnixMilli(), "text": fmt.Sprintf("Disposition: %s", disp.label), "auto": false},
				}
				if strings.HasPrefix(disp.code, "verified-threat-trespasser") {
					actionLog = append(actionLog, map[string]interface{}{
						"ts": ackTime.Add(15 * time.Second).UnixMilli(), "text": "Audio deterrence fired (3x)", "auto": false,
					})
				}
				if disp.code == "verified-threat-attempted-entry" {
					actionLog = append(actionLog, map[string]interface{}{
						"ts": ackTime.Add(20 * time.Second).UnixMilli(), "text": "Police dispatched (911)", "auto": false,
					})
				}
				actionLogJSON, _ := json.Marshal(actionLog)

				notes := ""
				switch {
				case disp.code == "verified-threat-trespasser":
					notes = "Single subject in restricted zone. Deterrence audio fired; subject left within 30s."
				case disp.code == "verified-threat-attempted-entry":
					notes = "Two subjects testing perimeter gate. PD dispatched; arrived in 6m."
				case disp.code == "false-positive-animal":
					notes = "Confirmed wildlife (deer / coyote pattern). No action."
				case disp.code == "false-positive-shadow":
					notes = "Tree shadow movement triggered detection. False positive."
				}

				_, err = db.Pool.Exec(ctx, `
					INSERT INTO security_events
					  (id, alarm_id, site_id, camera_id, severity, type, description,
					   disposition_code, disposition_label, operator_id, operator_callsign,
					   operator_notes, action_log, ts, resolved_at)
					VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
					ON CONFLICT (id) DO NOTHING`,
					eventID, alarmID, s.ID, camID,
					sev, evType,
					fmt.Sprintf("%s detected at %s", evType, s.Name),
					disp.code, disp.label,
					op.ID, op.Callsign, notes,
					actionLogJSON,
					eventTime.UnixMilli(),
					resolvedAt.UnixMilli(),
				)
				if err != nil {
					log.Printf("[SEED] security_event: %v", err)
					continue
				}

				eventCount++
				alarmCount++
			}
		}
	}

	log.Printf("[SEED] %d alarms + %d dispositioned events seeded across %d sites",
		alarmCount, eventCount, len(sites))
	return nil
}
