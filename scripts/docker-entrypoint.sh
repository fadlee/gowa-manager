#!/bin/sh
set -e

: "${DATA_DIR:=/app/data}"

mkdir -p "$DATA_DIR"
chown -R app:app "$DATA_DIR"

exec su-exec app "$@"
