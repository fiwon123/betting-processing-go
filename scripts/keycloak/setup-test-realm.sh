#!/bin/bash
# Keycloak provisioning script for test environment
# Waits for Keycloak to be ready, then creates realm, client, and test users.

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
  -d "grant_type=password" | python3 -c "import sys,json; print(json.load(sys.stdin)['access_token'])")

if [ -z "$ADMIN_TOKEN" ]; then
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
    python3 -c "import sys,json; users=json.load(sys.stdin); print(users[0]['id'] if users else '')")
  
  if [ -n "$USER_ID" ]; then
    curl -sf -X PUT "${KC_URL}/admin/realms/${REALM}/users/${USER_ID}/execute-actions-email" \
      -H "Authorization: Bearer ${ADMIN_TOKEN}" \
      -H "Content-Type: application/json" \
      -d "[]" > /dev/null 2>&1 || true
    
    # Remove any required actions
    curl -sf -X PUT "${KC_URL}/admin/realms/${REALM}/users/${USER_ID}" \
      -H "Authorization: Bearer ${ADMIN_TOKEN}" \
      -H "Content-Type: application/json" \
      -d "{\"requiredActions\": []}" > /dev/null 2>&1 || true
  fi
done

# Insert provider_id attributes directly into Keycloak DB
echo "Setting up provider_id attributes via DB..."
PGHOST="${KC_DB_HOST:-postgres}" PGPORT="5432" PGUSER="${KC_DB_USER:-keycloak_test}" PGPASSWORD="${KC_DB_PASSWORD:-keycloak_test}" PGDATABASE="${KC_DB:-keycloak_test}" \
  psql -c "
    INSERT INTO user_attribute (user_id, name, value)
    SELECT id, 'provider_id', 'provider1' FROM user_entity WHERE username = 'provider1'
    ON CONFLICT DO NOTHING;
  " 2>/dev/null || echo "DB attribute insert skipped (may need direct DB access)"

PGHOST="${KC_DB_HOST:-postgres}" PGPORT="5432" PGUSER="${KC_DB_USER:-keycloak_test}" PGPASSWORD="${KC_DB_PASSWORD:-keycloak_test}" PGDATABASE="${KC_DB:-keycloak_test}" \
  psql -c "
    INSERT INTO user_attribute (user_id, name, value)
    SELECT id, 'provider_id', 'provider2' FROM user_entity WHERE username = 'provider2'
    ON CONFLICT DO NOTHING;
  " 2>/dev/null || echo "DB attribute insert skipped (may need direct DB access)"

echo "Keycloak provisioning complete."
