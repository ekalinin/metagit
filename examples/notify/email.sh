#!/bin/sh
# metagit notification command: sends the event by email with sendmail.
#
# Settings in the environment of metagit serve, for example in its
# systemd EnvironmentFile:
#   METAGIT_NOTIFY_COMMAND=/usr/local/lib/metagit/email.sh
#   NOTIFY_EMAIL_TO=owner@example.com
#   NOTIFY_EMAIL_FROM=metagit@example.com   (optional)
#   SENDMAIL=/usr/sbin/sendmail             (optional, default: sendmail from PATH)
set -eu

# Uncomment to skip the message on every successful delivery.
# if [ "$METAGIT_EVENT" = delivered ]; then exit 0; fi

{
	printf 'To: %s\n' "$NOTIFY_EMAIL_TO"
	if [ -n "${NOTIFY_EMAIL_FROM:-}" ]; then
		printf 'From: %s\n' "$NOTIFY_EMAIL_FROM"
	fi
	printf 'Subject: metagit: %s in %s\n' "$METAGIT_EVENT" "$METAGIT_PROJECT"
	printf 'MIME-Version: 1.0\nContent-Type: text/plain; charset=UTF-8\n\n'
	printf '%s\n\n' "$METAGIT_MESSAGE"
	printf 'Project: %s\n' "$METAGIT_PROJECT"
	if [ -n "$METAGIT_TARGET" ]; then
		printf 'Target: %s\n' "$METAGIT_TARGET"
	fi
	if [ -n "$METAGIT_REF" ]; then
		printf 'Ref: %s\nArchive ref: %s\n' "$METAGIT_REF" "$METAGIT_ARCHIVE_REF"
	fi
	if [ -n "$METAGIT_REFS" ]; then
		printf 'Refs: %s\n' "$METAGIT_REFS"
	fi
	printf 'Time: %s\n' "$METAGIT_TIME"
} | "${SENDMAIL:-sendmail}" -t
