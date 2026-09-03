#!/bin/bash
# HTTPS with self-signed certificates

export WEB_CERT="/Volumes/DATA/work/projects/cctv/webnvr/192.168.0.21+2.pem"
export WEB_KEY="/Volumes/DATA/work/projects/cctv/webnvr/192.168.0.21+2-key.pem"

cd /Volumes/DATA/work/projects/cctv/webnvr
echo "🔒 Wails DevServer 시작 (HTTPS 모드)"
echo "📱 접속 주소: https://192.168.0.21:5173"
echo ""
wails dev
