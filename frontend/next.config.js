/** @type {import('next').NextConfig} */

// In docker-compose the api lives in a sibling container reachable as
// "api:8080" on the backplane network. In native dev the api runs on
// the host at localhost:8080. API_INTERNAL_URL lets the same Next.js
// build work in both places — compose sets it to http://api:8080;
// local dev leaves it unset and falls back to localhost.
const apiTarget = process.env.API_INTERNAL_URL || 'http://localhost:8080';

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
