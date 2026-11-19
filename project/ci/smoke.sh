#!/usr/bin/env bash
set -euo pipefail

COMPOSE=${COMPOSE:-"docker compose -f deploy/docker-compose.yml"}
API_URL=${API_URL:-http://localhost:8080}
ADMIN_URL=${ADMIN_URL:-http://localhost:8081}

temp="$($COMPOSE run --rm api staff create -username ci-project | sed -n 's/^临时密码.*：//p')"
test -n "$temp"
jar="$(mktemp)"
trap 'rm -f "$jar"' EXIT
admin() {
	curl -sf -b "$jar" -c "$jar" -H 'Content-Type: application/json' -H "Origin: $ADMIN_URL" "$@"
}
admin -X POST "$ADMIN_URL/admin/v1/auth/login" -d "{\"username\":\"ci-project\",\"password\":\"$temp\"}" >/dev/null
admin -X PUT "$ADMIN_URL/admin/v1/me/password" -d "{\"current_password\":\"$temp\",\"new_password\":\"ci-project-new-password\"}" >/dev/null
product_id="$(admin -X POST "$ADMIN_URL/admin/v1/products" -d '{"name":"ci smoke test product","price_cents":1000,"stock":5}' | jq -r .id)"
order_id="$(curl -sf -X POST "$API_URL/v1/orders" -H 'Content-Type: application/json' -d "{\"customer_name\":\"ci\",\"product_id\":\"$product_id\",\"quantity\":1}" | jq -r .id)"
for _ in $(seq 1 20); do
	status="$(curl -sf "$API_URL/v1/orders/$order_id" | jq -r .status)"
	echo "order status: $status"
	[ "$status" = "confirmed" ] && exit 0
	sleep 3
done
$COMPOSE logs worker
exit 1
