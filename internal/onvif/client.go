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

	// Per-service URLs discovered via GetCapabilities. The advertised
	// host is rewritten to match c.XAddr because NAT'd cameras report
	// their internal LAN IP, which is unreachable from outside.
	mediaURL     string
	ptzURL       string
	imagingURL   string
	eventsURL    string
	recordingURL string
	searchURL    string
	replayURL    string
	deviceIOURL  string
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
			// Timeouts sized for cellular-connected cameras — radio
			// negotiation alone can run several seconds on first dial.
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 4,
				MaxConnsPerHost:     4,
				IdleConnTimeout:     120 * time.Second,
				DisableKeepAlives:   false,
				ForceAttemptHTTP2:   false,
				DialContext: (&net.Dialer{
					Timeout:   12 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				TLSHandshakeTimeout:   8 * time.Second,
				ResponseHeaderTimeout: 15 * time.Second,
				// Camera firmware is all over the map on TLS — older
				// Milesight Sense series (SC4xx) tops out at TLS 1.0/1.1,
				// which Go 1.20+ refuses by default. Drop the floor here
				// so we can still talk to those cameras over HTTPS. Self-
				// signed certs on every IP camera mean we always skip
				// verification anyway.
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
					MinVersion:         tls.VersionTLS10,
				},
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

	// Discover per-service URLs. Best-effort — if GetCapabilities
	// fails or the camera doesn't advertise some service, we fall
	// back to the hard-coded path conventions in each method.
	if err := c.discoverServices(ctx); err != nil {
		log.Printf("[ONVIF] Service discovery failed (will use defaults): %v", err)
	}

	return info, nil
}

// discoverServices calls GetCapabilities and stores per-service URLs.
// Hostnames in the returned XAddrs are rewritten to match c.XAddr —
// the camera advertises its internal LAN address, but the platform
// reaches it via a public/NAT'd address that the user typed.
func (c *Client) discoverServices(ctx context.Context) error {
	body := `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <s:Header>` + c.BuildSecurityHeader() + `</s:Header>
  <s:Body>
    <tds:GetCapabilities>
      <tds:Category>All</tds:Category>
    </tds:GetCapabilities>
  </s:Body>
</s:Envelope>`

	resp, err := c.DoRequest(ctx, c.XAddr, body)
	if err != nil {
		return err
	}

	type capsResp struct {
		XMLName xml.Name `xml:"Envelope"`
		Body    struct {
			Response struct {
				Capabilities struct {
					Analytics struct{ XAddr string `xml:"XAddr"` } `xml:"Analytics"`
					Device    struct{ XAddr string `xml:"XAddr"` } `xml:"Device"`
					Events    struct{ XAddr string `xml:"XAddr"` } `xml:"Events"`
					Imaging   struct{ XAddr string `xml:"XAddr"` } `xml:"Imaging"`
					Media     struct{ XAddr string `xml:"XAddr"` } `xml:"Media"`
					PTZ       struct{ XAddr string `xml:"XAddr"` } `xml:"PTZ"`
					Extension struct {
						DeviceIO  struct{ XAddr string `xml:"XAddr"` } `xml:"DeviceIO"`
						Recording struct{ XAddr string `xml:"XAddr"` } `xml:"Recording"`
						Search    struct{ XAddr string `xml:"XAddr"` } `xml:"Search"`
						Replay    struct{ XAddr string `xml:"XAddr"` } `xml:"Replay"`
					} `xml:"Extension"`
				} `xml:"Capabilities"`
			} `xml:"GetCapabilitiesResponse"`
		} `xml:"Body"`
	}
	var parsed capsResp
	if err := xml.Unmarshal(resp, &parsed); err != nil {
		return fmt.Errorf("parse capabilities: %w", err)
	}
	caps := parsed.Body.Response.Capabilities

	// rewriteHost swaps the host portion of `advertised` with the host
	// from c.XAddr. NAT'd cameras advertise their LAN IP — unreachable
	// from anywhere outside the camera's local subnet.
	rewriteHost := func(advertised string) string {
		if advertised == "" {
			return ""
		}
		// Pull the path off the advertised URL and graft it onto our base.
		idx := strings.Index(advertised, "/onvif/")
		if idx < 0 {
			return advertised
		}
		path := advertised[idx:]
		// c.XAddr always contains "/onvif/device_service" — strip and use the host base.
		base := strings.TrimSuffix(c.XAddr, "/onvif/device_service")
		return base + path
	}

	c.mediaURL = rewriteHost(caps.Media.XAddr)
	c.ptzURL = rewriteHost(caps.PTZ.XAddr)
	c.imagingURL = rewriteHost(caps.Imaging.XAddr)
	c.eventsURL = rewriteHost(caps.Events.XAddr)
	c.recordingURL = rewriteHost(caps.Extension.Recording.XAddr)
	c.searchURL = rewriteHost(caps.Extension.Search.XAddr)
	c.replayURL = rewriteHost(caps.Extension.Replay.XAddr)
	c.deviceIOURL = rewriteHost(caps.Extension.DeviceIO.XAddr)

	log.Printf("[ONVIF] Discovered services: media=%s ptz=%s recording=%s",
		c.mediaURL, c.ptzURL, c.recordingURL)
	return nil
}

// serviceURL returns the discovered URL for `service` if we have one,
// otherwise falls back to the hard-coded path convention. The fallback
// covers cameras whose GetCapabilities didn't return that service —
// behaviour matches what the code did before discovery was added.
func (c *Client) serviceURL(service, fallbackPath string) string {
	switch service {
	case "media":
		if c.mediaURL != "" {
			return c.mediaURL
		}
	case "ptz":
		if c.ptzURL != "" {
			return c.ptzURL
		}
	case "imaging":
		if c.imagingURL != "" {
			return c.imagingURL
		}
	case "events":
		if c.eventsURL != "" {
			return c.eventsURL
		}
	case "recording":
		if c.recordingURL != "" {
			return c.recordingURL
		}
	case "search":
		if c.searchURL != "" {
			return c.searchURL
		}
	case "replay":
		if c.replayURL != "" {
			return c.replayURL
		}
	case "deviceio":
		if c.deviceIOURL != "" {
			return c.deviceIOURL
		}
	}
	return c.fallbackServiceURL(service, fallbackPath)
}

// fallbackServiceURL builds a service URL when GetCapabilities didn't
// return one. Vendors disagree on the path convention:
//
//   - "/onvif/device_service", "/onvif/media_service", "/onvif/event_service" — lower_snake
//   - "/onvif/Media", "/onvif/Events", "/onvif/Recording"                    — CamelCase
//
// Hikvision and many off-brand cameras use the lower_snake form;
// Milesight uses CamelCase. We pick the convention by inspecting any
// service URL we DID discover (media is the most reliable). If we
// discovered nothing, default to the legacy `device_service` →
// `<fallbackPath>` substitution that matches Hikvision-style layouts.
func (c *Client) fallbackServiceURL(service, fallbackPath string) string {
	// Mirror the convention from a discovered URL when we have one.
	if reference := c.firstDiscoveredURL(); reference != "" {
		if i := strings.LastIndex(reference, "/onvif/"); i >= 0 {
			base := reference[:i] + "/onvif/"
			if usesCamelCase(reference[i+len("/onvif/"):]) {
				return base + camelService(service)
			}
			return base + fallbackPath
		}
	}
	return strings.Replace(c.XAddr, "device_service", fallbackPath, 1)
}

// firstDiscoveredURL returns any non-empty service URL we recorded
// during GetCapabilities. Order matters only for picking a convention,
// so we prefer media (the camera will almost always advertise it).
func (c *Client) firstDiscoveredURL() string {
	for _, u := range []string{c.mediaURL, c.recordingURL, c.ptzURL, c.imagingURL, c.eventsURL, c.searchURL, c.replayURL, c.deviceIOURL} {
		if u != "" {
			return u
		}
	}
	return ""
}

// usesCamelCase reports whether the path tail looks like Milesight's
// "/onvif/Media" style rather than Hikvision's "/onvif/media_service".
func usesCamelCase(tail string) bool {
	// Trim any query string.
	if i := strings.IndexByte(tail, '?'); i >= 0 {
		tail = tail[:i]
	}
	// CamelCase service names start with an uppercase letter and don't
	// contain "_service" at all (e.g. "Media", "Events", "Recording").
	if tail == "" {
		return false
	}
	if strings.Contains(tail, "_service") {
		return false
	}
	first := tail[0]
	return first >= 'A' && first <= 'Z'
}

// camelService maps an internal service name to the CamelCase path tail
// used by cameras that follow Milesight's convention.
func camelService(service string) string {
	switch service {
	case "media":
		return "Media"
	case "ptz":
		return "PTZ"
	case "imaging":
		return "Imaging"
	case "events":
		return "Events"
	case "recording":
		return "Recording"
	case "search":
		return "Search"
	case "replay":
		return "Replay"
	case "deviceio":
		return "DeviceIO"
	}
	return service
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

// SystemReboot asks the camera to reboot via ONVIF tds:SystemReboot.
// Returns the human-readable confirmation message the camera echoes
// back (typically "Rebooting in N seconds"). The HTTP request itself
// usually completes before the camera actually drops its network —
// callers should expect the camera to be unreachable for ~30–90 s
// and re-discover its services on the next ONVIF Connect.
func (c *Client) SystemReboot(ctx context.Context) (string, error) {
	body := `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <s:Header>` + c.BuildSecurityHeader() + `</s:Header>
  <s:Body>
    <tds:SystemReboot/>
  </s:Body>
</s:Envelope>`

	resp, err := c.DoRequest(ctx, c.XAddr, body)
	if err != nil {
		return "", err
	}

	type rebootResp struct {
		XMLName xml.Name `xml:"Envelope"`
		Body    struct {
			Response struct {
				Message string `xml:"Message"`
			} `xml:"SystemRebootResponse"`
		} `xml:"Body"`
	}
	var parsed rebootResp
	if err := xml.Unmarshal(resp, &parsed); err != nil {
		// Some cameras return an empty SystemRebootResponse and drop
		// the connection mid-reply. Treat a parse failure as success
		// since the reboot command itself was accepted.
		return "Reboot requested", nil
	}
	msg := parsed.Body.Response.Message
	if msg == "" {
		msg = "Reboot requested"
	}
	return msg, nil
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
	mediaAddr := c.serviceURL("media", "media_service")

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

	// NAT host swap: if the camera advertises a private LAN IP for its
	// stream but we reached it on a public/routable host (typical for
	// 5G / NAT'd deployments where ONVIF traffic is port-forwarded), the
	// returned URI is unreachable from outside the camera's subnet.
	// Rewrite the RTSP host to match whichever address we used to dial
	// the device. Only kicks in when the swap actually fixes something
	// (advertised host private, our XAddr host not private), so on-LAN
	// deployments are unaffected.
	uri = c.rewriteStreamHost(uri)

	// Inject credentials into RTSP URI if needed
	if c.Username != "" && !strings.Contains(uri, "@") {
		uri = strings.Replace(uri, "rtsp://", fmt.Sprintf("rtsp://%s:%s@", c.Username, c.Password), 1)
	}

	log.Printf("[ONVIF] Stream URI for profile %s: %s", profileToken, sanitizeURI(uri))
	return uri, nil
}

// rewriteStreamHost swaps the host portion of an RTSP URI with the host
// we connected to via XAddr, but only when the advertised host is a
// private IP and ours is not. That's the NAT-traversal scenario; in any
// other case (already-routable host, both private, parse failure) we
// return the URI untouched.
func (c *Client) rewriteStreamHost(uri string) string {
	if uri == "" {
		return uri
	}
	advertisedHost, advertisedPort, ok := splitHostPort(uri)
	if !ok {
		return uri
	}
	connectHost := hostFromAddr(c.XAddr)
	if connectHost == "" || connectHost == advertisedHost {
		return uri
	}
	if !isPrivateHost(advertisedHost) || isPrivateHost(connectHost) {
		return uri
	}

	// Splice in the connect host. Preserve the advertised port — cameras
	// often run RTSP on a non-554 port that isn't our HTTP port; we
	// trust their port assertion since it survives NAT mapping.
	rebuilt := strings.Replace(uri,
		"rtsp://"+advertisedHost+":"+advertisedPort,
		"rtsp://"+connectHost+":"+advertisedPort,
		1,
	)
	if rebuilt == uri {
		// No port present in original — try the bare host form.
		rebuilt = strings.Replace(uri,
			"rtsp://"+advertisedHost,
			"rtsp://"+connectHost,
			1,
		)
	}
	log.Printf("[ONVIF] NAT rewrite: stream host %s -> %s", advertisedHost, connectHost)
	return rebuilt
}

// splitHostPort extracts the host and port from an RTSP URI, defaulting
// the port to 554 when omitted.
func splitHostPort(rtspURI string) (host, port string, ok bool) {
	s := strings.TrimPrefix(rtspURI, "rtsp://")
	// Strip credentials.
	if at := strings.LastIndex(s, "@"); at >= 0 {
		s = s[at+1:]
	}
	// Strip path.
	if slash := strings.Index(s, "/"); slash >= 0 {
		s = s[:slash]
	}
	if s == "" {
		return "", "", false
	}
	if h, p, err := net.SplitHostPort(s); err == nil {
		return h, p, true
	}
	return s, "554", true
}

// hostFromAddr returns just the host portion of a URL like
// "http://1.2.3.4/onvif/device_service".
func hostFromAddr(addr string) string {
	s := addr
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if slash := strings.Index(s, "/"); slash >= 0 {
		s = s[:slash]
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		return h
	}
	return s
}

// isPrivateHost reports whether a hostname or IP literal is in an
// RFC 1918 / link-local / loopback range. Hostnames that don't parse
// as IPs are treated as routable (we don't try to resolve them).
func isPrivateHost(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
}

// GetProfiles retrieves available media profiles
func (c *Client) GetProfiles(ctx context.Context) ([]StreamProfile, error) {
	mediaAddr := c.serviceURL("media", "media_service")

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
	ptzAddr := c.serviceURL("ptz", "ptz")

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
	searchAddr := c.serviceURL("search", "Search")
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
	recAddr := c.serviceURL("recording", "Recording")
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
	replayAddr := c.serviceURL("replay", "Replay")
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
	ptzAddr := c.serviceURL("ptz", "ptz")

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
		// Server-side log gets the full body for debugging; the
		// caller-visible error is truncated to 500 chars.
		if resp.StatusCode >= 500 {
			log.Printf("[ONVIF] HTTP %d from %s; full body: %s", resp.StatusCode, sanitizeURI(url), string(data))
		}
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
