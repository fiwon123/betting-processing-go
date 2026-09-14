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

# Disable VERIFY_PROFILE required action at realm level
# In Keycloak 26.x this is enabled by default and blocks "Account is not fully set up"
echo "Disabling VERIFY_PROFILE required action..."
curl -sf -X PUT "${KC_URL}/admin/realms/${REALM}/authentication/required-actions/VERIFY_PROFILE" \
  -H "Authorization: Bearer ${ADMIN_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"enabled":false}' > /dev/null 2>&1 || echo "Warning: could not disable VERIFY_PROFILE"

# Create JWT mapper for provider_id attribute on betting-api client
# This ensures provider_id appears in the access token
echo "Creating provider_id JWT mapper..."
CLIENT_UUID=$(curl -sf -H "Authorization: Bearer ${ADMIN_TOKEN}" \
  "${KC_URL}/admin/realms/${REALM}/clients?clientId=${CLIENT_ID}" | jq -r '.[0].id')

if [ -n "$CLIENT_UUID" ]; then
  curl -sf -X POST "${KC_URL}/admin/realms/${REALM}/clients/${CLIENT_UUID}/protocol-mappers/models" \
    -H "Authorization: Bearer ${ADMIN_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{
      "name": "provider_id",
      "protocol": "openid-connect",
      "protocolMapper": "oidc-usermodel-attribute-mapper",
      "config": {
        "user.attribute": "provider_id",
        "claim.name": "provider_id",
        "id.token.claim": "true",
        "access.token.claim": "true",
        "userinfo.token.claim": "true",
        "jsonType.label": "String",
        "multivalued": "false"
      }
    }' > /dev/null 2>&1 || echo "Warning: could not create provider_id mapper (may already exist)"
fi

# Workaround: Keycloak 26.x REST API does not persist user attributes via PUT.
# Set provider_id attribute directly in the database for both users.
echo "Setting provider_id attributes in database..."
psql "${KC_DB_URL:-postgresql://keycloak:keycloak@postgres:5432/keycloak}" -c "
  INSERT INTO user_attribute (user_id, name, value)
  SELECT id, 'provider_id', 'provider1' FROM user_entity WHERE username = 'provider1'
  ON CONFLICT DO NOTHING;
" > /dev/null 2>&1 || echo "Warning: could not set provider1 attribute via DB (psql may not be available)"

psql "${KC_DB_URL:-postgresql://keycloak:keycloak@postgres:5432/keycloak}" -c "
  INSERT INTO user_attribute (user_id, name, value)
  SELECT id, 'provider_id', 'provider2' FROM user_entity WHERE username = 'provider2'
  ON CONFLICT DO NOTHING;
" > /dev/null 2>&1 || echo "Warning: could not set provider2 attribute via DB (psql may not be available)"

echo "Keycloak provisioning complete."
