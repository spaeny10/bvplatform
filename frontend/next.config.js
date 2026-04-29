/** @type {import('next').NextConfig} */

// In docker-compose the api lives in a sibling container reachable as
// "api:8080" on the backplane network. In native dev the api runs on
// the host at 127.0.0.1:8080. API_INTERNAL_URL lets the same Next.js
// build work in both places — compose sets it to http://api:8080;
// local dev leaves it unset and falls back to 127.0.0.1.
//
// Why 127.0.0.1 and not localhost: on Windows hosts running WSL,
// "localhost" resolves to ::1 (IPv6) first, where Microsoft's
// wslrelay.exe listens and forwards traffic into the WSL subsystem —
// which can be running an entirely different (stale) copy of the API.
// Pinning to 127.0.0.1 keeps Next.js's server-side rewrite hitting the
// host's actual ONVIF tool process. Docker Compose is unaffected — it
// overrides API_INTERNAL_URL explicitly.
const apiTarget = process.env.API_INTERNAL_URL || 'http://127.0.0.1:8080';

const nextConfig = {
    async rewrites() {
        return [
            { source: '/api/:path*',        destination: `${apiTarget}/api/:path*` },
            { source: '/auth/:path*',       destination: `${apiTarget}/auth/:path*` },
            { source: '/ws',                destination: `${apiTarget}/ws` },
            { source: '/hls/:path*',        destination: `${apiTarget}/hls/:path*` },
            { source: '/exports/:path*',    destination: `${apiTarget}/exports/:path*` },
            { source: '/recordings/:path*', destination: `${apiTarget}/recordings/:path*` },
            { source: '/snapshots/:path*',  destination: `${apiTarget}/snapshots/:path*` },
            { source: '/webrtc/:path*',     destination: `${apiTarget}/webrtc/:path*` },
        ];
    },
};

module.exports = nextConfig;
