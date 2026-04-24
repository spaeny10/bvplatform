package onvif

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// Client connects to an ONVIF device and provides device management operations
type Client struct {
	Address  string
	Username string
	Password string
	XAddr    string

	deviceInfo *DeviceInfo
	httpClient *http.Client
	timeOffset time.Duration
}

// DeviceInfo holds basic device information
type DeviceInfo struct {
	Manufacturer    string `json:"manufacturer"`
	Model           string `json:"model"`
	FirmwareVersion string `json:"firmware_version"`
	SerialNumber    string `json:"serial_number"`
}

// StreamProfile represents a media profile with stream URI
type StreamProfile struct {
	Token     string `json:"token"`
	Name      string `json:"name"`
	StreamURI string `json:"stream_uri"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	HasPTZ    bool   `json:"has_ptz"`
}

// NewClient creates a new ONVIF client
func NewClient(address, username, password string) *Client {
	// Normalize address
	if !strings.HasPrefix(address, "http") {
		address = "http://" + address
	}
	if !strings.Contains(address, "/onvif/device_service") {
		if !strings.HasSuffix(address, "/") {
			address += "/"
		}
		address += "onvif/device_service"
	}

	return &Client{
		Address:  address,
		Username: username,
		Password: password,
		XAddr:    address,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 4,
				MaxConnsPerHost:     4,
				IdleConnTimeout:     120 * time.Second,
				DisableKeepAlives:   false,
				ForceAttemptHTTP2:   false,
				DialContext: (&net.Dialer{
					Timeout:   3 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				TLSHandshakeTimeout:   3 * time.Second,
				ResponseHeaderTimeout: 10 * time.Second,
				TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

// Connect establishes connection and retrieves device information
func (c *Client) Connect(ctx context.Context) (*DeviceInfo, error) {
	log.Printf("[ONVIF] Connecting to %s...", c.Address)

	// Sync time to avoid WS-Security NotAuthorized errors
	camTime, err := c.GetSystemDateAndTime(ctx)
	if err == nil {
		c.timeOffset = camTime.Sub(time.Now().UTC())
		log.Printf("[ONVIF] Synced time with camera (offset: %v)", c.timeOffset)
	} else {
		log.Printf("[ONVIF] Warning: Failed to sync camera time: %v", err)
	}

	info, err := c.GetDeviceInformation(ctx)
	if err != nil {
		return nil, fmt.Errorf("get device info: %w", err)
	}

	c.deviceInfo = info
	log.Printf("[ONVIF] Connected: %s %s (FW: %s)", info.Manufacturer, info.Model, info.FirmwareVersion)
	return info, nil
}

// GetDeviceInformation retrieves device manufacturer, model, firmware
func (c *Client) GetDeviceInformation(ctx context.Context) (*DeviceInfo, error) {
	body := `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <s:Header>` + c.BuildSecurityHeader() + `</s:Header>
  <s:Body>
    <tds:GetDeviceInformation/>
  </s:Body>
</s:Envelope>`

	resp, err := c.DoRequest(ctx, c.XAddr, body)
	if err != nil {
		return nil, err
	}

	// Parse response
	type getDeviceInfoResponse struct {
		XMLName xml.Name `xml:"Envelope"`
		Body    struct {
			Response struct {
				Manufacturer    string `xml:"Manufacturer"`
				Model           string `xml:"Model"`
				FirmwareVersion string `xml:"FirmwareVersion"`
				SerialNumber    string `xml:"SerialNumber"`
			} `xml:"GetDeviceInformationResponse"`
		} `xml:"Body"`
	}

	var parsed getDeviceInfoResponse
	if err := xml.Unmarshal(resp, &parsed); err != nil {
		return nil, fmt.Errorf("parse device info: %w", err)
	}

	return &DeviceInfo{
		Manufacturer:    parsed.Body.Response.Manufacturer,
		Model:           parsed.Body.Response.Model,
		FirmwareVersion: parsed.Body.Response.FirmwareVersion,
		SerialNumber:    parsed.Body.Response.SerialNumber,
	}, nil
}

// GetSystemDateAndTime retrieves the camera's UTC time for WS-Security syncing
func (c *Client) GetSystemDateAndTime(ctx context.Context) (time.Time, error) {
	body := `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <s:Body><tds:GetSystemDateAndTime/></s:Body>
</s:Envelope>`

	resp, err := c.DoRequest(ctx, c.XAddr, body)
	if err != nil {
		return time.Time{}, err
	}

	type sysDateAndTime struct {
		XMLName xml.Name `xml:"Envelope"`
		Body    struct {
			Response struct {
				Time struct {
					UTCDateTime struct {
						Date struct{ Year, Month, Day int }     `xml:"Date"`
						Time struct{ Hour, Minute, Second int } `xml:"Time"`
					} `xml:"UTCDateTime"`
				} `xml:"SystemDateAndTime"`
			} `xml:"GetSystemDateAndTimeResponse"`
		} `xml:"Body"`
	}

	var parsed sysDateAndTime
	if err := xml.Unmarshal(resp, &parsed); err != nil {
		return time.Time{}, fmt.Errorf("parse system time: %w", err)
	}

	utc := parsed.Body.Response.Time.UTCDateTime
	if utc.Date.Year == 0 {
		return time.Time{}, fmt.Errorf("invalid time returned")
	}

	camTime := time.Date(utc.Date.Year, time.Month(utc.Date.Month), utc.Date.Day,
		utc.Time.Hour, utc.Time.Minute, utc.Time.Second, 0, time.UTC)
	return camTime, nil
}

// GetSnapshotURI retrieves the snapshot URI for a given profile token
func (c *Client) GetSnapshotURI(ctx context.Context, profileToken string) (string, error) {
	request := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
%s
<s:Body>
	<trt:GetSnapshotUri>
		<trt:ProfileToken>%s</trt:ProfileToken>
	</trt:GetSnapshotUri>
</s:Body>
</s:Envelope>`, c.BuildSecurityHeader(), profileToken)

	body, err := c.DoRequest(ctx, c.XAddr, request)
	if err != nil {
		return "", err
	}

	type getSnapshotURIResponse struct {
		XMLName xml.Name `xml:"Envelope"`
		Body    struct {
			Response struct {
				MediaUri struct {
					Uri string `xml:"Uri"`
				} `xml:"MediaUri"`
			} `xml:"GetSnapshotUriResponse"`
		} `xml:"Body"`
	}

	var resp getSnapshotURIResponse
	if err := xml.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("xml unmarshal GET_SNAPSHOT_URI: %w\nBody: %s", err, string(body))
	}

	return resp.Body.Response.MediaUri.Uri, nil
}

// FetchSnapshot downloads the image from the specified URI using Basic Authentication
func (c *Client) FetchSnapshot(ctx context.Context, uri string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", uri, nil)
	if err != nil {
		return nil, fmt.Errorf("create list request: %w", err)
	}

	// Add basic auth to the request
	if c.Username != "" && c.Password != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute snapshot request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code fetching snapshot: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// GetStreamURI retrieves the RTSP stream URI for a given profile token
func (c *Client) GetStreamURI(ctx context.Context, profileToken string) (string, error) {
	// First get media service address
	mediaAddr := strings.Replace(c.XAddr, "device_service", "media_service", 1)

	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:trt="http://www.onvif.org/ver10/media/wsdl"
            xmlns:tt="http://www.onvif.org/ver10/schema">
  <s:Header>%s</s:Header>
  <s:Body>
    <trt:GetStreamUri>
      <trt:StreamSetup>
        <tt:Stream>RTP-Unicast</tt:Stream>
        <tt:Transport><tt:Protocol>RTSP</tt:Protocol></tt:Transport>
      </trt:StreamSetup>
      <trt:ProfileToken>%s</trt:ProfileToken>
    </trt:GetStreamUri>
  </s:Body>
</s:Envelope>`, c.BuildSecurityHeader(), profileToken)

	resp, err := c.DoRequest(ctx, mediaAddr, body)
	if err != nil {
		return "", err
	}

	// Parse URI from response
	type getStreamURIResponse struct {
		XMLName xml.Name `xml:"Envelope"`
		Body    struct {
			Response struct {
				MediaUri struct {
					Uri string `xml:"Uri"`
				} `xml:"MediaUri"`
			} `xml:"GetStreamUriResponse"`
		} `xml:"Body"`
	}

	var parsed getStreamURIResponse
	if err := xml.Unmarshal(resp, &parsed); err != nil {
		return "", fmt.Errorf("parse stream URI: %w", err)
	}

	uri := parsed.Body.Response.MediaUri.Uri

	// Inject credentials into RTSP URI if needed
	if c.Username != "" && !strings.Contains(uri, "@") {
		uri = strings.Replace(uri, "rtsp://", fmt.Sprintf("rtsp://%s:%s@", c.Username, c.Password), 1)
	}

	log.Printf("[ONVIF] Stream URI for profile %s: %s", profileToken, sanitizeURI(uri))
	return uri, nil
}

// GetProfiles retrieves available media profiles
func (c *Client) GetProfiles(ctx context.Context) ([]StreamProfile, error) {
	mediaAddr := strings.Replace(c.XAddr, "device_service", "media_service", 1)

	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
  <s:Header>%s</s:Header>
  <s:Body>
    <trt:GetProfiles/>
  </s:Body>
</s:Envelope>`, c.BuildSecurityHeader())

	resp, err := c.DoRequest(ctx, mediaAddr, body)
	if err != nil {
		return nil, err
	}

	type profilesResponse struct {
		XMLName xml.Name `xml:"Envelope"`
		Body    struct {
			Response struct {
				Profiles []struct {
					Token string `xml:"token,attr"`
					Name  string `xml:"Name"`
					Video struct {
						Resolution struct {
							Width  int `xml:"Width"`
							Height int `xml:"Height"`
						} `xml:"Resolution"`
					} `xml:"VideoEncoderConfiguration"`
					PTZConfiguration struct {
						Token string `xml:"token,attr"`
						Name  string `xml:"Name"`
					} `xml:"PTZConfiguration"`
				} `xml:"Profiles"`
			} `xml:"GetProfilesResponse"`
		} `xml:"Body"`
	}

	var parsed profilesResponse
	if err := xml.Unmarshal(resp, &parsed); err != nil {
		return nil, fmt.Errorf("parse profiles: %w", err)
	}

	var profiles []StreamProfile
	for _, p := range parsed.Body.Response.Profiles {
		profile := StreamProfile{
			Token:  p.Token,
			Name:   p.Name,
			Width:  p.Video.Resolution.Width,
			Height: p.Video.Resolution.Height,
			HasPTZ: p.PTZConfiguration.Token != "",
		}

		// Get stream URI for each profile
		if uri, err := c.GetStreamURI(ctx, p.Token); err == nil {
			profile.StreamURI = uri
		}

		profiles = append(profiles, profile)
	}

	log.Printf("[ONVIF] Found %d profile(s)", len(profiles))
	return profiles, nil
}

// PTZMove starts continuous movement of the camera
func (c *Client) PTZMove(ctx context.Context, profileToken string, pan, tilt, zoom float64) error {
	ptzAddr := strings.Replace(c.XAddr, "device_service", "ptz", 1)

	// Format values properly (ONVIF requires between -1.0 and 1.0)
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl"
            xmlns:tt="http://www.onvif.org/ver10/schema">
  <s:Header>%s</s:Header>
  <s:Body>
    <tptz:ContinuousMove>
      <tptz:ProfileToken>%s</tptz:ProfileToken>
      <tptz:Velocity>
        <tt:PanTilt x="%f" y="%f"/>
        <tt:Zoom x="%f"/>
      </tptz:Velocity>
    </tptz:ContinuousMove>
  </s:Body>
</s:Envelope>`, c.BuildSecurityHeader(), profileToken, pan, tilt, zoom)

	resp, err := c.DoRequest(ctx, ptzAddr, body)
	if err != nil {
		return fmt.Errorf("ptz move: %w", err)
	}

	// Verify it's a success response (should return ContinuousMoveResponse)
	if !strings.Contains(string(resp), "ContinuousMoveResponse") {
		return fmt.Errorf("unexpected PTZ move response: %s", string(resp[:min(len(resp), 200)]))
	}

	return nil
}

// ── Profile G: Storage, Recording, Search, Replay ──
//
// Cameras with onboard SD cards expose ONVIF Profile G endpoints at
// /onvif/Recording, /onvif/Search, /onvif/Replay. We use them for two
// things: (1) surfacing SD health (card present? recordings populated?)
// in the ops dashboard, and (2) future fallback playback — if the
// server-side recording has a gap, pull the missing window from the
// camera's own storage.

// StorageConfiguration is the shape our internal SD-status endpoint
// returns. Flattens the Profile G GetStorageConfigurations/GetRecordingSummary
// responses to exactly what the UI needs.
type StorageConfiguration struct {
	// Present is true when at least one usable storage device is reported
	// by the camera. A missing/failed SD card returns an empty list.
	Present bool `json:"present"`
	// StorageType is the reported medium: "SD", "NAS", "Internal", etc.
	// Milesight reports the SD slot as "SD" when occupied.
	StorageType string `json:"storage_type,omitempty"`
	// RecordingCount is the number of recording handles the camera knows
	// about — zero means SD is either empty or recording isn't configured.
	RecordingCount int `json:"recording_count"`
	// DataFrom / DataUntil come from the Recording Summary. If Count is 0
	// these are epoch-zero sentinels.
	DataFrom  time.Time `json:"data_from,omitempty"`
	DataUntil time.Time `json:"data_until,omitempty"`
}

// GetStorageConfigurations asks the device for its configured storage
// backends. Returns the first usable device, or Present=false when the
// camera reports no storage (no SD card seated, or SD card not formatted
// by the camera's firmware).
func (c *Client) GetStorageConfigurations(ctx context.Context) (StorageConfiguration, error) {
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <s:Header>%s</s:Header>
  <s:Body><tds:GetStorageConfigurations/></s:Body>
</s:Envelope>`, c.BuildSecurityHeader())

	resp, err := c.DoRequest(ctx, c.XAddr, body)
	if err != nil {
		return StorageConfiguration{}, fmt.Errorf("storage config: %w", err)
	}
	raw := string(resp)

	out := StorageConfiguration{}
	// The response is XML; we probe a few known tag shapes rather than
	// defining a full XSD-generated struct. Milesight returns <tds:Type>SD</>
	// when a card is present.
	if strings.Contains(raw, "<tds:StorageConfiguration") || strings.Contains(raw, "<tds:LocalPath") {
		out.Present = true
		out.StorageType = extractXMLTag(raw, "Type")
		if out.StorageType == "" {
			out.StorageType = "SD"
		}
	}

	// Augment with Profile G's recording summary — tells us whether anything
	// is actually on the card. Fails gracefully when the Search service
	// isn't exposed (older firmware).
	summary, err := c.GetRecordingSummary(ctx)
	if err == nil {
		out.RecordingCount = summary.NumberRecordings
		out.DataFrom = summary.DataFrom
		out.DataUntil = summary.DataUntil
	}
	return out, nil
}

// RecordingSummary mirrors the ONVIF Search service's summary response.
// Zero values indicate an empty (or missing) SD card.
type RecordingSummary struct {
	NumberRecordings int       `json:"number_recordings"`
	DataFrom         time.Time `json:"data_from"`
	DataUntil        time.Time `json:"data_until"`
}

// GetRecordingSummary queries the camera's Search service for its
// aggregated recording window. A NumberRecordings of zero means the
// card is either empty, missing, or not yet enrolled in a recording job.
func (c *Client) GetRecordingSummary(ctx context.Context) (RecordingSummary, error) {
	searchAddr := strings.Replace(c.XAddr, "device_service", "Search", 1)
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:tse="http://www.onvif.org/ver10/search/wsdl">
  <s:Header>%s</s:Header>
  <s:Body><tse:GetRecordingSummary/></s:Body>
</s:Envelope>`, c.BuildSecurityHeader())
	resp, err := c.DoRequest(ctx, searchAddr, body)
	if err != nil {
		return RecordingSummary{}, err
	}
	raw := string(resp)

	out := RecordingSummary{}
	// Tag names inside the Summary block: DataFrom, DataUntil, NumberRecordings
	if v := extractXMLTag(raw, "NumberRecordings"); v != "" {
		fmt.Sscanf(v, "%d", &out.NumberRecordings)
	}
	if v := extractXMLTag(raw, "DataFrom"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			out.DataFrom = t
		}
	}
	if v := extractXMLTag(raw, "DataUntil"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			out.DataUntil = t
		}
	}
	return out, nil
}

// CameraRecording is a single recording handle on the camera's storage.
// Multiple recordings per camera are common — one per channel or per
// schedule window. We surface RecordingToken to pass back to Replay.
type CameraRecording struct {
	Token       string    `json:"token"`
	Source      string    `json:"source"` // e.g. "IP Camera"
	EarliestRec time.Time `json:"earliest_recording,omitempty"`
	LatestRec   time.Time `json:"latest_recording,omitempty"`
}

// GetRecordings lists every recording handle the camera knows about.
// Called sparingly — Profile G searches should use FindRecordings with a
// time window, not walk this list.
func (c *Client) GetRecordings(ctx context.Context) ([]CameraRecording, error) {
	recAddr := strings.Replace(c.XAddr, "device_service", "Recording", 1)
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:trc="http://www.onvif.org/ver10/recording/wsdl">
  <s:Header>%s</s:Header>
  <s:Body><trc:GetRecordings/></s:Body>
</s:Envelope>`, c.BuildSecurityHeader())
	resp, err := c.DoRequest(ctx, recAddr, body)
	if err != nil {
		return nil, fmt.Errorf("get recordings: %w", err)
	}
	raw := string(resp)

	// Rough extraction — one CameraRecording per <RecordingItem> block.
	// Full XSD unmarshal would be cleaner but this package deliberately
	// avoids the dependency; the shape is stable across Milesight firmware.
	var out []CameraRecording
	idx := 0
	for {
		open := strings.Index(raw[idx:], "<trc:RecordingItem")
		if open < 0 {
			break
		}
		open += idx
		closeAt := strings.Index(raw[open:], "</trc:RecordingItem>")
		if closeAt < 0 {
			break
		}
		block := raw[open : open+closeAt]
		idx = open + closeAt + len("</trc:RecordingItem>")

		token := ""
		if m := strings.Index(block, `token="`); m >= 0 {
			rest := block[m+len(`token="`):]
			if end := strings.Index(rest, `"`); end >= 0 {
				token = rest[:end]
			}
		}
		item := CameraRecording{
			Token:  token,
			Source: extractXMLTag(block, "Source"),
		}
		if v := extractXMLTag(block, "EarliestRecording"); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				item.EarliestRec = t
			}
		}
		if v := extractXMLTag(block, "LatestRecording"); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				item.LatestRec = t
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// GetReplayUri asks the camera for an RTSP URL that streams a stored
// recording back in real-time. Used by the fallback playback path —
// when local server recording has a gap, we point the player at the
// camera's own replay URL for the missing window.
func (c *Client) GetReplayUri(ctx context.Context, recordingToken string) (string, error) {
	replayAddr := strings.Replace(c.XAddr, "device_service", "Replay", 1)
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:trp="http://www.onvif.org/ver10/replay/wsdl"
            xmlns:tt="http://www.onvif.org/ver10/schema">
  <s:Header>%s</s:Header>
  <s:Body>
    <trp:GetReplayUri>
      <trp:StreamSetup>
        <tt:Stream>RTP-Unicast</tt:Stream>
        <tt:Transport><tt:Protocol>RTSP</tt:Protocol></tt:Transport>
      </trp:StreamSetup>
      <trp:RecordingToken>%s</trp:RecordingToken>
    </trp:GetReplayUri>
  </s:Body>
</s:Envelope>`, c.BuildSecurityHeader(), escapeXML(recordingToken))
	resp, err := c.DoRequest(ctx, replayAddr, body)
	if err != nil {
		return "", fmt.Errorf("get replay uri: %w", err)
	}
	uri := extractXMLTag(string(resp), "Uri")
	if uri == "" {
		return "", fmt.Errorf("replay response missing Uri field")
	}
	return uri, nil
}

// extractXMLTag returns the text between the first pair of <…:Tag> and
// </…:Tag> elements in raw, regardless of namespace prefix. Naive but
// adequate for the shallow, well-formed responses ONVIF cameras emit.
func extractXMLTag(raw, tag string) string {
	idx := 0
	for {
		open := strings.Index(raw[idx:], ":"+tag+">")
		if open < 0 {
			return ""
		}
		open += idx + len(":"+tag+">")
		closeAt := strings.Index(raw[open:], "</")
		if closeAt < 0 {
			return ""
		}
		val := strings.TrimSpace(raw[open : open+closeAt])
		if val != "" {
			return val
		}
		idx = open + closeAt
	}
}

// Known Milesight relay output tokens. These are consistent across the
// current deterrence-capable models. Cameras expose them via GetRelayOutputs
// on the DeviceIO service; tokens below match what we probed in the field.
const (
	RelayTokenAlarmOut     = "AlarmOut_0"
	RelayTokenWarningLight = "Warning Light"
	RelayTokenSounder      = "Sounder"
)

// SetRelayOutputState flips a digital output on the camera. Used for active
// deterrence: triggering a strobe ("Warning Light") or siren ("Sounder")
// when an operator confirms a real threat. For monostable outputs (the
// default for all three Milesight relays) the camera auto-releases after
// its configured DelayTime — usually 10 s for strobe/siren and 1 s for the
// generic alarm output. Caller should not re-assert immediately; the
// re-fire helper handles sustained activation.
//
// state is either "active" (energize) or "inactive" (release, for cameras
// running in Bistable mode). Milesight's monostable relays ignore
// "inactive" after a fired pulse.
//
// The SOAP body uses the device/wsdl namespace, NOT deviceIO/wsdl — we
// learned this by probing the camera directly. Milesight rejects the
// deviceIO namespace with "method not implemented".
func (c *Client) SetRelayOutputState(ctx context.Context, token, state string) error {
	if state != "active" && state != "inactive" {
		return fmt.Errorf("state must be 'active' or 'inactive', got %q", state)
	}
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <s:Header>%s</s:Header>
  <s:Body>
    <tds:SetRelayOutputState>
      <tds:RelayOutputToken>%s</tds:RelayOutputToken>
      <tds:LogicalState>%s</tds:LogicalState>
    </tds:SetRelayOutputState>
  </s:Body>
</s:Envelope>`, c.BuildSecurityHeader(), escapeXML(token), state)

	resp, err := c.DoRequest(ctx, c.XAddr, body)
	if err != nil {
		return fmt.Errorf("relay set: %w", err)
	}
	if !strings.Contains(string(resp), "SetRelayOutputStateResponse") {
		return fmt.Errorf("unexpected relay response: %s", string(resp[:min(len(resp), 300)]))
	}
	return nil
}

// TriggerStrobe fires the Warning Light relay on the camera. Convenience
// wrapper — the underlying call is identical for any output, but naming
// the use case makes audit logs and handler code clearer.
func (c *Client) TriggerStrobe(ctx context.Context) error {
	return c.SetRelayOutputState(ctx, RelayTokenWarningLight, "active")
}

// TriggerSiren fires the Sounder relay (audible alarm).
func (c *Client) TriggerSiren(ctx context.Context) error {
	return c.SetRelayOutputState(ctx, RelayTokenSounder, "active")
}

// TriggerAlarmOut fires the generic AlarmOut_0 relay. Useful when external
// deterrence (building-wide siren, gate interlock) is wired to this contact.
func (c *Client) TriggerAlarmOut(ctx context.Context) error {
	return c.SetRelayOutputState(ctx, RelayTokenAlarmOut, "active")
}

// escapeXML replaces characters that break SOAP bodies. Relay tokens
// contain spaces ("Warning Light") but rarely anything exotic; this is
// defence against future token names that include & < > " '.
func escapeXML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

// PTZStop stops all movement of the camera
func (c *Client) PTZStop(ctx context.Context, profileToken string) error {
	ptzAddr := strings.Replace(c.XAddr, "device_service", "ptz", 1)

	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl">
  <s:Header>%s</s:Header>
  <s:Body>
    <tptz:Stop>
      <tptz:ProfileToken>%s</tptz:ProfileToken>
      <tptz:PanTilt>true</tptz:PanTilt>
      <tptz:Zoom>true</tptz:Zoom>
    </tptz:Stop>
  </s:Body>
</s:Envelope>`, c.BuildSecurityHeader(), profileToken)

	resp, err := c.DoRequest(ctx, ptzAddr, body)
	if err != nil {
		return fmt.Errorf("ptz stop: %w", err)
	}

	if !strings.Contains(string(resp), "StopResponse") {
		return fmt.Errorf("unexpected PTZ stop response: %s", string(resp[:min(len(resp), 200)]))
	}

	return nil
}

// buildSecurityHeader creates a WS-Security UsernameToken header
func (c *Client) BuildSecurityHeader() string {
	if c.Username == "" {
		return ""
	}

	nonce := make([]byte, 16)
	rand.Read(nonce)
	created := time.Now().UTC().Add(c.timeOffset).Format(time.RFC3339)

	h := sha1.New()
	h.Write(nonce)
	h.Write([]byte(created))
	h.Write([]byte(c.Password))
	digest := base64.StdEncoding.EncodeToString(h.Sum(nil))
	nonce64 := base64.StdEncoding.EncodeToString(nonce)

	return fmt.Sprintf(`
    <Security xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd" s:mustUnderstand="1">
      <UsernameToken>
        <Username>%s</Username>
        <Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">%s</Password>
        <Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">%s</Nonce>
        <Created xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">%s</Created>
      </UsernameToken>
    </Security>`, c.Username, digest, nonce64, created)
}

// doRequest sends a SOAP request and returns the response body
// DoRequest sends a raw SOAP request and returns the response body.
func (c *Client) DoRequest(ctx context.Context, url, body string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")

	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ONVIF request to %s: %w", url, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ONVIF error (HTTP %d): %s", resp.StatusCode, string(data[:min(len(data), 500)]))
	}

	return data, nil
}

// sanitizeURI removes credentials from a URI for logging
func sanitizeURI(uri string) string {
	if idx := strings.Index(uri, "@"); idx > 0 {
		prefix := uri[:strings.Index(uri, "://")+3]
		rest := uri[idx:]
		return prefix + "***:***" + rest
	}
	return uri
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
