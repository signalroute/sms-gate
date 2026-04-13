#!/usr/bin/env bash
set -euo pipefail
BINARY=${1:-/usr/local/bin/sms-gate}
id -u smsgate &>/dev/null || useradd -r -s /sbin/nologin smsgate
mkdir -p /etc/sms-gate /var/lib/sms-gate
chown smsgate:smsgate /var/lib/sms-gate
[ -f /etc/sms-gate/config.yaml ] || cp config.yaml.example /etc/sms-gate/config.yaml
cp "$BINARY" /usr/local/bin/sms-gate
cp deploy/sms-gate.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now sms-gate
echo "sms-gate installed and started."
