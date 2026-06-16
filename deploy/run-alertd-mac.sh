#!/bin/zsh
# Wrapper launchd untuk alertd di Mac: cd ke repo → load .env → exec binary.
# Secret HANYA di .env (gitignored); plist tidak menyimpan kredensial.
set -e
cd "$HOME/Documents/xau-ict-engine"
set -a
. ./.env
set +a
exec ./bin/alertd "$@"
