# Milesight Camera Driver — Implementation Brief

**Target audience:** Claude Code (or any engineer), working inside the VMS repository.
**Deliverable:** A Go sub-package at `internal/drivers/milesight/` that implements the VMS's existing `CameraDriver` interface, supports the Milesight camera families the VMS needs to manage, owns the full RTSP session, emits Annex-B H.264/H.265 NAL units with PTS, streams ONVIF events, and surfaces per-object tracking coordinates from the `vnd.onvif.metadata` RTP track.

---

## 0. Orient yourself before writing any code

Read the following in the VMS repo **before you start implementing**, and only start writing the driver once you understand them:

1. **The `CameraDriver` interface** — the top-level contract every driver implements. Likely lives in `internal/drivers/`, `pkg/drivers/`, `internal/camera/`, or similar. Grep for `type CameraDriver interface` and `type Driver interface`.
2. **All types it references** — `Frame`, `Stream`, `Event`, `EventSink`, `PTZController`, `Config`, etc. Walk the referenced types one level out.
3. **One existing camera driver** (if any exists) — to match style, logging, error handling, lifecycle.
4. **The RTSP helpers the VMS already uses** — library choice, Digest handling, PTS units, backpressure convention, SPS/PPS emission strategy on reconnect.

Only after those are clear: implement this driver.

If any of those files are missing or you can't find equivalents, **ask the human** rather than invent an interface. Guessing costs a rewrite later.

---

## 1. Mission

Build a `CameraDriver`-conforming Go package that manages Milesight IP cameras, with all the per-firmware quirks already discovered for the MS-C8266-FIGPE baked in so the driver also works for similar firmwares in the C2xxx/C5xxx/C8xxx standard-PoE line and the PTZ-dome line.

The driver must:

- Discover the camera (ONVIF device service + clock sync + service endpoint map).
- Authenticate correctly across all ONVIF operations (dual-auth; see §3).
- Rewrite every NAT-leaked URL it receives from the camera (see §3).
- Own the full RTSP session: connect, demux video/audio/metadata tracks, emit Annex-B NAL units with PTS for downstream consumption, and publish parsed metadata frames.
- Subscribe to ONVIF PullPoint events and emit a normalized `Event` stream.
- Expose persisted rule geometry (tripwires, regions) and, where PTZ is supported, PTZ controls.
- Do all of this against multiple Milesight families with capability probing — PTZ domes advertise the PTZ service; standard bullets don't; the driver no-ops gracefully.

---

## 2. Target camera families

| Family | Example model | Connection | ONVIF | PTZ | Analytics | Known quirks |
|---|---|---|---|---|---|---|
| Cellular bullet | MS-C8266-FIGPE | 5G/cellular | Yes | No | Yes | NAT rewriting critical |
| Standard PoE | MS-C2xxx / C5xxx / C8xxx | PoE/LAN | Yes | No | Yes | May or may not need NAT rewriting (LAN IPs still returned) |
| PTZ dome | MS-C2xxx-PTZ / C8xxx-PTZ | PoE/LAN | Yes | Yes | Yes | Adds PTZ service; rest identical |

Assume all share the same gSOAP/2.8 ONVIF stack and LIVE555 RTSP server. Do **not** special-case by model string when a capability probe will do. The driver branches on `GetServices` results, not on `Model` string.

**Out of scope:** Milesight NVRs (MS-N5xxx / N7xxx). Those use a different REST API (LAPI) and should be a separate driver if the VMS ever needs them.

---

## 3. Hard requirements discovered from live probing

These are the non-obvious behaviors the driver *must* implement. Every one of them was learned from iterative probing against a real MS-C8266-FIGPE; they are not speculation.

### 3.1 Dual-auth on ONVIF

Every authenticated ONVIF call must carry **both**:

- WS-Security `UsernameToken` with `PasswordDigest` in the SOAP header, and
- HTTP Digest auth at the transport layer (handled by the HTTP client / RoundTripper).

If you send only WS-Security, the camera still authenticates the user but maps them to a minimal read-only role. Calls like `GetProfiles`, `GetEventProperties`, `CreatePullPointSubscription`, `GetNetworkInterfaces`, `GetUsers`, `GetRules` will return a SOAP fault with subcode `ter:NotAuthorized` even when the user is Administrator. Adding HTTP Digest alongside elevates to the full role and unlocks everything.

**WS-Security envelope shape that worked (copy this exactly):**

```xml
<wsse:Security s:mustUnderstand="1">
  <wsse:UsernameToken>
    <wsse:Username>{user}</wsse:Username>
    <wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">{digest}</wsse:Password>
    <wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">{nonce_b64}</wsse:Nonce>
    <wsu:Created>{created}</wsu:Created>
  </wsse:UsernameToken>
</wsse:Security>
```

Where:

- `nonce` = 16 random bytes; `nonce_b64` = base64 of those bytes.
- `created` = ISO-8601 UTC timestamp, **aligned to the camera's clock, not local clock** (see §3.4).
- `digest` = base64( SHA1( nonce_bytes || created_bytes || password_bytes ) ).

### 3.2 NAT-aware URL rewriting

Cameras behind NAT (cellular most obviously, but also any camera behind a router) return their *internal* IP in every URL they hand back. Specifically:

- The `XAddr` elements in `GetServices` responses.
- The `wsa:Address` in the `CreatePullPointSubscriptionResponse` subscription reference.
- The `Uri` returned by `GetStreamUri`.
- The `Content-Base` header and `o=` origin line in RTSP DESCRIBE responses.

Example from the real camera: `GetServices` returned `http://192.168.103.221:80/onvif/Media` — that IP is the T-Mobile APN-private IP the camera sees on its own interface. If the driver is reaching the camera via `162.191.235.243`, that internal URL is unreachable.

**Rule:** after parsing any such response, rewrite every returned URL's host and port to match the public host/port the driver used to reach the ONVIF device service. Preserve scheme, path, query, fragment, and userinfo. This applies to both HTTP and RTSP URLs.

### 3.3 RTSP Digest on a single TCP socket

LIVE555 on this firmware binds Digest nonces to the TCP connection. If the driver opens a fresh socket for the authed retry, the server rejects the retry with a fresh `401` carrying a new nonce. The authed retry must ride the **same socket** as the initial challenge.

The `uri` field in the Digest `Authorization` header must be the **exact URL in the request line** (userinfo stripped). Do not include `algorithm=MD5` in the Authorization header unless the server's challenge includes it — LIVE555's does not, and some pedantic servers reject extras.

Note: most RTSP libraries already handle Digest correctly. If the VMS uses `gortsplib/v4` or similar, this is solved. If the driver implements RTSP itself, enforce the single-socket rule explicitly.

### 3.4 Clock skew handling

Bring up the ONVIF client in this order:

1. `GetSystemDateAndTime` — **no authentication**, part of the ONVIF PRE_AUTH set.
2. Compute `skew = camera_time - local_utc_time`.
3. Store the skew on the client.
4. Every subsequent WS-Security `Created` timestamp = `local_utc_time + skew`.

Do not trust HTTP `Date:` headers from the camera — gSOAP emits `Thu, 01 Jan 1970 ...` on some code paths even when the real clock is correct. `GetSystemDateAndTime` is reliable.

### 3.5 Capability-first, not model-first

Do not branch on the `Model` string returned by `GetDeviceInformation`. Instead, after `GetServices`, inspect which service namespaces are present:

| If present                                           | Then                                                      |
|------------------------------------------------------|-----------------------------------------------------------|
| `http://www.onvif.org/ver20/ptz/wsdl`                | Expose PTZ controls                                       |
| `http://www.onvif.org/ver20/analytics/wsdl`          | Read persisted rule geometry                              |
| `http://www.onvif.org/ver10/events/wsdl` + `WSPullPointSupport=true` | Enable PullPoint event stream               |
| `http://www.onvif.org/ver20/media/wsdl`              | Prefer Media2 if needed; fall back to Media1              |
| `vnd.onvif.metadata` track in SDP                    | Open the metadata RTP track                               |

The driver must work correctly on a camera that lacks any of these, silently disabling the features that depend on them.

---

## 4. Concrete reference data (use for unit test fixtures)

### 4.1 `GetServices` output, NAT-rewritten

```json
{
  "http://www.onvif.org/ver10/device/wsdl":    "http://<public>/onvif/device_service",
  "http://www.onvif.org/ver10/media/wsdl":     "http://<public>/onvif/Media",
  "http://www.onvif.org/ver10/events/wsdl":    "http://<public>/onvif/Events",
  "http://www.onvif.org/ver20/ptz/wsdl":       "http://<public>/onvif/PTZ",
  "http://www.onvif.org/ver20/imaging/wsdl":   "http://<public>/onvif/Imaging",
  "http://www.onvif.org/ver20/analytics/wsdl": "http://<public>/onvif/Analytics",
  "http://www.onvif.org/ver10/recording/wsdl": "http://<public>/onvif/Recording",
  "http://www.onvif.org/ver10/search/wsdl":    "http://<public>/onvif/Search",
  "http://www.onvif.org/ver10/replay/wsdl":    "http://<public>/onvif/Replay",
  "http://www.onvif.org/ver10/deviceIO/wsdl":  "http://<public>/onvif/deviceIO",
  "http://www.onvif.org/ver20/media/wsdl":     "http://<public>/onvif/media2"
}
```

### 4.2 Three RTSP profiles (all on the same camera)

| Profile token | Name | RTSP path | Video | Audio | Metadata |
|---|---|---|---|---|---|
| Profile_1 | MsMediaProfile1 | `/main`  | H.265 (pt 96) | PCMU (pt 0) | `vnd.onvif.metadata` (pt 98) |
| Profile_2 | MsMediaProfile2 | `/sub`   | H.264 (pt 96) | PCMU (pt 0) | `vnd.onvif.metadata` (pt 98) |
| Profile_3 | MsMediaProfile3 | `/third` | H.264 (pt 96) | PCMU (pt 0) | `vnd.onvif.metadata` (pt 98) |

All three carry an `m=application 0 RTP/AVP 98` track with `a=rtpmap:98 vnd.onvif.metadata/90000` and `a=control:track3`. All carry PCMU audio on track2. Main is H.265; sub and third are H.264.

### 4.3 The 17 event topics this firmware emits

```
tns1:VideoSource/MotionAlarm
tns1:Device/Trigger/DigitalInput
tns1:Device/Trigger/Relay
tns1:AudioAnalytics/Audio/DetectedSound
tns1:RuleEngine/CellMotionDetector/Motion
tns1:RuleEngine/TamperDetector/Tamper
tns1:RuleEngine/LineDetector/Crossed
tns1:RuleEngine/FieldDetector/ObjectsInside
tns1:RuleEngine/FieldInDetector/ObjectsInside
tns1:RuleEngine/FieldOutDetector/ObjectsOutside
tns1:RuleEngine/MotionDetector/Motion
tns1:RuleEngine/LoiteringDetector/Loitering
tns1:RuleEngine/ObjectLeftDetector/ObjectLeft
tns1:RuleEngine/ObjectRemoveDetector/ObjectRemove
tns1:RuleEngine/CounterDetector/Counter
tns1:RuleEngine/RegionalCounter/Count
tns1:RuleEngine/AudioDetector/Audio
```

### 4.4 Canonical event payload shape

Every `NotificationMessage` follows this structure:

```xml
<ns0:NotificationMessage
    xmlns:ns0="http://docs.oasis-open.org/wsn/b-2"
    xmlns:ns1="http://www.onvif.org/ver10/schema">
  <ns0:Topic Dialect="...ConcreteSet">tns1:RuleEngine/FieldDetector/ObjectsInside</ns0:Topic>
  <ns0:Message>
    <ns1:Message UtcTime="2026-04-22T13:30:31Z" PropertyOperation="Changed">
      <ns1:Source>
        <ns1:SimpleItem Name="VideoSourceConfigurationToken"  Value="VideoSource"/>
        <ns1:SimpleItem Name="VideoAnalyticsConfigurationToken" Value="VideoAnalyticsToken"/>
        <ns1:SimpleItem Name="Rule" Value="MyFieldDetectorRule1"/>
      </ns1:Source>
      <ns1:Key>
        <ns1:SimpleItem Name="ObjectId" Value="0"/>
      </ns1:Key>
      <ns1:Data>
        <ns1:SimpleItem Name="IsInside" Value="true"/>
      </ns1:Data>
    </ns1:Message>
  </ns0:Message>
</ns0:NotificationMessage>
```

Notes for the parser:

- `PropertyOperation` is `Initialized` (sent when PullPoint subscription starts), `Changed` (a real transition), or `Deleted`.
- `Source` always contains the `Rule` name plus VideoSource/VideoAnalytics config tokens (for rule-engine events). Non-rule events use `InputToken`, `RelayToken`, etc.
- `Key` is a placeholder for rule events (`ObjectId="0"`). Real per-object tracking IDs live on the metadata RTP track.
- `Data` carries the state booleans: `IsInside`, `IsCrossed`, `IsMotion`, `IsLoitering`, `IsLeft`, `IsRemove`, `IsTamper`, `IsAudio`, `IsCounter`, `isSoundDetected`, `State`, `LogicalState`, `Count`. Per topic. See §4.5.

### 4.5 State field by topic

| Topic | Data state field | Type |
|---|---|---|
| VideoSource/MotionAlarm | `State` | bool |
| Device/Trigger/DigitalInput | `LogicalState` | bool |
| Device/Trigger/Relay | `LogicalState` | `active` / `inactive` |
| AudioAnalytics/Audio/DetectedSound | `isSoundDetected` | bool |
| RuleEngine/CellMotionDetector/Motion | `IsMotion` | bool |
| RuleEngine/TamperDetector/Tamper | `IsTamper` | bool |
| RuleEngine/LineDetector/Crossed | `IsCrossed` | bool |
| RuleEngine/FieldDetector/ObjectsInside | `IsInside` | bool |
| RuleEngine/FieldInDetector/ObjectsInside | `IsInside` | bool |
| RuleEngine/FieldOutDetector/ObjectsOutside | `IsOutside` | bool |
| RuleEngine/MotionDetector/Motion | `IsMotion` | bool |
| RuleEngine/LoiteringDetector/Loitering | `IsLoitering` | bool |
| RuleEngine/ObjectLeftDetector/ObjectLeft | `IsLeft` | bool |
| RuleEngine/ObjectRemoveDetector/ObjectRemove | `IsRemove` | bool |
| RuleEngine/CounterDetector/Counter | `IsCounter` | bool (trigger), separate `Count` on totals |
| RuleEngine/RegionalCounter/Count | `IsCounter` | bool |
| RuleEngine/AudioDetector/Audio | `IsAudio` | bool |

### 4.6 Rule geometry (from `GetRules`)

Coordinate space is **integer 0..1024** on both axes (not ONVIF-standard -1..+1 floats). Convert to pixels as `px = x * frameW / 1024`, `py = y * frameH / 1024`. Rule XML shapes:

```xml
<!-- Line detector (tripwire) -->
<tt:Rule Type="tt:LineDetector" Name="MyLineDetectorRule1">
  <tt:Parameters>
    <tt:SimpleItem Name="Enable"      Value="true|false"/>
    <tt:SimpleItem Name="Sensitivity" Value="0..100"/>
    <tt:SimpleItem Name="Direction"   Value="0|1|2"/>   <!-- 0=LR, 1=RL, 2=both -->
    <tt:ElementItem Name="Segments">
      <tt:Polyline>
        <tt:Point x="N" y="N"/>
        <tt:Point x="N" y="N"/>
      </tt:Polyline>
    </tt:ElementItem>
  </tt:Parameters>
</tt:Rule>

<!-- Field / region / intrusion / loitering / object-left / object-remove etc. -->
<tt:Rule Type="tt:FieldDetector" Name="MyFieldDetectorRule1">
  <tt:Parameters>
    <tt:SimpleItem Name="Enable" Value="true|false"/>
    <tt:ElementItem Name="Field">
      <tt:PolygonConfiguration>
        <tt:Polygon>
          <tt:Point x="0" y="0"/>
          <tt:Point x="0" y="1024"/>
          <tt:Point x="1024" y="1024"/>
          <tt:Point x="1024" y="0"/>
        </tt:Polygon>
      </tt:PolygonConfiguration>
    </tt:ElementItem>
  </tt:Parameters>
</tt:Rule>
```

### 4.7 ONVIF Metadata RTP track format

Payload type 98, clock rate 90000, MIME `vnd.onvif.metadata`. Track control URI is `track3` aggregate-controlled via the session-level `control:*`.

Once SETUP+PLAY is done on that track, RTP packets carry UTF-8 XML fragments that the sender eventually concatenates into a `<tt:MetadataStream>` document. Each `<tt:Frame>` contains object-level geometry:

```xml
<tt:MetadataStream xmlns:tt="http://www.onvif.org/ver10/schema">
  <tt:VideoAnalytics>
    <tt:Frame UtcTime="2026-04-22T13:30:31.120Z">
      <tt:Transformation>
        <tt:Translate x="-1.0" y="-1.0"/>
        <tt:Scale x="0.001953125" y="0.001953125"/>   <!-- = 2/1024 -->
      </tt:Transformation>
      <tt:Object ObjectId="42">
        <tt:Appearance>
          <tt:Shape>
            <tt:BoundingBox left="-0.2" top="-0.3" right="0.1" bottom="0.2"/>
            <tt:CenterOfGravity x="-0.05" y="-0.05"/>
          </tt:Shape>
          <tt:Class>
            <tt:Type Likelihood="0.9">Human</tt:Type>
          </tt:Class>
        </tt:Appearance>
      </tt:Object>
    </tt:Frame>
  </tt:VideoAnalytics>
</tt:MetadataStream>
```

BoundingBox coordinates on this track are **normalized -1..+1 ONVIF standard** — a different coordinate space from the rule geometry. The driver must handle both conversions.

The `ObjectId` on this track is the real per-object tracking ID. Events on PullPoint can be correlated to metadata-track objects by UTC timestamp (ObjectId in rule-engine events is always `"0"` placeholder on this firmware).

---

## 5. Proposed package layout

```
internal/drivers/milesight/
├── driver.go          # Top-level type that implements the VMS's CameraDriver interface.
│                      # Owns lifecycle (connect/close), fans events out to the sink,
│                      # and wires the ONVIF + RTSP sub-packages together.
├── config.go          # Per-camera config: host, port, user, password, profile selection.
├── capabilities.go    # Struct recording what ONVIF services + features were discovered.
├── errors.go          # Driver-specific sentinel errors + error wrapping.
│
├── auth/
│   ├── wssec.go       # BuildWSSecurityHeader(user, pass, skew) -> string
│   ├── digest.go      # http.RoundTripper that adds HTTP Digest after a 401 challenge.
│   └── clock.go       # Tracks camera-vs-local skew; SkewedNow() -> time.Time.
│
├── onvif/
│   ├── client.go      # SOAP client: uses an http.Client with Digest RoundTripper.
│   │                  # Wraps every authed call in ws-sec envelope + HTTP digest.
│   ├── envelope.go    # Build/parse SOAP envelopes, detect and unwrap SOAP faults.
│   ├── nat.go         # RewriteHost(url, publicHost, publicPort) - §3.2.
│   ├── device.go      # GetSystemDateAndTime, GetDeviceInformation, GetCapabilities,
│   │                  # GetServices, GetNetworkInterfaces, GetUsers, GetScopes.
│   ├── media.go       # GetProfiles, GetStreamUri (Media1 or Media2 depending on caps).
│   ├── events.go      # GetEventProperties, CreatePullPointSubscription, PullMessages.
│   ├── analytics.go   # GetRules, GetAnalyticsModules.
│   ├── ptz.go         # GetConfigurations, ContinuousMove, Stop, Goto/SetPreset (if PTZ).
│   └── types.go       # Go structs for all parsed response types we care about.
│
├── rtsp/
│   ├── session.go     # Opens RTSP to the selected profile. Uses the VMS's chosen RTSP
│   │                  # library (e.g. gortsplib). Delivers NAL units + audio + metadata.
│   └── metadata.go    # Depacketizes RTP PT 98, concatenates XML fragments, parses
│                      # <tt:MetadataStream> -> MetadataFrame.
│
├── events/
│   └── parser.go      # Turn NotificationMessage XML into the VMS's Event type.
│
├── testdata/
│   ├── getservices_response.xml
│   ├── getrules_response.xml
│   ├── event_changed_fielddetector.xml
│   ├── event_initialized_linedetector.xml
│   ├── sdp_main_h265.sdp
│   ├── sdp_sub_h264.sdp
│   └── metadata_frame.xml
│
└── cmd/msprobe/
    └── main.go        # CLI tool — Go port of milesight_probe_v7.py.
                       # Run against a new Milesight model to verify the driver works
                       # on that firmware before adding it to production config.
```

---

## 6. Core Go types the driver will define

These are internal types. The driver adapts them to the VMS's `CameraDriver` interface, which you'll conform to *exactly* after reading the interface file.

```go
// Config is per-camera configuration passed in by the VMS.
type Config struct {
    Host       string        // 162.191.235.243
    HTTPPort   int           // 80 by default
    RTSPPort   int           // 554
    Username   string
    Password   string
    Profile    string        // "/main" | "/sub" | "/third" or profile token
    PullPeriod time.Duration // PullMessages Timeout (e.g. 5s)
    DialTimeout time.Duration
}

// Capabilities reflects what the camera advertised.
type Capabilities struct {
    HasPTZ       bool
    HasAnalytics bool
    HasEvents    bool
    HasMedia2    bool
    HasMetadata  bool   // vnd.onvif.metadata track present in SDP
    PullPointMax int
    SupportedVersions []string
    Manufacturer string
    Model        string
    Firmware     string
    Serial       string
}

// Event — normalized form emitted to the VMS.
type Event struct {
    UTCTime   time.Time
    Topic     string
    Operation string            // Initialized | Changed | Deleted
    Rule      string            // e.g. "MyLineDetectorRule1", empty if non-rule
    Source    map[string]string // everything in <Source>
    ObjectID  string            // from <Key>, placeholder "0" for rule events
    Data      map[string]string // IsInside, IsCrossed, State, LogicalState, ...
    Raw       []byte            // original NotificationMessage XML, for debugging
}

// Rule — persisted analytics geometry.
type Rule struct {
    Name        string
    Type        RuleType          // LineDetector, FieldDetector, Intrusion, ...
    Enabled     bool
    Sensitivity int
    Direction   int               // 0=LR 1=RL 2=both, LineDetector only
    Polyline    []Point           // LineDetector (0..1024 space)
    Polygon     []Point           // Field/Region detectors (0..1024 space)
    Raw         map[string]string
}
type Point struct{ X, Y int }
type RuleType string

// PixelPolyline / PixelPolygon — same rule geometry, converted.
func (r Rule) PixelPolyline(w, h int) []image.Point
func (r Rule) PixelPolygon(w, h int)  []image.Point

// MetadataFrame — one parsed <tt:Frame> from the RTSP metadata track.
type MetadataFrame struct {
    UTCTime time.Time
    Objects []TrackedObject
    Raw     []byte
}

type TrackedObject struct {
    ObjectID      int
    BBox          NormalizedBBox  // -1..+1 ONVIF normalized
    Center        NormalizedPoint
    Class         string           // "Human", "Vehicle" — if advertised
    Confidence    float32
    Attributes    map[string]string
}

type NormalizedBBox struct { Left, Top, Right, Bottom float32 }
type NormalizedPoint struct { X, Y float32 }
```

---

## 7. Implementation plan (phased)

Execute in this order. Each phase builds on the previous and has a clean stopping point. After each phase, run the acceptance checks and commit.

### Phase 1 — Skeleton + auth

- Create `internal/drivers/milesight/` with `driver.go`, `config.go`, `capabilities.go`, `errors.go`.
- Implement `auth/` package (WS-Security builder, HTTP Digest RoundTripper, Clock).
- Stub the `CameraDriver` interface methods on a `*Driver` type: `Open`, `Close`, reporting unimplemented for the rest.
- **Acceptance:** Unit tests for `BuildWSSecurityHeader` (matches the envelope shown in §3.1 byte-for-byte given fixed nonce+created+password) and for the `rewrite_host` algorithm on the fixture URLs in §4.1.

### Phase 2 — ONVIF Device service + capability discovery

- `onvif/client.go`, `onvif/envelope.go`, `onvif/nat.go`.
- `onvif/device.go`: `GetSystemDateAndTime` (no auth), `GetDeviceInformation`, `GetCapabilities`, `GetServices`.
- In `driver.Open`: call GetSystemDateAndTime → compute skew → do authed GetDeviceInformation + GetServices → rewrite all XAddrs → store Capabilities.
- **Acceptance:** Integration test against a real camera or a recorded SOAP fixture. Must populate `Capabilities` correctly from the fixture in §4.1.

### Phase 3 — ONVIF Media + RTSP URI discovery

- `onvif/media.go`: GetProfiles, GetStreamUri. Use Media2 if `Capabilities.HasMedia2`, else Media1.
- Rewrite the returned RTSP URI host/port.
- **Acceptance:** Given a camera with three profiles (main/sub/third), the driver must expose all three with correctly rewritten URLs. Profile selection by config `Profile` field.

### Phase 4 — ONVIF Events (PullPoint) + Event parser

- `onvif/events.go`: CreatePullPointSubscription + loop of PullMessages. Rewrite the subscription reference URL. Re-subscribe on termination (renew before expiry, or on explicit `TerminationTime` fault).
- `events/parser.go`: convert `NotificationMessage` XML to `Event`.
- Fan events into a `<-chan Event` (or the VMS's `EventSink`, whichever the interface dictates).
- **Acceptance:** Offline unit test using fixture event XML in §4.4 and §4.5. Live test: subscribe and verify Initialized snapshot for every topic in §4.3 is emitted at subscription time.

### Phase 5 — ONVIF Analytics rule geometry

- `onvif/analytics.go`: GetRules, GetAnalyticsModules.
- Parse into `[]Rule` with Polyline/Polygon in 0..1024 space. Offer `PixelPolyline(w,h)` / `PixelPolygon(w,h)` helpers.
- Cache at connect time; refresh on demand.
- **Acceptance:** Given the `getrules_response.xml` fixture, parses all rule types in §4.6 correctly. Parser handles unknown rule types gracefully (preserved in `Raw`).

### Phase 6 — RTSP session + NAL emission

- `rtsp/session.go`: open RTSP to the selected profile using the VMS's RTSP library. Ride Digest auth on a single socket. Hand Annex-B H.264/H.265 NAL units (with PTS) up to the VMS's existing frame-consumer type — look at the VMS's other drivers / stream-handler code to copy the exact emission pattern (SPS/PPS on keyframe, IDR handling, etc.).
- PCMU audio likewise if the VMS consumes audio from drivers.
- Respect the `Content-Base` URL and rewrite if needed.
- **Acceptance:** The VMS's existing recorder/viewer can play the stream end to end. Reconnection works. Backpressure is respected (don't leak RTP packets if downstream is slow).

### Phase 7 — ONVIF metadata RTP track

- `rtsp/metadata.go`: SETUP track3 (`vnd.onvif.metadata`), reassemble the XML stream from RTP packets, parse `<tt:Frame>` into `MetadataFrame`.
- Consider: Milesight RTP packetization for this track commonly splits the XML across packets with RTP marker bit on the last fragment, or uses a well-known sentinel. Treat RTP timestamp as frame boundary; concatenate until the marker bit.
- Fan `MetadataFrame` into its own channel / sink. Correlation with events is the VMS's job, but emit `UTCTime` on both sides so correlation is easy.
- **Acceptance:** Walk-test: as a human walks across the camera view, metadata frames show bounding boxes that sweep with the walker; at the same UTC timestamp (+/- 1 s), a matching PullPoint event arrives if a rule is enabled.

### Phase 8 — PTZ (only on PTZ-family models)

- `onvif/ptz.go`: GetConfigurations, ContinuousMove, Stop, (optionally GotoPreset, SetPreset, GetPresets).
- Expose through whatever PTZ interface the VMS defines. If the VMS has a `PTZController` interface, return an instance from `Driver.PTZController()` only when `Capabilities.HasPTZ`; otherwise return `nil, ErrNoPTZ`.
- **Acceptance:** On a real PTZ dome, VMS UI can pan/tilt/zoom. On a bullet, PTZ API calls return `ErrNoPTZ` cleanly.

### Phase 9 — `cmd/msprobe` CLI

- Port `milesight_probe_v7.py` to Go using the driver's own packages. Same outputs (text log + JSON), same purpose: point it at a new Milesight model before trusting the driver against it.
- **Acceptance:** Running `msprobe --host X --user admin --pw '***'` produces a `.txt` report with every section the Python probe produced (auth sanity, clock, services, profiles, rules, event properties, PullPoint snapshot, RTSP SDPs).

---

## 8. Anti-patterns — don't

- Don't call `Get*` operations with bare WS-Security. Add HTTP Digest every time. (See §3.1.)
- Don't trust any URL returned by the camera. Run it through `RewriteHost` first. (See §3.2.)
- Don't open a new TCP socket for the RTSP Digest retry. (See §3.3.)
- Don't build `Created` timestamps from local time. Use `SkewedNow()`. (See §3.4.)
- Don't branch on `Model` string. Branch on `Capabilities`. (See §3.5.)
- Don't embed credentials in logged URLs. If anything the driver emits (logs, error messages, metrics, JSON exports) could contain a password, redact it. The `auth.Config` should expose a `Redact` helper.
- Don't decode video in the driver. The VMS said it wants Annex-B NAL units + PTS. Respect that.
- Don't parse the full analytics metadata XML eagerly — it's an append-only stream. Parse incrementally at frame boundaries (`<tt:Frame>` close tag) and hand off each frame.
- Don't assume all three profiles are H.264. Profile_1 is H.265 on this model, and will differ by model. Read SDP and set codec per profile.
- Don't assume the event topic tree is static across firmwares. Cache the result of `GetEventProperties` per camera; don't hard-code topic lists in the driver (§4.3 is for tests, not for production code paths).
- Don't try to filter events client-side by subscribing with a topic expression unless you've verified the camera supports that filter dialect. PullPoint with no filter + client-side filtering is more portable.

---

## 9. Edge cases worth covering

- **Clock wanders during a long session.** Re-run `GetSystemDateAndTime` periodically (every few minutes) and refresh the skew. WS-Security validity windows are typically 5 minutes.
- **PullPoint subscription terminates.** Either renew before `TerminationTime` using the renew endpoint, or accept termination and recreate on the next poll. Prefer renewal to avoid missing events during the recreate gap.
- **Camera reboots / network flaps.** Driver should enter a reconnection state, not crash. Event consumers see a `Disconnected` / `Reconnected` signal (whatever the VMS convention is).
- **Password rotation.** Driver config should be reloadable without restarting the VMS, or at minimum allow a `Reconfigure(Config)` call.
- **Large GetProfiles responses.** On this firmware, `GetProfiles` returned 123 KB. Don't log the full body at INFO level. Debug-level + redacted.
- **ONVIF user lacks admin role.** If `GetNetworkInterfaces` returns `ter:NotAuthorized` even after dual auth, the ONVIF user role is too low. Surface that specifically in the driver's connect error: "ONVIF user must have Administrator role."
- **No metadata track on this firmware.** Older Milesight firmwares may not include `vnd.onvif.metadata` in SDP. Driver must work without it; emit metadata-less events only.

---

## 10. Reference artifacts

Drop these into `internal/drivers/milesight/testdata/` (or equivalent location for fixtures in your VMS):

- `milesight_probe_v7.py` — the working Python probe that produced all this intelligence. Keep it for future Milesight models: pip install requests, run it against the new camera, feed the output back to the driver test suite.
- `milesight_probe_v7_example.json` — a real run's output (redacted). Use as ground truth for parser tests.

The Python probe and this brief are the full handoff; everything in the brief is derived from real `milesight_probe_v7.py` output against an MS-C8266-FIGPE on firmware 61.8.0.5-r3.

---

## 11. Definition of done

The driver is production-ready when:

1. It implements the VMS's `CameraDriver` interface with no TODO markers and no panics.
2. All unit tests in `internal/drivers/milesight/...` pass (`go test ./...`).
3. A real MS-C8266-FIGPE connects, emits events, streams video, and (if configured) publishes metadata frames over a sustained 24-hour session with no leaks or crashes.
4. Standard-PoE (C2xxx/C5xxx/C8xxx) and PTZ-dome (C2xxx-PTZ/C8xxx-PTZ) cameras also connect and stream with the capability-probed code paths lighting up appropriately — PTZ works on the dome, is absent on the bullet, without any model-string special cases in the driver.
5. `cmd/msprobe` runs against any of the above and produces a readable diagnostic report.
6. No credential leaks in logs, metrics, traces, or error messages (grep the repo for the word `password` / `pass` with value interpolation to confirm).
7. The driver degrades gracefully if a camera is offline, has a dead ONVIF user, rotates its IP, or lacks a capability.

That's the bar. When all seven are true, the driver is done.