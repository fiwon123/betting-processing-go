# Betting Processing Go

Sistema de processamento de transações de apostas (bets, wins, losses, refunds, rollbacks) entre provedores de jogos e carteiras de jogadores.

## Pré-requisitos

- Go 1.26.7+
- Docker e Docker Compose v2+
- migrate CLI (para migrations locais)
- cURL (para testes de health)
- jq (para testes manuais)

## Variáveis de Ambiente

Copie `.env.example` para `.env`:

```bash
cp .env.example .env
```

Variáveis principais:

| Variável | Descrição | Padrão |
|----------|-----------|--------|
| `API_DB` | Nome do banco de dados | `api` |
| `API_DB_USER` | Usuário do banco | `api` |
| `API_DB_PASSWORD` | Senha do banco | `api` |
| `API_DB_HOST` | Host do banco | `postgres` |
| `API_DB_PORT` | Porta do banco | `5432` |
| `API_OIDC_ENABLED` | Habilitar autenticação OIDC | `true` |
| `API_OIDC_ISSUER_URL` | URL do issuer Keycloak | `http://keycloak:8080/realms/betting` |
| `API_OIDC_CLIENT_ID` | Client ID no Keycloak | `betting-api` |
| `API_OIDC_JWKS_URL` | URL das chaves JWKS | `http://keycloak:8080/realms/betting/protocol/openid-connect/certs` |
| `SQS_QUEUE_NAME` | Nome da fila SQS de entrada | `wager-transactions.fifo` |
| `SQS_OUTBOUND_QUEUE_NAME` | Nome da fila SQS de saída | `wager-events-outbound.fifo` |

## Inicialização

### Subir o ambiente completo

```bash
docker compose up --build
```

Isso sobe:
- PostgreSQL 16 (porta 5432)
- Keycloak 26.7.3 (porta 8081)
- Ministack/SQS (porta 4566)
- API (porta 8080)
- Worker (SQS consumer + reference resolver + outbox publisher)

### Provisionamento do Keycloak

O Keycloak é provisionado **automaticamente** ao executar `docker compose up --build`. O serviço `keycloak-init` aguarda o Keycloak ficar pronto e cria:

- Realm `betting`
- Client `betting-api` (secret: `secret123`, Direct Access Grants enabled)
- Usuários `provider1` (senha: `provider1`) e `provider2` (senha: `provider2`)
- Atributo `provider_id` em cada usuário (injetado diretamente no DB + mapper JWT)

Para verificar se o provisionamento funcionou, acesse o Keycloak Admin Console: http://localhost:8081 (login: `admin` / `admin`).

### Migrations

As migrations são executadas automaticamente via container `migrate`. Para executar manualmente:

```bash
# Subir apenas o banco
docker compose up -d postgres

# Executar migrations
make migrate_up

# Reverter última migration
make migrate_down

# Criar nova migration
make migrate_create VAL=nome_da_migration
```

### Inicialização das Filas SQS

As filas são criadas automaticamente via container `sqs-init`. Para criar manualmente:

```bash
docker compose up -d ministack
# Aguarde ministack estar pronto
docker compose run --rm sqs-init
```

## Execução da Aplicação

### Desenvolvimento local

```bash
# API
make run

# Worker (em outro terminal)
go run ./cmd/worker
```

### Via Docker

```bash
docker compose up --build api worker
```

## Teste Manual Passo a Passo

> **Importante:** O campo `playerId` deve ser um **UUID válido**. A coluna no banco de dados é do tipo `UUID`, portanto valores como `"player-1"` causarão erro. Use o formato `xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx`.

### 1. Subir o ambiente

```bash
docker compose up --build
```

Aguarde até ver `started` nos logs da API (~30-40 segundos):

```bash
docker compose logs -f api
# Ctrl+C para parar de assistir (containers continuam rodando)
```

### 2. Verificar health checks

```bash
curl http://localhost:8080/health/live
# {"status":"ok"}

curl http://localhost:8080/health/ready
# {"status":"ok"}
```

### 3. Obter token de autenticação

O setup automático cria dois usuários de exemplo no Keycloak:

| Usuário | Senha | provider_id |
|---------|-------|-------------|
| `provider1` | `provider1` | `provider1` |
| `provider2` | `provider2` | `provider2` |

```bash
TOKEN=$(curl -s -X POST http://localhost:8081/realms/betting/protocol/openid-connect/token \
  -d "client_id=betting-api&client_secret=secret123&grant_type=password&username=provider1&password=provider1" \
  | jq -r '.access_token')

# Verificar que o token contém o provider_id
echo "$TOKEN" | cut -d'.' -f2 | base64 -d 2>/dev/null | jq '{provider_id, preferred_username}'
```

### 4. Criar carteira

```bash
WALLET=$(curl -s -X POST http://localhost:8080/wallets \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "playerId": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "initialBalance": {"amount": "500.00", "currency": "BRL"}
  }')

echo $WALLET | jq '.'
# Deve retornar balance: "500.00", version: 1

WALLET_ID=$(echo $WALLET | jq -r '.id')
```

### 5. Processar BET (apostar R$ 100,00)

```bash
curl -s -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: provider1:ext-bet-001" \
  -d "{
    \"providerId\": \"provider1\",
    \"externalTransactionId\": \"ext-bet-001\",
    \"playerId\": \"a1b2c3d4-e5f6-7890-abcd-ef1234567890\",
    \"walletId\": \"$WALLET_ID\",
    \"roundId\": \"round-1\",
    \"gameId\": \"game-1\",
    \"kind\": \"BET\",
    \"money\": {\"amount\": \"100.00\", \"currency\": \"BRL\"}
  }" | jq '{status, idempotentReplay}'
# {"status":"PROCESSED","idempotentReplay":false}
```

### 6. Processar WIN (ganhar R$ 50,00)

```bash
curl -s -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: provider1:ext-win-001" \
  -d "{
    \"providerId\": \"provider1\",
    \"externalTransactionId\": \"ext-win-001\",
    \"playerId\": \"a1b2c3d4-e5f6-7890-abcd-ef1234567890\",
    \"walletId\": \"$WALLET_ID\",
    \"roundId\": \"round-1\",
    \"gameId\": \"game-1\",
    \"kind\": \"WIN\",
    \"money\": {\"amount\": \"50.00\", \"currency\": \"BRL\"}
  }" | jq '{status, idempotentReplay}'
# {"status":"PROCESSED","idempotentReplay":false}
```

### 7. Verificar saldo (500 - 100 + 50 = 450)

```bash
curl -s http://localhost:8080/wallets/$WALLET_ID \
  -H "Authorization: Bearer $TOKEN" | jq '{balance: .balance, version: .version}'
# {"balance":{"amount":"450.00","currency":"BRL"},"version":3}
```

### 8. Verificar ledger (histórico de movimentações)

```bash
curl -s "http://localhost:8080/wallets/$WALLET_ID/ledger?limit=10" \
  -H "Authorization: Bearer $TOKEN" | jq '.entries[] | {direction, amount: .amount.amount}'
# CREDIT  500.00  (abertura)
# DEBIT   100.00  (BET)
# CREDIT   50.00  (WIN)
```

### 9. Testar idempotência (reenviar o mesmo BET)

```bash
curl -s -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: provider1:ext-bet-001" \
  -d "{
    \"providerId\": \"provider1\",
    \"externalTransactionId\": \"ext-bet-001\",
    \"playerId\": \"a1b2c3d4-e5f6-7890-abcd-ef1234567890\",
    \"walletId\": \"$WALLET_ID\",
    \"roundId\": \"round-1\",
    \"gameId\": \"game-1\",
    \"kind\": \"BET\",
    \"money\": {\"amount\": \"100.00\", \"currency\": \"BRL\"}
  }" | jq '{idempotentReplay, status}'
# {"idempotentReplay":true,"status":"PROCESSED"}
# Saldo NÃO muda — a transação não é processada novamente
```

### 10. Testar saldo insuficiente (BET de R$ 999)

```bash
curl -s -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: provider1:ext-bet-big" \
  -d "{
    \"providerId\": \"provider1\",
    \"externalTransactionId\": \"ext-bet-big\",
    \"playerId\": \"a1b2c3d4-e5f6-7890-abcd-ef1234567890\",
    \"walletId\": \"$WALLET_ID\",
    \"roundId\": \"round-2\",
    \"gameId\": \"game-2\",
    \"kind\": \"BET\",
    \"money\": {\"amount\": \"999.00\", \"currency\": \"BRL\"}
  }" | jq '.'
# {"status":"REJECTED"} com saldo insuficiente
```

### 11. Testar isolamento de provedor

```bash
# Obter token do provider2
TOKEN2=$(curl -s -X POST http://localhost:8081/realms/betting/protocol/openid-connect/token \
  -d "client_id=betting-api&client_secret=secret123&grant_type=password&username=provider2&password=provider2" \
  | jq -r '.access_token')

# Tentar ler carteira do provider1 — deve retornar 403
curl -s http://localhost:8080/wallets/$WALLET_ID \
  -H "Authorization: Bearer $TOKEN2" | jq '.'
# {"error":"forbidden"}
```

### 12. Reconciliação

```bash
curl -s -X POST http://localhost:8080/wallets/$WALLET_ID/reconciliation \
  -H "Authorization: Bearer $TOKEN" | jq '.'
# {"consistent":true, ...}
```

### 13. Métricas

```bash
curl -s http://localhost:8080/metrics | grep "wager_transactions_total"
```

### 14. Swagger UI

Abra no navegador: **http://localhost:8080/docs/**

Para testar endpoints protegidos no Swagger:
1. Obtenha o token JWT (passo 3)
2. Clique no botão "Authorize" no Swagger UI
3. Cole o token no campo `Value` (formato: `Bearer <token>`)

### 15. Parar o ambiente

```bash
docker compose down -v
```

## Comandos de Teste

### Resumo rápido (via Makefile)

| Comando | Descrição |
|---------|-----------|
| `make test_unit` | Testes unitários com race detector |
| `make test_integration` | Testes de integração com PostgreSQL real |
| `make test_e2e` | Testes E2E com stack completa em Docker |
| `make test_all` | Unitários + E2E |
| `make test_full` | Todos com coverage |

### Testes unitários

```bash
go test ./...
```

### Testes com detector de race

```bash
go test -race ./...
```

### Verificação de código

```bash
go vet ./...
```

### Formatação

```bash
gofmt -w .
```

### Todos os testes (unitários + race)

```bash
make test_full
```

## Preparação de Dependências dos Testes

### Testes de integração

Os testes de integração requerem um banco de dados PostgreSQL real:

```bash
# 1. Subir banco de teste
docker compose --env-file .env.test -f docker-compose.test.yml up -d postgres migrate

# 2. Exportar variável de ambiente
export TEST_DATABASE_URL="postgresql://api_test:api_test@localhost:5433/api_test?sslmode=disable"

# 3. Executar testes de integração
go test -v -race -count=1 -tags=integration -timeout=300s ./...
```

### Testes E2E

Os testes E2E requerem todos os serviços rodando. O Makefile usa `.env.test` para isolar os bancos de dados de teste:

```bash
# Via Makefile (recomendado)
make test_e2e
```

Ou manualmente:

```bash
# 1. Subir ambiente completo de teste
docker compose --env-file .env.test -f docker-compose.test.yml up --build -d

# 2. Aguardar serviços ficarem prontos
docker compose --env-file .env.test -f docker-compose.test.yml ps

# 3. Executar testes E2E
go test -v -race -count=1 -tags=e2e -timeout=300s ./internal/e2e/...

# 4. Derrubar ambiente
docker compose --env-file .env.test -f docker-compose.test.yml down -v
```

### Testes de múltiplas instâncias

Os testes de concorrência simulam 3 instâncias independentes usando goroutines com conexões separadas ao banco:

```bash
go test -v -race -count=1 -tags=integration -run TestConcurrentInstances ./internal/wagertransaction/...
```

### Simulações de falha

Para simular falhas, os testes de integração manipulam diretamente o estado no banco:

- **Referência pendente**: define status da transação para `PENDING` antes de submeter reversão
- **Max retries**: define `ref_attempts = 10` na transação
- **Saldo insuficiente**: usa wallet com saldo baixo
- **Concorrência**: goroutines competindo pela mesma carteira

## Estrutura do Projeto

```
betting-processing-go/
├── cmd/
│   ├── api/main.go           # Servidor HTTP
│   ├── worker/main.go        # Workers SQS
│   └── sqs-init/main.go      # Inicialização de filas
├── internal/
│   ├── domain/               # Tipos compartilhados
│   ├── money/                # Value Object Money
│   ├── wallet/               # Agregado Wallet
│   ├── wagertransaction/     # Agregado WagerTransaction
│   ├── infra/                # Infraestrutura
│   └── adapters/             # HTTP handlers, middleware, workers
├── migrations/               # SQL migrations
├── scripts/                  # Scripts de setup
├── docker-compose.yaml       # Ambiente de desenvolvimento
├── docker-compose.test.yml   # Ambiente de teste
├── .env.example              # Variáveis de ambiente (dev)
├── .env.test                 # Variáveis de ambiente (teste)
├── Makefile                  # Comandos úteis
├── ARCHITECTURE.md           # Documentação da arquitetura
└── README.md                 # Este arquivo
```
