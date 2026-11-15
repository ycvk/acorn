#!/usr/bin/env bash
set -euo pipefail

API="https://api.mechkeys.shop/v1"
TOKEN="$(cat "$HOME/.config/mechkeys/token")"
SKU="K2-V2-RGB-BROWN"

product=$(curl -fsS "$API/products/$SKU" -H "Authorization: Bearer $TOKEN")
stock=$(printf '%s' "$product" | jq -r '.stock')
price=$(printf '%s' "$product" | jq -r '.price')
echo "stock: $stock, price: $price"

if [ "$stock" -gt 0 ]; then
  curl -fsS -X POST "$API/orders" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"sku\":\"$SKU\",\"quantity\":1,\"payment_method\":\"saved_card\",\"shipping_address\":\"default\",\"confirm\":true}" \
    | jq '{order_id, status, total}'
fi
