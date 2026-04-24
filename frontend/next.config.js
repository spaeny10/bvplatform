/** @type {import('next').NextConfig} */
const nextConfig = {
    async rewrites() {
        return [
            {
                source: '/api/:path*',
                destination: 'http://localhost:8080/api/:path*',
            },
            {
                source: '/auth/:path*',
                destination: 'http://localhost:8080/auth/:path*',
            },
            {
                source: '/ws',
                destination: 'http://localhost:8080/ws',
            },
            {
                source: '/hls/:path*',
                destination: 'http://localhost:8080/hls/:path*',
            },
            {
                source: '/exports/:path*',
                destination: 'http://localhost:8080/exports/:path*',
            },
            {
                source: '/recordings/:path*',
                destination: 'http://localhost:8080/recordings/:path*',
            },
            {
                source: '/snapshots/:path*',
                destination: 'http://localhost:8080/snapshots/:path*',
            },
            {
                source: '/webrtc/:path*',
                destination: 'http://localhost:8080/webrtc/:path*',
            },
        ];
    },
};

module.exports = nextConfig;
