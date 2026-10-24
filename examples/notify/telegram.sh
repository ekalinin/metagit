#!/bin/sh
# metagit notification command: sends the event to a Telegram chat through
# the Bot API.
#
# Settings in the environment of metagit serve, for example in its
# systemd EnvironmentFile:
#   METAGIT_NOTIFY_COMMAND=/usr/local/lib/metagit/telegram.sh
#   TELEGRAM_BOT_TOKEN=123456:ABC-DEF
#   TELEGRAM_CHAT_ID=123456789
set -eu

# Uncomment to skip the message on every successful delivery.
# if [ "$METAGIT_EVENT" = delivered ]; then exit 0; fi

where=$METAGIT_PROJECT
if [ -n "$METAGIT_TARGET" ]; then
	where="$where ($METAGIT_TARGET)"
fi
# Telegram limits a message to 4096 characters.
text=$(printf 'metagit %s: %s' "$where" "$METAGIT_MESSAGE" | head -c 4000)

exec curl -fsS --retry 3 --max-time 10 -o /dev/null \
	--data-urlencode "chat_id=$TELEGRAM_CHAT_ID" \
	--data-urlencode "text=$text" \
	"https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/sendMessage"
