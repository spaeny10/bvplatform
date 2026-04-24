package onvif

import (
	"context"
	"encoding/xml"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	wsDiscoveryAddr  = "239.255.255.250:3702"
	wsDiscoveryProbe = `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing"
            xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery"
            xmlns:dn="http://www.onvif.org/ver10/network/wsdl">
  <s:Header>
    <a:Action s:mustUnderstand="1">http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</a:Action>
    <a:MessageID>uuid:__MESSAGE_ID__</a:MessageID>
    <a:ReplyTo>
      <a:Address>http://schemas.xmlsoap.org/ws/2004/08/addressing/role/anonymous</a:Address>
    </a:ReplyTo>
    <a:To s:mustUnderstand="1">urn:schemas-xmlsoap-org:ws:2005:04:discovery</a:To>
  </s:Header>
  <s:Body>
    <d:Probe>
      <d:Types>dn:NetworkVideoTransmitter</d:Types>
    </d:Probe>
  </s:Body>
</s:Envelope>`
)

// DiscoveredDevice represents a camera found via WS-Discovery
type DiscoveredDevice struct {
	Address      string `json:"address"`
	Name         string `json:"name"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	XAddr        string `json:"xaddr"`
}

// probeMatchEnvelope is used to parse WS-Discovery responses
type probeMatchEnvelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    struct {
		ProbeMatches struct {
			ProbeMatch []struct {
				XAddrs string `xml:"XAddrs"`
				Scopes string `xml:"Scopes"`
			} `xml:"ProbeMatch"`
		} `xml:"ProbeMatches"`
	} `xml:"Body"`
}

// Discover sends a WS-Discovery probe and returns found ONVIF devices
func Discover(ctx context.Context, timeout time.Duration) ([]DiscoveredDevice, error) {
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	log.Println("[ONVIF] Starting WS-Discovery probe...")

	// Create a timeout context so everything gets cleaned up
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Resolve multicast address
	addr, err := net.ResolveUDPAddr("udp4", wsDiscoveryAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve discovery address: %w", err)
	}

	// Build probe message
	messageID := fmt.Sprintf("urn:uuid:%d", time.Now().UnixNano())
	probe := strings.Replace(wsDiscoveryProbe, "__MESSAGE_ID__", messageID, 1)

	seen := make(map[string]bool)
	var devices []DiscoveredDevice
	var mu sync.Mutex

	// Get all network interfaces and send probes on each one
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Printf("[ONVIF] Could not enumerate interfaces: %v", err)
	}

	var conns []*net.UDPConn
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok || ipNet.IP.To4() == nil {
				continue
			}
			localAddr := &net.UDPAddr{IP: ipNet.IP, Port: 0}
			conn, err := net.ListenUDP("udp4", localAddr)
			if err != nil {
				log.Printf("[ONVIF] Could not bind to %s (%s): %v", ipNet.IP, iface.Name, err)
				continue
			}
			conns = append(conns, conn)
			log.Printf("[ONVIF] Sending probe on interface %s (%s)", iface.Name, ipNet.IP)

			if _, err := conn.WriteToUDP([]byte(probe), addr); err != nil {
				log.Printf("[ONVIF] Failed to send probe on %s: %v", ipNet.IP, err)
			}
		}
	}

	// If no interface-specific connections worked, fall back to default
	if len(conns) == 0 {
		log.Println("[ONVIF] No interface-specific binds, using default")
		conn, err := net.ListenUDP("udp4", nil)
		if err != nil {
			return nil, fmt.Errorf("listen UDP: %w", err)
		}
		conns = append(conns, conn)
		if _, err := conn.WriteToUDP([]byte(probe), addr); err != nil {
			return nil, fmt.Errorf("send probe: %w", err)
		}
	}

	// Close all connections when context expires — this unblocks ReadFromUDP on Windows
	go func() {
		<-ctx.Done()
		for _, c := range conns {
			c.Close()
		}
	}()

	// Collect responses from all connections in parallel
	var wg sync.WaitGroup
	deadline := time.Now().Add(timeout)

	for _, conn := range conns {
		wg.Add(1)
		go func(c *net.UDPConn) {
			defer wg.Done()

			c.SetReadDeadline(deadline)
			buf := make([]byte, 65535)

			for {
				if ctx.Err() != nil {
					return
				}

				n, remoteAddr, err := c.ReadFromUDP(buf)
				if err != nil {
					// Any error (timeout, closed, ICMP) terminates this goroutine
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						return // timeout is expected
					}
					// Connection was closed by context cancellation
					if ctx.Err() != nil {
						return
					}
					log.Printf("[ONVIF] Read error: %v", err)
					return // Don't loop forever on persistent errors
				}

				// Parse response
				var envelope probeMatchEnvelope
				if err := xml.Unmarshal(buf[:n], &envelope); err != nil {
					log.Printf("[ONVIF] Failed to parse response from %s: %v", remoteAddr, err)
					continue
				}

				for _, match := range envelope.Body.ProbeMatches.ProbeMatch {
					xaddrs := strings.Fields(match.XAddrs)
					for _, xaddr := range xaddrs {
						mu.Lock()
						if seen[xaddr] {
							mu.Unlock()
							continue
						}
						seen[xaddr] = true
						mu.Unlock()

						device := DiscoveredDevice{
							Address: remoteAddr.IP.String(),
							XAddr:   xaddr,
						}

						// Parse scopes for name, manufacturer, model
						scopes := strings.Fields(match.Scopes)
						for _, scope := range scopes {
							if strings.Contains(scope, "onvif://www.onvif.org/name/") {
								device.Name = extractScopeValue(scope, "name/")
							}
							if strings.Contains(scope, "onvif://www.onvif.org/manufacturer/") {
								device.Manufacturer = extractScopeValue(scope, "manufacturer/")
							}
							if strings.Contains(scope, "onvif://www.onvif.org/model/") || strings.Contains(scope, "onvif://www.onvif.org/hardware/") {
								device.Model = extractScopeValue(scope, "model/")
								if device.Model == "" {
									device.Model = extractScopeValue(scope, "hardware/")
								}
							}
						}

						if device.Name == "" {
							device.Name = device.Address
						}

						mu.Lock()
						devices = append(devices, device)
						mu.Unlock()
						log.Printf("[ONVIF] Found: %s (%s) at %s", device.Name, device.Model, device.XAddr)
					}
				}
			}
		}(conn)
	}

	wg.Wait()
	log.Printf("[ONVIF] Discovery complete: found %d device(s)", len(devices))
	return devices, nil
}

func extractScopeValue(scope, key string) string {
	idx := strings.Index(scope, key)
	if idx < 0 {
		return ""
	}
	value := scope[idx+len(key):]
	value = strings.ReplaceAll(value, "%20", " ")
	return strings.TrimSpace(value)
}
