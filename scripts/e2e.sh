#!/bin/sh
# Suite E2E: one Tansu Account + all six apps on scratch ports.
# Builds from this repo into .e2e/ (gitignored). Nothing here touches
# production ports or data.
#
# Usage: ./scripts/e2e.sh start|test|stop
set -u
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=$ROOT/.e2e
ACCT=http://127.0.0.1:3206
SECRET=e2e6-secret-0123456789

# app dir -> "binary:port" (dirs are new names, binary/client ids stay kura*)
APPS="account:kuraaccount:3206 notes:kuranotes:3200 assistant:kurachat:3201 calendar:kuracalendar:3203 spend:kuraspend:3204 people:kurapeople:3205 home:kurahome:3207"
CLIENTS="notes:kuranotes:3200 assistant:kurachat:3201 calendar:kuracalendar:3203 spend:kuraspend:3204 people:kurapeople:3205 home:kurahome:3207"

build_all() {
  mkdir -p "$WORK"
  for a in $APPS; do
    dir=$(echo "$a" | cut -d: -f1); bin=$(echo "$a" | cut -d: -f2)
    (cd "$ROOT/apps/$dir" && go build -o "$WORK/$bin" ./cmd/$bin) || return 1
  done
}

start_all() {
  build_all || { echo "build failed"; return 1; }
  # Fresh fleet every start: stale users would poison signup assertions.
  rm -rf "$WORK/data" "$WORK"/jar-*
  mkdir -p "$WORK/data"
  REG=$(for a in $CLIENTS; do
    dir=$(echo "$a" | cut -d: -f1); id=$(echo "$a" | cut -d: -f2); port=$(echo "$a" | cut -d: -f3)
    printf '{"id":"%s","secret":"%s","name":"%s","home":"http://127.0.0.1:%s/","redirect_uris":["http://127.0.0.1:%s/login/kura/callback"]},' \
      "$id" "$SECRET" "$dir" "$port" "$port"
  done)
  REG="[${REG%,}]"
  BIND=127.0.0.1:3206 DATA_DIR=$WORK/data/acct KURA_CLIENTS_JSON="$REG" \
    setsid -f $WORK/kuraaccount serve </dev/null >>$WORK/account.log 2>&1
  for a in $CLIENTS; do
    dir=$(echo "$a" | cut -d: -f1); id=$(echo "$a" | cut -d: -f2); port=$(echo "$a" | cut -d: -f3)
    BIND=127.0.0.1:$port DATA_DIR=$WORK/data/$dir \
      KURA_ACCOUNT_URL=$ACCT KURA_CLIENT_ID=$id KURA_CLIENT_SECRET=$SECRET \
      setsid -f $WORK/$id serve </dev/null >>$WORK/$id.log 2>&1
  done
  sleep 3
  curl -s -o /dev/null -w "account: %{http_code}\n" $ACCT/up
  for a in $CLIENTS; do
    port=$(echo "$a" | cut -d: -f3)
    curl -s -o /dev/null -w "$port: %{http_code}\n" http://127.0.0.1:$port/up
  done
}

stop_all() {
  # Bracket trick: the pattern never matches this script's own cmdline.
  for pat in 'e2e/kuraaccoun[t] serve' 'e2e/kuranote[s] serve' 'e2e/kuracha[t] serve' \
             'e2e/kuracalenda[r] serve' 'e2e/kuraspen[d] serve' \
             'e2e/kurapeopl[e] serve' 'e2e/kurahom[e] serve'; do
    for pid in $(pgrep -f "$pat"); do kill "$pid" 2>/dev/null; done
  done
  echo stopped
}

run_test() {
  rm -f $WORK/jarA
  curl -s -c $WORK/jarA $ACCT/signup -o /dev/null
  CSRF=$(awk '/kura_csrf/{print $NF}' $WORK/jarA)
  SIGNUP=$(curl -s -b $WORK/jarA -c $WORK/jarA -o /dev/null -w "%{http_code}" \
    --data-urlencode "email=suite@example.com" \
    --data-urlencode "password=password123" \
    --data-urlencode "password_confirmation=password123" \
    --data-urlencode "csrf_token=$CSRF" $ACCT/signup)
  if [ "$SIGNUP" != "303" ]; then
    # user may already exist from a previous run: log in instead
    curl -s -c $WORK/jarA $ACCT/login -o /dev/null
    CSRF=$(awk '/kura_csrf/{print $NF}' $WORK/jarA)
    curl -s -b $WORK/jarA -c $WORK/jarA -o /dev/null -w "login: %{http_code}\n" \
      --data-urlencode "email=suite@example.com" \
      --data-urlencode "password=password123" \
      --data-urlencode "csrf_token=$CSRF" $ACCT/login
  else
    echo "signup: 303"
  fi
  FAIL=0
  for a in $CLIENTS; do
    dir=$(echo "$a" | cut -d: -f1); id=$(echo "$a" | cut -d: -f2); port=$(echo "$a" | cut -d: -f3)
    JAR=$WORK/jar-$id
    rm -f $JAR
    AUTHZ=$(curl -s -c $JAR -o /dev/null -w "%{redirect_url}" http://127.0.0.1:$port/login/kura)
    case "$AUTHZ" in
      "$ACCT/authorize?"*) ;;
      *) echo "$id: BAD authorize: $AUTHZ"; FAIL=1; continue ;;
    esac
    CALLBACK=$(curl -s -b $WORK/jarA -o /dev/null -w "%{redirect_url}" "$AUTHZ")
    case "$CALLBACK" in
      "http://127.0.0.1:$port/login/kura/callback?code="*) ;;
      *) echo "$id: BAD callback: $CALLBACK"; FAIL=1; continue ;;
    esac
    CODE=$(curl -s -b $JAR -c $JAR -o /dev/null -w "%{http_code} %{redirect_url}" "$CALLBACK")
    HOME=$(curl -s -b $JAR -o /dev/null -w "%{http_code}" http://127.0.0.1:$port/)
    if [ "$CODE" = "303 http://127.0.0.1:$port/" ] && [ "$HOME" = "200" ]; then
      echo "$id: OK (callback 303, home 200)"
    else
      echo "$id: FAIL callback=[$CODE] home=[$HOME]"; FAIL=1
    fi
  done
  curl -s -b $WORK/jarA $ACCT/ -o $WORK/hub.html -w "hub: %{http_code}\n"
  LIT=$(grep -o 'class="tail lit"' $WORK/hub.html | wc -l)
  echo "lit tails: $LIT (want 6)"
  [ "$LIT" = "6" ] || FAIL=1
  # Standalone regression, live: password signup on notes with a local user.
  rm -f $WORK/jar-local
  curl -s -c $WORK/jar-local http://127.0.0.1:3200/signup -o /dev/null
  CSRF=$(awk '/kura_csrf/{print $NF}' $WORK/jar-local)
  S=$(curl -s -b $WORK/jar-local -c $WORK/jar-local -o /dev/null -w "%{http_code}" \
    --data-urlencode "email=local@example.com" --data-urlencode "password=password123" \
    --data-urlencode "password_confirmation=password123" \
    --data-urlencode "csrf_token=$CSRF" http://127.0.0.1:3200/signup)
  H=$(curl -s -b $WORK/jar-local -o /dev/null -w "%{http_code}" http://127.0.0.1:3200/)
  BTN=$(curl -s http://127.0.0.1:3200/login | grep -c "login/kura" || true)
  echo "standalone signup: $S home: $H kura-button-present: $BTN"
  [ "$S" = "303" ] && [ "$H" = "200" ] && [ "$BTN" -ge 1 ] || FAIL=1
  if [ "$FAIL" = "0" ]; then echo "E2E-6: ALL GREEN"; else echo "E2E-6: FAILURES"; fi
  return $FAIL
}

case "${1:-}" in
  start) start_all ;;
  stop) stop_all ;;
  test) run_test ;;
  *) echo "usage: scripts/e2e.sh start|test|stop" ;;
esac
