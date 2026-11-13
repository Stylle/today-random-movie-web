#!/bin/sh
set -eu
if [ "$(id -u)" = 0 ]; then
  /usr/local/bin/data-init
  exec su-exec 10001:10001 /app/today-random-movie "$@"
fi
exec /app/today-random-movie "$@"

