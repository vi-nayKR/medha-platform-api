#!/usr/bin/env bash
# setup-nginx.sh — Install Nginx and expose Medha API publicly on port 80.
# Run as root or with sudo on the server (Ubuntu 24.04).
# Usage: sudo bash scripts/setup-nginx.sh

set -euo pipefail

echo "=== Installing Nginx ==="
apt-get update -y
apt-get install -y nginx

echo "=== Writing Nginx virtual host ==="
cat > /etc/nginx/sites-available/medha-api << 'EOF'
server {
    listen 80;
    server_name _;   # Matches any hostname / IP

    # Security headers
    add_header X-Content-Type-Options nosniff;
    add_header X-Frame-Options DENY;
    add_header Referrer-Policy no-referrer;

    # Proxy everything to the Go API
    location / {
        proxy_pass         http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header   Host              $host;
        proxy_set_header   X-Real-IP         $remote_addr;
        proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto $scheme;
        proxy_read_timeout 60s;
        proxy_send_timeout 60s;
        client_max_body_size 150M;
    }
}
EOF

ln -sf /etc/nginx/sites-available/medha-api /etc/nginx/sites-enabled/medha-api
rm -f /etc/nginx/sites-enabled/default

echo "=== Testing Nginx config ==="
nginx -t

echo "=== Opening firewall port 80 ==="
if command -v ufw &>/dev/null; then
    ufw allow 80/tcp
    ufw allow 443/tcp
    echo "  UFW: port 80/443 allowed"
fi

echo "=== Reloading Nginx ==="
systemctl enable nginx
systemctl restart nginx

echo ""
echo "✅ Done! Medha API is now publicly accessible via HTTP (port 80)."
echo "   Swagger UI : http://<SERVER_PUBLIC_IP>/api/docs"
echo "   Health     : http://<SERVER_PUBLIC_IP>/health"
echo ""
echo "To get the public IP: curl -s ifconfig.me"
