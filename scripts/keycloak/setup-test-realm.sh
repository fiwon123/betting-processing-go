#!/bin/sh
# Keycloak provisioning script for test environment
# Waits for Keycloak to be ready, then creates realm, client, and test users.
# Dependencies: curl, jq (installed in the init container)

set -e

KC_URL="${KC_URL:-http://localhost:8081}"
KC_ADMIN="${KC_ADMIN_USERNAME:-admin}"
KC_ADMIN_PASS="${KC_ADMIN_PASSWORD:-admin}"
REALM="betting"
CLIENT_ID="betting-api"
CLIENT_SECRET="secret123"

echo "Waiting for Keycloak to be ready..."
for i in $(seq 1 60); do
  if curl -sf "${KC_URL}/realms/master" > /dev/null 2>&1; then
    echo "Keycloak is ready."
    break
  fi
  if [ "$i" -eq 60 ]; then
    echo "Keycloak not ready after 60s, exiting."
    exit 1
  fi
  sleep 1
done

# Get admin token
ADMIN_TOKEN=$(curl -sf -X POST "${KC_URL}/realms/master/protocol/openid-connect/token" \
  -d "client_id=admin-cli" \
  -d "username=${KC_ADMIN}" \
  -d "password=${KC_ADMIN_PASS}" \
  -d "grant_type=password" | jq -r '.access_token')

if [ -z "$ADMIN_TOKEN" ] || [ "$ADMIN_TOKEN" = "null" ]; then
  echo "Failed to get admin token"
  exit 1
fi

# Create realm
echo "Creating realm ${REALM}..."
curl -sf -X POST "${KC_URL}/admin/realms" \
  -H "Authorization: Bearer ${ADMIN_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{
    \"realm\": \"${REALM}\",
    \"enabled\": true,
    \"registrationAllowed\": false,
    \"loginWithEmailAllowed\": true
  }" || echo "Realm may already exist"

# Create client
echo "Creating client ${CLIENT_ID}..."
curl -sf -X POST "${KC_URL}/admin/realms/${REALM}/clients" \
  -H "Authorization: Bearer ${ADMIN_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{
    \"clientId\": \"${CLIENT_ID}\",
    \"enabled\": true,
    \"publicClient\": false,
    \"secret\": \"${CLIENT_SECRET}\",
    \"directAccessGrantsEnabled\": true,
    \"standardFlowEnabled\": false,
    \"serviceAccountsEnabled\": false
  }" || echo "Client may already exist"

# Create provider1 user
echo "Creating user provider1..."
curl -sf -X POST "${KC_URL}/admin/realms/${REALM}/users" \
  -H "Authorization: Bearer ${ADMIN_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{
    \"username\": \"provider1\",
    \"enabled\": true,
    \"credentials\": [{
      \"type\": \"password\",
      \"value\": \"provider1\",
      \"temporary\": false
    }],
    \"attributes\": {
      \"provider_id\": [\"provider1\"]
    }
  }" || echo "User provider1 may already exist"

# Create provider2 user
echo "Creating user provider2..."
curl -sf -X POST "${KC_URL}/admin/realms/${REALM}/users" \
  -H "Authorization: Bearer ${ADMIN_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{
    \"username\": \"provider2\",
    \"enabled\": true,
    \"credentials\": [{
      \"type\": \"password\",
      \"value\": \"provider2\",
      \"temporary\": false
    }],
    \"attributes\": {
      \"provider_id\": [\"provider2\"]
    }
  }" || echo "User provider2 may already exist"

# Disable VERIFY_PROFILE required action for both users
echo "Disabling VERIFY_PROFILE for users..."
for USER in provider1 provider2; do
  USER_ID=$(curl -sf -H "Authorization: Bearer ${ADMIN_TOKEN}" \
    "${KC_URL}/admin/realms/${REALM}/users?username=${USER}" | \
    jq -r '.[0].id // empty')
  
  if [ -n "$USER_ID" ]; then
    curl -sf -X PUT "${KC_URL}/admin/realms/${REALM}/users/${USER_ID}" \
      -H "Authorization: Bearer ${ADMIN_TOKEN}" \
      -H "Content-Type: application/json" \
      -d "{\"requiredActions\": []}" > /dev/null 2>&1 || true
  fi
done

echo "Keycloak provisioning complete."
