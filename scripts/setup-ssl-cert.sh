#!/bin/bash
# Script để tạo và renew SSL certificate bằng Certbot (Let's Encrypt)

set -e

# ============================================
# CẤU HÌNH
# ============================================
DOMAIN="${DOMAIN:-}"                        # Domain cần tạo certificate
OUTPUT_DIR="${OUTPUT_DIR:-./certs}"         # Thư mục lưu certificate
EMAIL="${EMAIL:-}"                          # Email cho Let's Encrypt (optional)
CERTBOT_MODE="${CERTBOT_MODE:-standalone}"  # standalone hoặc webroot

# ============================================
# KIỂM TRA ĐIỀU KIỆN
# ============================================
if [ -z "$DOMAIN" ]; then
    echo "❌ Lỗi: Chưa set DOMAIN"
    echo ""
    echo "Cách sử dụng:"
    echo "  DOMAIN=ai-gw.wearewarp.link ./scripts/pull-acm-cert.sh"
    echo ""
    echo "Các biến môi trường:"
    echo "  DOMAIN       - Domain cần tạo certificate (bắt buộc)"
    echo "  EMAIL        - Email cho Let's Encrypt (optional, recommended)"
    echo "  OUTPUT_DIR   - Thư mục lưu cert (default: ./certs)"
    echo "  CERTBOT_MODE - standalone hoặc webroot (default: standalone)"
    echo ""
    echo "Ví dụ:"
    echo "  DOMAIN=ai-gw.wearewarp.link EMAIL=admin@wearewarp.link ./scripts/pull-acm-cert.sh"
    exit 1
fi

# Kiểm tra certbot
if ! command -v certbot &> /dev/null; then
    echo "❌ Lỗi: Certbot chưa được cài đặt"
    echo ""
    echo "Cài đặt certbot:"
    echo "  Ubuntu/Debian: sudo apt update && sudo apt install certbot"
    echo "  CentOS/RHEL:   sudo yum install certbot"
    echo "  macOS:         brew install certbot"
    echo "  Amazon Linux:  sudo amazon-linux-extras install epel && sudo yum install certbot"
    exit 1
fi

# ============================================
# TẠO THƯ MỤC OUTPUT
# ============================================
mkdir -p "$OUTPUT_DIR"

echo "🔐 Đang tạo certificate cho domain: $DOMAIN"
echo "   Mode: $CERTBOT_MODE"
echo "   Output: $OUTPUT_DIR"
echo ""

# ============================================
# TẠO CERTIFICATE
# ============================================
CERTBOT_ARGS="certonly"

if [ "$CERTBOT_MODE" = "standalone" ]; then
    # Standalone mode - certbot tự chạy web server trên port 80
    # Cần dừng các service đang dùng port 80 trước
    CERTBOT_ARGS="$CERTBOT_ARGS --standalone"
    echo "⚠️  Standalone mode: Đảm bảo port 80 không bị chiếm"
elif [ "$CERTBOT_MODE" = "webroot" ]; then
    # Webroot mode - dùng web server hiện có
    WEBROOT="${WEBROOT:-/var/www/html}"
    CERTBOT_ARGS="$CERTBOT_ARGS --webroot -w $WEBROOT"
    echo "📁 Webroot mode: $WEBROOT"
fi

CERTBOT_ARGS="$CERTBOT_ARGS -d $DOMAIN"

if [ -n "$EMAIL" ]; then
    CERTBOT_ARGS="$CERTBOT_ARGS --email $EMAIL --agree-tos --no-eff-email"
else
    CERTBOT_ARGS="$CERTBOT_ARGS --register-unsafely-without-email --agree-tos"
fi

# Chạy certbot
echo "🚀 Chạy certbot..."
sudo certbot $CERTBOT_ARGS

# ============================================
# COPY CERTIFICATE ĐẾN OUTPUT DIR
# ============================================
LETSENCRYPT_DIR="/etc/letsencrypt/live/$DOMAIN"

if [ ! -d "$LETSENCRYPT_DIR" ]; then
    echo "❌ Lỗi: Không tìm thấy certificate tại $LETSENCRYPT_DIR"
    exit 1
fi

echo "📋 Copy certificate đến $OUTPUT_DIR..."

sudo cp "$LETSENCRYPT_DIR/cert.pem" "$OUTPUT_DIR/cert.pem"
sudo cp "$LETSENCRYPT_DIR/chain.pem" "$OUTPUT_DIR/chain.pem"
sudo cp "$LETSENCRYPT_DIR/fullchain.pem" "$OUTPUT_DIR/fullchain.pem"
sudo cp "$LETSENCRYPT_DIR/privkey.pem" "$OUTPUT_DIR/privkey.pem"

# Set ownership và permissions
sudo chown $(whoami):$(whoami) "$OUTPUT_DIR"/*.pem
chmod 600 "$OUTPUT_DIR/privkey.pem"
chmod 644 "$OUTPUT_DIR/cert.pem" "$OUTPUT_DIR/chain.pem" "$OUTPUT_DIR/fullchain.pem"

echo ""
echo "✅ Certificate đã được tạo thành công!"
echo ""
echo "📁 Files:"
echo "   - $OUTPUT_DIR/cert.pem       (Certificate)"
echo "   - $OUTPUT_DIR/chain.pem      (Certificate Chain)"
echo "   - $OUTPUT_DIR/fullchain.pem  (Full Chain)"
echo "   - $OUTPUT_DIR/privkey.pem    (Private Key)"
echo ""
echo "📝 Cấu hình trong config.yaml:"
echo "   tls:"
echo "     mode: manual"
echo "     cert: $OUTPUT_DIR/fullchain.pem"
echo "     key: $OUTPUT_DIR/privkey.pem"
echo ""
echo "🔄 Để renew certificate (chạy định kỳ mỗi 60-90 ngày):"
echo "   sudo certbot renew"
echo "   # Sau đó chạy lại script này để copy cert mới"
echo ""
echo "⏰ Thiết lập auto-renew với cron:"
echo "   sudo crontab -e"
echo "   # Thêm dòng sau (chạy lúc 3:00 AM mỗi ngày):"
echo "   0 3 * * * certbot renew --quiet && cp /etc/letsencrypt/live/$DOMAIN/*.pem $OUTPUT_DIR/"

