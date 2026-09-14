# Betting Processing Go

Sistema de processamento de transações de apostas (bets, wins, losses, refunds, rollbacks) entre provedores de jogos e carteiras de jogadores.

## Pré-requisitos

- Go 1.26.7+
- Docker e Docker Compose v2+
- migrate CLI (para migrations locais)
- cURL (para testes de health)

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
- Atributo `provider_id` em cada usuário

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

## Exemplos de Chamadas

### Criar carteira

```bash
# Obter token
TOKEN=$(curl -s -X POST http://localhost:8081/realms/betting/protocol/openid-connect/token \
  -d "client_id=betting-api&client_secret=secret123&grant_type=password&username=provider1&password=provider1" \
  | jq -r '.access_token')

# Criar carteira
curl -X POST http://localhost:8080/wallets \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "playerId": "player-1",
    "initialBalance": {"amount": "100.00", "currency": "BRL"}
  }'
```

### Processar BET

```bash
curl -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: provider1:ext-bet-001" \
  -d '{
    "providerId": "provider1",
    "externalTransactionId": "ext-bet-001",
    "playerId": "player-1",
    "walletId": "WALLET_ID",
    "roundId": "round-1",
    "gameId": "game-1",
    "kind": "BET",
    "money": {"amount": "50.00", "currency": "BRL"}
  }'
```

### Processar WIN

```bash
curl -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: provider1:ext-win-001" \
  -d '{
    "providerId": "provider1",
    "externalTransactionId": "ext-win-001",
    "playerId": "player-1",
    "walletId": "WALLET_ID",
    "roundId": "round-1",
    "gameId": "game-1",
    "kind": "WIN",
    "money": {"amount": "30.00", "currency": "BRL"}
  }'
```

### Processar REFUND

```bash
curl -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: provider1:ext-refund-001" \
  -d '{
    "providerId": "provider1",
    "externalTransactionId": "ext-refund-001",
    "playerId": "player-1",
    "walletId": "WALLET_ID",
    "roundId": "round-1",
    "gameId": "game-1",
    "kind": "REFUND",
    "money": {"amount": "50.00", "currency": "BRL"},
    "referenceExternalTransactionId": "ext-bet-001"
  }'
```

### Consultar carteira

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/wallets/WALLET_ID
```

### Consultar ledger

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/wallets/WALLET_ID/ledger
```

### Reconciliação

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8080/wallets/WALLET_ID/reconciliation
```

### Swagger / OpenAPI

A documentação interativa da API está disponível em modo development:

- **Swagger UI**: http://localhost:8080/docs/
- **Swagger JSON**: http://localhost:8080/docs/specs

Para testar endpoints protegidos no Swagger:
1. Obtenha um token JWT (veja "Obter token" acima)
2. Clique no botão "Authorize" no Swagger UI
3. Cole o token no campo `Value` (formato: `Bearer <token>`)

### Health checks

```bash
curl http://localhost:8080/health/live
curl http://localhost:8080/health/ready
```

### Métricas

```bash
curl http://localhost:8080/metrics
```

As métricas Prometheus estão expostas em `http://localhost:8080/metrics`. Para configurar o Prometheus, adicione o endpoint no `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'betting-api'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: '/metrics'
```

Métricas disponíveis:

| Métrica | Tipo | Descrição |
|---------|------|-----------|
| `wager_transactions_total` | CounterVec | Total de transações por status |
| `wager_transactions_duplicate_total` | Counter | Total de transações duplicadas |
| `wager_transactions_retry_total` | Counter | Total de retries de referência |
| `wager_transactions_dlq_total` | Counter | Total de eventos para DLQ |
| `wager_concurrency_conflict_total` | Counter | Total de conflitos de lock |
| `wager_outbox_pending_count` | Gauge | Eventos outbox pendentes |
| `wager_outbox_publish_latency_seconds` | Histogram | Latência de publicação |
| `wager_processing_duration_seconds` | Histogram | Duração de processamento |
| `wager_reconciliation_divergence_total` | Counter | Divergências de reconciliação |

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
docker compose -f docker-compose.test.yml up -d postgres migrate

# 2. Exportar variável de ambiente
export TEST_DATABASE_URL="postgresql://api_test:api_test@localhost:5433/api_test?sslmode=disable"

# 3. Executar testes de integração
go test -v -race -count=1 -tags=integration -timeout=300s ./...
```

### Testes E2E

Os testes E2E requerem todos os serviços rodando:

```bash
# 1. Subir ambiente completo de teste
docker compose -f docker-compose.test.yml up --build -d

# 2. Aguardar serviços ficarem prontos
docker compose -f docker-compose.test.yml ps

# 3. Executar testes E2E
go test -v -race -count=1 -tags=e2e -timeout=300s ./internal/e2e/...

# 4. Derrubar ambiente
docker compose -f docker-compose.test.yml down -v
```

Ou usar o Makefile:

```bash
make test_e2e
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
├── Makefile                  # Comandos úteis
├── ARCHITECTURE.md           # Documentação da arquitetura
└── README.md                 # Este arquivo
```