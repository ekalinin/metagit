#!/bin/sh
# metagit notification command: posts the event JSON to a webhook.
#
# Settings in the environment of metagit serve, for example in its
# systemd EnvironmentFile:
#   METAGIT_NOTIFY_COMMAND=/usr/local/lib/metagit/webhook.sh
#   NOTIFY_WEBHOOK_URL=https://example.com/hooks/metagit
#
# metagit passes the event as JSON on stdin and in METAGIT_* variables.
set -eu

# Uncomment to skip the message on every successful delivery.
# if [ "$METAGIT_EVENT" = delivered ]; then exit 0; fi

exec curl -fsS --retry 3 --max-time 10 -o /dev/null \
	-H 'Content-Type: application/json' \
	--data-binary @- \
	"$NOTIFY_WEBHOOK_URL"
