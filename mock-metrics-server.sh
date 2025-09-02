#!/bin/bash
# Simple mock metrics server for testing
# Usage: ./mock-metrics-server.sh [port]

PORT=${1:-5252}

echo "Starting mock metrics server on port $PORT..."
echo "Serving Tailscale-compatible metrics at http://localhost:$PORT/metrics"

python3 -c "
import http.server
import socketserver
from urllib.parse import urlparse

class MetricsHandler(http.server.SimpleHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/metrics':
            self.send_response(200)
            self.send_header('Content-Type', 'text/plain; version=0.0.4; charset=utf-8')
            self.end_headers()
            
            # Mock Tailscale metrics
            metrics = '''# HELP tailscale_up Tailscale connectivity status
# TYPE tailscale_up gauge
tailscale_up 1

# HELP tailscale_derp_latency_seconds DERP server latency
# TYPE tailscale_derp_latency_seconds gauge
tailscale_derp_latency_seconds{region=\"1\"} 0.023
tailscale_derp_latency_seconds{region=\"2\"} 0.045

# HELP tailscale_client_rx_bytes_total Received bytes
# TYPE tailscale_client_rx_bytes_total counter
tailscale_client_rx_bytes_total{path=\"direct\",type=\"rx\"} 1234567

# HELP tailscale_client_tx_bytes_total Transmitted bytes  
# TYPE tailscale_client_tx_bytes_total counter
tailscale_client_tx_bytes_total{path=\"direct\",type=\"tx\"} 987654

# HELP tailscale_client_connection_failures_total Connection failures
# TYPE tailscale_client_connection_failures_total counter
tailscale_client_connection_failures_total{reason=\"no_path\"} 5
'''
            self.wfile.write(metrics.encode())
        else:
            self.send_response(404)
            self.end_headers()
            self.wfile.write(b'Not Found')

with socketserver.TCPServer(('', $PORT), MetricsHandler) as httpd:
    print(f'Mock metrics server running on port $PORT')
    print('Press Ctrl+C to stop')
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        print('\\nShutting down...')
"
