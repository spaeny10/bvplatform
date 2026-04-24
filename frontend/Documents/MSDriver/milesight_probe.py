#!/usr/bin/env python3
"""
Milesight IP camera probe v7 — RTSP Digest over a single TCP connection.

v7 fix: LIVE555 on this firmware binds Digest nonces to the TCP
connection. v6 opened a fresh socket for the authed retry, so the
original nonce was already discarded when the server saw the retry.
v7 sends both the no-auth probe and the Digest-authed retry on the
SAME socket. Also drops `algorithm=MD5` from the Authorization header
since LIVE555's challenge doesn't include an algorithm param.

Previously (kept):
  * Credential redaction in every Report write (.txt + .json).
  * RTSP request line URL strips userinfo; creds only in Auth header.

Still present from v5:
  * Dual auth: WS-Security + HTTP Digest on every authenticated ONVIF
    call (required for operator/admin tier operations on this firmware).
Still present from v4:
  * NAT-aware URL rewriting — every XAddr / subscription URL / RTSP URI
    the camera returns (with internal APN IP) gets rewritten to the
    public host/port we reached ONVIF on.

What it does:
  1. GetSystemDateAndTime (no auth) — discovers ONVIF endpoint + camera clock.
  2. GetDeviceInformation (WS-Security auth) — confirms credentials work.
  3. GetServices — discovers Media, Events, Analytics endpoint URLs, then
     REWRITES them to the public host.
  4. GetProfiles, GetStreamUri — RTSP URLs (RTSP host is rewritten too).
  5. GetEventProperties — topic tree (every event this firmware can emit).
  6. CreatePullPointSubscription + PullMessages loop for N seconds —
     captures LIVE events with real coordinate payloads.
  7. GetRules + GetAnalyticsModules per video analytics configuration —
     persisted ROI / tripwire / region geometry.
  8. RTSP DESCRIBE on each stream URL — dumps SDP (video/audio/metadata tracks).

Install:
    pip install --user requests

Run:
    python milesight_probe_v7.py --ip 162.191.235.243 --user admin --pw 'YOURPW' --seconds 60

Trigger VCA events (walk through frame, cross tripwire) during the
"PullPoint live capture" phase.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
import re
import secrets
import socket
import sys
import time
from datetime import datetime, timezone
from urllib.parse import urlparse
from xml.etree import ElementTree as ET

import requests
from requests.auth import HTTPBasicAuth, HTTPDigestAuth


# ---------------- SOAP scaffolding ----------------

NS = {
    "s": "http://www.w3.org/2003/05/soap-envelope",
    "s11": "http://schemas.xmlsoap.org/soap/envelope/",
    "tds": "http://www.onvif.org/ver10/device/wsdl",
    "trt": "http://www.onvif.org/ver10/media/wsdl",
    "tev": "http://www.onvif.org/ver10/events/wsdl",
    "tan": "http://www.onvif.org/ver20/analytics/wsdl",
    "tt": "http://www.onvif.org/ver10/schema",
    "wsa": "http://www.w3.org/2005/08/addressing",
    "wsnt": "http://docs.oasis-open.org/wsn/b-2",
    "wsse": "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd",
    "wsu": "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd",
}

ENVELOPE_TEMPLATE = """<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope
    xmlns:s="http://www.w3.org/2003/05/soap-envelope"
    xmlns:tds="http://www.onvif.org/ver10/device/wsdl"
    xmlns:trt="http://www.onvif.org/ver10/media/wsdl"
    xmlns:tev="http://www.onvif.org/ver10/events/wsdl"
    xmlns:tan="http://www.onvif.org/ver20/analytics/wsdl"
    xmlns:tt="http://www.onvif.org/ver10/schema"
    xmlns:wsa="http://www.w3.org/2005/08/addressing"
    xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2"
    xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"
    xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">
  <s:Header>{header}</s:Header>
  <s:Body>{body}</s:Body>
</s:Envelope>"""


def ws_security_header(user: str, pw: str, clock_skew_seconds: float = 0.0) -> str:
    """WS-Security UsernameToken with PasswordDigest.

    clock_skew_seconds lets us align the 'Created' timestamp to the camera's
    clock (we fetch that via GetSystemDateAndTime first).
    """
    nonce = secrets.token_bytes(16)
    now = datetime.now(timezone.utc).timestamp() + clock_skew_seconds
    created = datetime.fromtimestamp(now, tz=timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%fZ")
    digest_bytes = hashlib.sha1(nonce + created.encode("utf-8") + pw.encode("utf-8")).digest()
    digest_b64 = base64.b64encode(digest_bytes).decode("ascii")
    nonce_b64 = base64.b64encode(nonce).decode("ascii")
    return (
        '<wsse:Security s:mustUnderstand="1">'
        "<wsse:UsernameToken>"
        f"<wsse:Username>{user}</wsse:Username>"
        '<wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">'
        f"{digest_b64}</wsse:Password>"
        '<wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">'
        f"{nonce_b64}</wsse:Nonce>"
        f"<wsu:Created>{created}</wsu:Created>"
        "</wsse:UsernameToken>"
        "</wsse:Security>"
    )


def build_envelope(body: str, user: str | None = None, pw: str | None = None,
                    clock_skew: float = 0.0, extra_header: str = "") -> str:
    header = extra_header
    if user and pw:
        header += ws_security_header(user, pw, clock_skew)
    return ENVELOPE_TEMPLATE.format(header=header, body=body)


def soap_post(url: str, envelope: str, timeout: float = 15.0,
               http_auth: object | None = None) -> requests.Response:
    """POST a SOAP envelope. If http_auth is supplied (HTTPDigestAuth/Basic),
    it's attached at the HTTP transport layer in addition to whatever WS-Security
    is inside the envelope. Milesight requires BOTH for operator/admin calls."""
    return requests.post(
        url,
        data=envelope.encode("utf-8"),
        headers={"Content-Type": 'application/soap+xml; charset=utf-8'},
        timeout=timeout,
        auth=http_auth,
    )


def pretty_xml(text: str) -> str:
    try:
        from xml.dom.minidom import parseString
        return parseString(text).toprettyxml(indent="  ")
    except Exception:
        return text


def find_text(root: ET.Element, path: str) -> str | None:
    el = root.find(path, NS)
    return el.text if el is not None else None


def rewrite_host(url: str, public_host: str, public_port: int) -> str:
    """
    Replace the host:port of `url` with (public_host, public_port).
    Keeps the scheme (http/rtsp/etc), path, query, and any userinfo.

    Cameras behind NAT always return their own interior IP in XAddr /
    subscription references / stream URIs. When we're reaching the camera
    via its public IP, those interior URLs are unreachable and we have to
    rewrite the authority portion while keeping path+query intact.
    """
    if not url:
        return url
    try:
        p = urlparse(url)
    except Exception:
        return url
    if not p.hostname:
        return url
    userinfo = ""
    if p.username:
        userinfo = p.username
        if p.password:
            userinfo += f":{p.password}"
        userinfo += "@"
    # For RTSP default port is 554, for HTTP 80 — don't force an explicit port
    # onto the URL unless the original had one, to keep output readable.
    has_explicit_port = p.port is not None
    new_port = public_port
    default_ports = {"http": 80, "https": 443, "rtsp": 554}
    if not has_explicit_port and default_ports.get(p.scheme) == new_port:
        netloc = f"{userinfo}{public_host}"
    else:
        netloc = f"{userinfo}{public_host}:{new_port}"
    rebuilt = f"{p.scheme}://{netloc}{p.path}"
    if p.query:
        rebuilt += f"?{p.query}"
    if p.fragment:
        rebuilt += f"#{p.fragment}"
    return rebuilt


# ---------------- Report ----------------

class Report:
    """Report writer with credential redaction.

    Every string written to disk (including JSON values) gets the password
    and any password-derived artifacts scrubbed. Set via .set_secret(pw).
    """

    def __init__(self, path_txt: str, path_json: str):
        self.fh = open(path_txt, "w", encoding="utf-8")
        self.json_path = path_json
        self.data: dict = {}
        self._secrets: list[str] = []

    def set_secret(self, secret: str):
        if secret and secret not in self._secrets:
            self._secrets.append(secret)
            # Also redact common URL-embedded variants (user:pass@host)
            # by explicitly registering the raw password — urlparse-roundtripped
            # URLs will still contain the same literal string.

    def _redact(self, text: str) -> str:
        if not isinstance(text, str) or not self._secrets:
            return text
        out = text
        for s in self._secrets:
            if s and s in out:
                out = out.replace(s, "***REDACTED***")
        return out

    def _redact_obj(self, obj):
        if isinstance(obj, str):
            return self._redact(obj)
        if isinstance(obj, dict):
            return {k: self._redact_obj(v) for k, v in obj.items()}
        if isinstance(obj, list):
            return [self._redact_obj(x) for x in obj]
        return obj

    def section(self, title: str):
        title = self._redact(title)
        self.fh.write(f"\n\n{'=' * 72}\n{title}\n{'=' * 72}\n")
        self.fh.flush()
        print(f">>> {title}")

    def write(self, text: str):
        text = self._redact(text)
        self.fh.write(text if text.endswith("\n") else text + "\n")
        self.fh.flush()

    def store(self, key: str, value):
        self.data[key] = self._redact_obj(value)

    def close(self):
        self.fh.close()
        with open(self.json_path, "w", encoding="utf-8") as jf:
            json.dump(self.data, jf, indent=2, default=str, ensure_ascii=False)


def dump_response(report: Report, label: str, r: requests.Response):
    report.write(
        f"{label} -> HTTP {r.status_code} ({r.headers.get('Content-Type')}, {len(r.content)}B)\n"
        f"headers: {dict(r.headers)}\n"
        f"body:\n{pretty_xml(r.text) if 'xml' in (r.headers.get('Content-Type') or '').lower() else r.text[:2500]}"
    )


# ---------------- ONVIF discovery ----------------

DEVICE_PATHS = [
    "/onvif/device_service",
    "/onvif/Device",
    "/onvif/services",
]


def discover_device_endpoint(report: Report, ip: str, ports: list[int]) -> tuple[str, datetime] | None:
    """
    Find which (port, path) speaks ONVIF, and learn the camera's clock along
    the way. GetSystemDateAndTime requires no auth, so it's our probe.
    """
    report.section("ONVIF: discovering device endpoint + camera clock")
    body = "<tds:GetSystemDateAndTime/>"
    envelope = build_envelope(body)  # no auth
    for port in ports:
        for path in DEVICE_PATHS:
            url = f"http://{ip}:{port}{path}"
            try:
                r = soap_post(url, envelope, timeout=8)
                report.write(
                    f"POST {url} -> HTTP {r.status_code} ({r.headers.get('Content-Type')})"
                )
                if r.status_code != 200 or "xml" not in (r.headers.get("Content-Type") or "").lower():
                    continue
                try:
                    root = ET.fromstring(r.text)
                except ET.ParseError as e:
                    report.write(f"  xml parse failed: {e}")
                    continue
                # Look for UTCDateTime
                utc_year = find_text(root, ".//tt:UTCDateTime/tt:Date/tt:Year")
                if not utc_year:
                    # Some cameras return only LocalDateTime
                    utc_year = find_text(root, ".//tt:LocalDateTime/tt:Date/tt:Year")
                if utc_year:
                    month = find_text(root, ".//tt:UTCDateTime/tt:Date/tt:Month") or "1"
                    day = find_text(root, ".//tt:UTCDateTime/tt:Date/tt:Day") or "1"
                    hour = find_text(root, ".//tt:UTCDateTime/tt:Time/tt:Hour") or "0"
                    minute = find_text(root, ".//tt:UTCDateTime/tt:Time/tt:Minute") or "0"
                    second = find_text(root, ".//tt:UTCDateTime/tt:Time/tt:Second") or "0"
                    cam_time = datetime(int(utc_year), int(month), int(day),
                                         int(hour), int(minute), int(second),
                                         tzinfo=timezone.utc)
                    report.write(f"  ONVIF OK at {url}  camera UTC = {cam_time.isoformat()}")
                    report.store("device_endpoint", url)
                    report.store("camera_time", cam_time.isoformat())
                    report.write(f"  raw SOAP response:\n{pretty_xml(r.text)}")
                    return url, cam_time
                else:
                    report.write(f"  SOAP response had no UTCDateTime; body:\n{pretty_xml(r.text)[:2000]}")
            except Exception as e:
                report.write(f"POST {url} failed: {e}")
    report.write("No ONVIF endpoint responded. Check that ONVIF is enabled in the camera UI.")
    return None


# ---------------- Authenticated ONVIF calls ----------------

SIMPLE_DEVICE_CALLS = [
    ("GetDeviceInformation",    "<tds:GetDeviceInformation/>"),
    ("GetCapabilities",         "<tds:GetCapabilities><tds:Category>All</tds:Category></tds:GetCapabilities>"),
    ("GetServices",             "<tds:GetServices><tds:IncludeCapability>true</tds:IncludeCapability></tds:GetServices>"),
    ("GetHostname",             "<tds:GetHostname/>"),
    ("GetNetworkInterfaces",    "<tds:GetNetworkInterfaces/>"),
    ("GetUsers",                "<tds:GetUsers/>"),
    ("GetScopes",               "<tds:GetScopes/>"),
]


def _is_not_authorized(status: int, body: str) -> bool:
    """Detect the specific ter:NotAuthorized SOAP Fault Milesight returns when
    our auth is accepted but not privileged enough for the operation."""
    if status == 200 or not body:
        return False
    return "NotAuthorized" in body


def call_authenticated(report: Report, url: str, user: str, pw: str, skew: float,
                        label: str, body: str) -> tuple[int, str]:
    """
    Milesight firmware needs WS-Security for authentication and ALSO accepts
    HTTP Digest. For some operations it'll auth us at a low privilege with
    WS-Security alone; adding HTTP Digest seems to kick it into the right
    privilege tier. We always send both.

    If the camera still returns NotAuthorized, we retry once with HTTP Digest
    only (no WS-Security body header) as a diagnostic — that tells us whether
    WS-Security or the combination is what's causing the fault.
    """
    env = build_envelope(body, user, pw, skew)
    digest_auth = HTTPDigestAuth(user, pw)

    # Attempt A: WS-Security (in SOAP) + HTTP Digest (on transport)
    try:
        r = soap_post(url, env, http_auth=digest_auth)
    except Exception as e:
        report.write(f"{label} [ws+digest] failed: {e}")
        return 0, ""
    dump_response(report, f"{label} [ws+digest]", r)
    if not _is_not_authorized(r.status_code, r.text):
        return r.status_code, r.text

    # Attempt B: HTTP Digest only (no WS-Security) — diagnostic retry
    env_plain = build_envelope(body)  # no user/pw, no wsse header
    try:
        r2 = soap_post(url, env_plain, http_auth=digest_auth)
    except Exception as e:
        report.write(f"{label} [digest only] failed: {e}")
        return r.status_code, r.text
    dump_response(report, f"{label} [digest only]", r2)
    if not _is_not_authorized(r2.status_code, r2.text) and r2.status_code == 200:
        return r2.status_code, r2.text

    # Neither worked — return the first attempt's result so caller sees the fault.
    return r.status_code, r.text


def parse_services(xml_text: str) -> dict[str, str]:
    """From GetServices response, extract endpoint URLs keyed by service namespace."""
    out = {}
    try:
        root = ET.fromstring(xml_text)
    except ET.ParseError:
        return out
    for svc in root.findall(".//tds:Service", NS):
        ns = svc.find("tds:Namespace", NS)
        xaddr = svc.find("tds:XAddr", NS)
        if ns is not None and xaddr is not None and ns.text and xaddr.text:
            out[ns.text] = xaddr.text
    return out


def probe_device(report: Report, url: str, user: str, pw: str, skew: float,
                  public_host: str, public_port: int) -> dict[str, str]:
    report.section("ONVIF: authenticated device calls")
    services: dict[str, str] = {}
    for label, body in SIMPLE_DEVICE_CALLS:
        status, text = call_authenticated(report, url, user, pw, skew, label, body)
        if label == "GetServices" and status == 200 and text:
            raw = parse_services(text)
            services = {ns: rewrite_host(xaddr, public_host, public_port) for ns, xaddr in raw.items()}
            report.store("service_endpoints_raw", raw)
            report.store("service_endpoints", services)
            report.write(
                f"\nService endpoints as reported by camera (internal IP):\n{json.dumps(raw, indent=2)}"
                f"\n\nService endpoints rewritten to public host {public_host}:{public_port}:\n"
                f"{json.dumps(services, indent=2)}"
            )
    return services


# ---------------- Media (profiles, stream URIs) ----------------

def probe_media(report: Report, media_url: str, user: str, pw: str, skew: float,
                 public_host: str, rtsp_public_port: int) -> list[dict]:
    report.section(f"ONVIF Media @ {media_url}")
    profiles_info = []

    # GetProfiles
    status, text = call_authenticated(
        report, media_url, user, pw, skew, "GetProfiles", "<trt:GetProfiles/>"
    )
    if status != 200 or not text:
        return profiles_info

    try:
        root = ET.fromstring(text)
    except ET.ParseError:
        return profiles_info

    profiles = root.findall(".//trt:Profiles", NS)
    if not profiles:
        profiles = root.findall(".//{http://www.onvif.org/ver10/schema}Profiles")

    for p in profiles:
        token = p.attrib.get("token") or p.attrib.get("{http://www.onvif.org/ver10/schema}token")
        name_el = p.find("tt:Name", NS)
        name = name_el.text if name_el is not None else "?"
        entry = {"token": token, "name": name}

        # GetStreamUri
        body = (
            "<trt:GetStreamUri>"
            "<trt:StreamSetup>"
            "<tt:Stream>RTP-Unicast</tt:Stream>"
            "<tt:Transport><tt:Protocol>RTSP</tt:Protocol></tt:Transport>"
            "</trt:StreamSetup>"
            f"<trt:ProfileToken>{token}</trt:ProfileToken>"
            "</trt:GetStreamUri>"
        )
        st, txt = call_authenticated(
            report, media_url, user, pw, skew, f"GetStreamUri({name}/{token})", body
        )
        if st == 200 and txt:
            try:
                rroot = ET.fromstring(txt)
                uri_el = rroot.find(".//tt:Uri", NS)
                if uri_el is not None and uri_el.text:
                    entry["rtsp_uri_raw"] = uri_el.text
                    entry["rtsp_uri"] = rewrite_host(uri_el.text, public_host, rtsp_public_port)
            except ET.ParseError:
                pass

        # Video analytics configuration token (for Analytics calls later)
        vac = p.find("tt:VideoAnalyticsConfiguration", NS)
        if vac is not None:
            entry["vac_token"] = vac.attrib.get("token")
        profiles_info.append(entry)

    report.store("profiles", profiles_info)
    return profiles_info


# ---------------- Analytics (rules + modules) ----------------

def probe_analytics(report: Report, analytics_url: str, user: str, pw: str, skew: float,
                     vac_tokens: list[str]):
    if not analytics_url or not vac_tokens:
        return
    report.section(f"ONVIF Analytics @ {analytics_url}")
    results = []
    for tok in set(vac_tokens):
        rules_body = f"<tan:GetRules><tan:ConfigurationToken>{tok}</tan:ConfigurationToken></tan:GetRules>"
        call_authenticated(report, analytics_url, user, pw, skew,
                            f"GetRules({tok})", rules_body)
        mods_body = f"<tan:GetAnalyticsModules><tan:ConfigurationToken>{tok}</tan:ConfigurationToken></tan:GetAnalyticsModules>"
        call_authenticated(report, analytics_url, user, pw, skew,
                            f"GetAnalyticsModules({tok})", mods_body)
        results.append(tok)
    report.store("analytics_probed_configs", results)


# ---------------- Events: GetEventProperties + PullPoint ----------------

def probe_events(report: Report, events_url: str, user: str, pw: str, skew: float,
                  seconds: int, public_host: str, public_port: int):
    if not events_url:
        return
    report.section(f"ONVIF Events @ {events_url}")

    # GetEventProperties — tells us which topics this firmware publishes
    call_authenticated(report, events_url, user, pw, skew,
                        "GetEventProperties", "<tev:GetEventProperties/>")

    # CreatePullPointSubscription
    report.section(f"ONVIF PullPoint: subscribe + capture for {seconds}s")
    sub_body = (
        "<tev:CreatePullPointSubscription>"
        f"<tev:InitialTerminationTime>PT{seconds + 60}S</tev:InitialTerminationTime>"
        "</tev:CreatePullPointSubscription>"
    )
    status, text = call_authenticated(report, events_url, user, pw, skew,
                                        "CreatePullPointSubscription", sub_body)
    if status != 200 or not text:
        report.write("Could not create PullPoint subscription.")
        return

    # Parse subscription reference address
    try:
        root = ET.fromstring(text)
    except ET.ParseError as e:
        report.write(f"CreatePullPointSubscription XML parse failed: {e}")
        return
    addr = root.find(".//wsa:Address", NS)
    if addr is None or not addr.text:
        report.write("No subscription address in response; cannot PullMessages.")
        return
    sub_url_raw = addr.text
    sub_url = rewrite_host(sub_url_raw, public_host, public_port)
    report.write(f"Subscription URL (as-reported): {sub_url_raw}")
    report.write(f"Subscription URL (rewritten to public): {sub_url}")

    # Pull messages in a loop for `seconds`
    end = time.time() + seconds
    all_events = []
    while time.time() < end:
        pull_body = (
            "<tev:PullMessages>"
            "<tev:Timeout>PT5S</tev:Timeout>"
            "<tev:MessageLimit>100</tev:MessageLimit>"
            "</tev:PullMessages>"
        )
        env = build_envelope(
            pull_body, user, pw, skew,
            extra_header=f'<wsa:To s:mustUnderstand="1">{sub_url}</wsa:To>',
        )
        try:
            r = soap_post(sub_url, env, timeout=10, http_auth=HTTPDigestAuth(user, pw))
        except Exception as e:
            report.write(f"PullMessages error: {e}")
            break
        if r.status_code != 200:
            report.write(f"PullMessages -> HTTP {r.status_code}\n{r.text[:2000]}")
            break
        try:
            rroot = ET.fromstring(r.text)
        except ET.ParseError as e:
            report.write(f"PullMessages XML parse failed: {e}")
            break
        msgs = rroot.findall(".//wsnt:NotificationMessage", NS)
        for m in msgs:
            all_events.append(ET.tostring(m, encoding="unicode"))
            report.write(f"\n--- event #{len(all_events)} ---\n{pretty_xml(ET.tostring(m, encoding='unicode'))}")

    report.write(f"\nCaptured {len(all_events)} events in {seconds}s")
    report.store("live_events_xml", all_events)


# ---------------- RTSP DESCRIBE (proper Digest auth) ----------------

_DIGEST_KV = re.compile(r'(\w+)\s*=\s*(?:"([^"]*)"|([^,\s]+))')


def _parse_digest_challenge(www_auth: str) -> dict:
    """Parse a 'Digest realm="...", nonce="...", ...' challenge into a dict."""
    out = {}
    # Strip leading scheme word
    if www_auth.lower().startswith("digest"):
        www_auth = www_auth[6:].lstrip()
    for m in _DIGEST_KV.finditer(www_auth):
        key = m.group(1).lower()
        val = m.group(2) if m.group(2) is not None else m.group(3)
        out[key] = val
    return out


def _digest_response(user: str, pw: str, realm: str, nonce: str,
                      method: str, uri: str, algorithm: str = "MD5") -> str:
    """RFC 2617 Digest response: MD5(HA1:nonce:HA2) with qop=none (common on LIVE555)."""
    ha1 = hashlib.md5(f"{user}:{realm}:{pw}".encode()).hexdigest()
    ha2 = hashlib.md5(f"{method}:{uri}".encode()).hexdigest()
    return hashlib.md5(f"{ha1}:{nonce}:{ha2}".encode()).hexdigest()


def _read_rtsp_response(sock: socket.socket, timeout: float) -> tuple[int, dict, str]:
    """Read one RTSP response from sock. Honors Content-Length if present."""
    sock.settimeout(timeout)
    buf = b""
    # 1. Read headers
    while b"\r\n\r\n" not in buf:
        chunk = sock.recv(4096)
        if not chunk:
            break
        buf += chunk
    head, _, rest = buf.partition(b"\r\n\r\n")
    head_text = head.decode("utf-8", errors="replace")
    status_line = head_text.splitlines()[0] if head_text else ""
    m = re.match(r"RTSP/1\.0 (\d+)", status_line)
    status = int(m.group(1)) if m else 0
    headers: dict = {}
    for line in head_text.splitlines()[1:]:
        if ":" in line:
            k, v = line.split(":", 1)
            headers[k.strip().lower()] = v.strip()
    # 2. Read body up to Content-Length
    clen = int(headers.get("content-length", 0) or 0)
    body = rest
    while len(body) < clen:
        try:
            chunk = sock.recv(4096)
        except socket.timeout:
            break
        if not chunk:
            break
        body += chunk
    return status, headers, body.decode("utf-8", errors="replace")


def rtsp_describe(report: Report, uri_no_creds: str, user: str, pw: str,
                   timeout: float = 8.0):
    """RTSP DESCRIBE with Digest auth, sent over a SINGLE persistent TCP
    connection so that LIVE555's connection-scoped nonces stay valid."""
    parsed = urlparse(uri_no_creds)
    if parsed.username or parsed.password:
        parsed = parsed._replace(netloc=(parsed.hostname or "") + (f":{parsed.port}" if parsed.port else ""))
        uri_no_creds = parsed.geturl()
    host = parsed.hostname
    port = parsed.port or 554

    def build_request(cseq: int, auth_header: str | None = None) -> bytes:
        lines = [
            f"DESCRIBE {uri_no_creds} RTSP/1.0",
            f"CSeq: {cseq}",
            "Accept: application/sdp",
            "User-Agent: milesight-probe-v7",
        ]
        if auth_header:
            lines.append(f"Authorization: {auth_header}")
        return ("\r\n".join(lines) + "\r\n\r\n").encode()

    try:
        with socket.create_connection((host, port), timeout=timeout) as s:
            # Attempt 1: no auth — provokes the Digest challenge
            s.sendall(build_request(1))
            status, headers, body = _read_rtsp_response(s, timeout)
            www = (headers.get("www-authenticate", "") or "")
            report.write(
                f"RTSP {uri_no_creds} (no-auth) -> {status}  WWW-Authenticate: {www!r}"
            )

            if status == 401:
                if www.lower().startswith("digest"):
                    ch = _parse_digest_challenge(www)
                    realm = ch.get("realm", "")
                    nonce = ch.get("nonce", "")
                    resp = _digest_response(user, pw, realm, nonce,
                                              "DESCRIBE", uri_no_creds)
                    # Note: no algorithm param — LIVE555's challenge didn't
                    # include one and some pedantic servers reject extras.
                    auth = (
                        f'Digest username="{user}", realm="{realm}", '
                        f'nonce="{nonce}", uri="{uri_no_creds}", response="{resp}"'
                    )
                elif www.lower().startswith("basic"):
                    token = base64.b64encode(f"{user}:{pw}".encode()).decode()
                    auth = f"Basic {token}"
                else:
                    report.write(f"RTSP {uri_no_creds}: unknown auth scheme: {www!r}")
                    return

                # Attempt 2: authed, reusing the same socket
                s.sendall(build_request(2, auth))
                status, headers, body = _read_rtsp_response(s, timeout)

            # Redact any Authorization echo in headers before logging
            safe_headers = {k: v for k, v in headers.items()
                             if k not in ("authorization",)}
            report.write(
                f"RTSP {uri_no_creds} -> {status}\nheaders: {safe_headers}\n\nSDP:\n{body}"
            )
    except Exception as e:
        report.write(f"RTSP DESCRIBE {uri_no_creds} failed: {e}")


def probe_rtsp(report: Report, profiles: list[dict], user: str, pw: str):
    any_uri = [p for p in profiles if p.get("rtsp_uri")]
    if not any_uri:
        return
    report.section("RTSP: DESCRIBE per stream (SDP = what's on the wire)")
    for p in any_uri:
        uri = p["rtsp_uri"]
        # Strip any userinfo — we NEVER put the password in the URL.
        parsed = urlparse(uri)
        netloc = parsed.hostname or ""
        if parsed.port:
            netloc += f":{parsed.port}"
        clean_uri = f"{parsed.scheme}://{netloc}{parsed.path}"
        if parsed.query:
            clean_uri += "?" + parsed.query
        rtsp_describe(report, clean_uri, user, pw)


# ---------------- main ----------------

def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--ip", default="162.191.235.243")
    ap.add_argument("--ports", default="80,8000,8080", help="comma-separated ports to try for ONVIF")
    ap.add_argument("--rtsp-port", type=int, default=554,
                    help="public RTSP port (internal-APN IP URLs will be rewritten to this)")
    ap.add_argument("--user", required=True)
    ap.add_argument("--pw", required=True)
    ap.add_argument("--seconds", type=int, default=60)
    ap.add_argument("--out-dir", default=".")
    args = ap.parse_args()

    os.makedirs(args.out_dir, exist_ok=True)
    tag = args.ip.replace(".", "_")
    txt = os.path.join(args.out_dir, f"milesight_probe_v7_{tag}.txt")
    js = os.path.join(args.out_dir, f"milesight_probe_v7_{tag}.json")

    report = Report(txt, js)
    # IMPORTANT: register the password for redaction BEFORE any write that
    # might include it. All subsequent Report.write/store/section calls scrub it.
    report.set_secret(args.pw)
    report.write(f"Milesight probe v7 (single-socket RTSP Digest) run at {time.ctime()} targeting {args.ip}")
    report.write(f"Python: {sys.version}")

    ports = [int(x.strip()) for x in args.ports.split(",") if x.strip()]

    try:
        found = discover_device_endpoint(report, args.ip, ports)
        if not found:
            report.write(
                "\n\nNo ONVIF endpoint found. Check camera UI:\n"
                "  Settings -> System -> Security -> ONVIF  (enable it)\n"
                "  Add an ONVIF user with admin role if required.\n"
                "Try again with the ONVIF user's credentials."
            )
            return
        device_url, cam_time = found
        skew = (cam_time - datetime.now(timezone.utc)).total_seconds()
        report.write(f"\nClock skew (camera - local) = {skew:.0f}s")

        # Extract the public (host, port) we actually reached ONVIF on.
        parsed = urlparse(device_url)
        public_host = parsed.hostname or args.ip
        public_port = parsed.port or 80
        report.write(f"Public ONVIF endpoint: {public_host}:{public_port} (all internal XAddrs will be rewritten to this)")

        services = probe_device(report, device_url, args.user, args.pw, skew,
                                  public_host, public_port)

        media_url = services.get("http://www.onvif.org/ver10/media/wsdl") or device_url
        events_url = services.get("http://www.onvif.org/ver10/events/wsdl") or device_url
        analytics_url = services.get("http://www.onvif.org/ver20/analytics/wsdl") or ""

        profiles = probe_media(report, media_url, args.user, args.pw, skew,
                                public_host, args.rtsp_port)
        vac_tokens = [p["vac_token"] for p in profiles if p.get("vac_token")]
        if analytics_url and vac_tokens:
            probe_analytics(report, analytics_url, args.user, args.pw, skew, vac_tokens)
        probe_events(report, events_url, args.user, args.pw, skew, args.seconds,
                      public_host, public_port)
        probe_rtsp(report, profiles, args.user, args.pw)
    finally:
        report.close()

    print(f"\nReport:  {txt}")
    print(f"JSON:    {js}")


if __name__ == "__main__":
    main()