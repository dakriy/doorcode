#!/bin/sh
# Starts the broker, first creating its dynamic-security configuration when
# the data volume has none: an administrator named and passworded by
# DOORCODE_BROKER_ADMIN_USERNAME and DOORCODE_BROKER_ADMIN_PASSWORD, allowed
# to manage clients and, beyond what the plugin's own administrator may do,
# to publish to doorcode/#, which the server needs to reach the doors.
set -eu
config=/mosquitto/data/dynamic-security.json
if [ ! -f "$config" ]; then
	mosquitto_ctrl dynsec init "$config" "$DOORCODE_BROKER_ADMIN_USERNAME" "$DOORCODE_BROKER_ADMIN_PASSWORD" >/dev/null
	sed -i 's|"acls":\[|"acls":[{"acltype":"publishClientSend","topic":"doorcode/#","allow":true},|' "$config"
	chown mosquitto:mosquitto "$config"
	chmod 600 "$config"
fi
exec mosquitto -c /mosquitto/config/mosquitto.conf
