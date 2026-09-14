# Arquitetura

## 1. Visão Geral

O Betting Processing Go é um serviço backend que processa transações de apostas (bets, wins, losses, refunds, rollbacks) entre provedores de jogos e carteiras de jogadores. Ele recebe requisições de transações via SQS, aplica regras de negócio, muta saldos de carteiras atomicamente e publica eventos de domínio.

## 2. Padrão Arquitetural

O sistema segue **Arquitetura Hexagonal (Ports and Adapters)** combinada com **Design Orientado a Domínio** e **pacotes baseados em features**:

- **Domínio** (`internal/domain`): tipos centrais, eventos, cálculo de hash de idempotência.
- **Serviços de domínio** (`internal/wallet`, `internal/wagertransaction`): lógica de negócio por agregado.
- **Infraestrutura** (`internal/infra`): banco de dados, SQS, configuração, logging, métricas.
- **Adaptadores** (`internal/adapters`): handlers HTTP, workers SQS, middleware de auth.

Cada pacote de feature possui sua própria interface de repositório, erros e tipos de domínio, mantendo limites limpos.

## 3. Stack Tecnológica

| Componente | Tecnologia |
|------------|-----------|
| Linguagem | Go 1.26.7 |
| HTTP Router | go-chi/chi v5 |
| DI | Uber Fx |
| Banco de Dados | PostgreSQL 16 (pgx/v5 driver) |
| Fila | Amazon SQS (FIFO) via ministack localmente |
| Auth | Keycloak 26.7.3 (OAuth2/OIDC) |
| JWT | golang-jwt/v5 (RS256) |
| Observabilidade | Zap (logs), Prometheus (métricas) |
| Infra | Docker Compose, migrate/migrate |

## 4. Layout de Pacotes

```
internal/
  domain/              -- tipos compartilhados: Event, Direction, hash de idempotência
  money/               -- Value Object Money (int64 centavos, apenas BRL)
  wallet/              -- Agregado Wallet: criar, debitar, creditar, reconciliar
  wagertransaction/    -- Agregado WagerTransaction: processar, rejeitar, resolver
  infra/
    cfg/               -- configuração via variáveis de ambiente
    db/                -- pgxpool, implementações de repositórios
    logger/            -- logger zap
    metrics/           -- contadores, histogramas e gauges Prometheus
    sqs/               -- wrapper do cliente SQS
  adapters/
    handlers/          -- handlers HTTP (wallet, wager transaction, health)
    middleware/         -- middleware de auth OIDC
    workers/           -- consumidor SQS, resolvedor de referências, publisher de outbox
cmd/
  api/main.go          -- entry point do servidor HTTP (Uber Fx)
  worker/main.go       -- worker SQS + resolvedor de referências + publisher de outbox
  sqs-init/main.go     -- bootstrapper de filas SQS
```

## 5. Representação do Dinheiro

O dinheiro é armazenado como `int64` **unidades menores** (centavos para BRL). Apenas BRL é suportado.

| Camada | Tipo | Exemplo BRL 10.50 |
|--------|------|--------------------|
| Go (interno) | `int64` | `1050` |
| PostgreSQL | `BIGINT` | `1050` |
| JSON API | `{"amount":"10.50","currency":"BRL"}` | string decimal |

O parsing (`money.ParseMoney`) valida formato, rejeita notação científica, negativos e valores que excedem o máximo de `int64`. Operações aritméticas verificam correspondência de moeda e overflow.

### Limites da Representação

O tipo `int64` com escala de 2 casas decimais suporta valores de `-92.233.720.368.547,75` a `92.233.720.368.547,75`. Para BRL, o valor máximo representável é ~92 trilhões de reais, suficiente para qualquer cenário de uso. Overflow é detectado e rejeitado em parsing, soma, subtração e negação.

### Normalização no Hash de Idempotência

O hash de idempotência (SHA-256) é calculado sobre strings normalizadas: valores trimmed, chaves ordenadas alfabeticamente. A `idempotencyKey` é excluída do cálculo. A `referenceExternalTransactionId` é incluída apenas quando não vazia.

## 6. Estratégia de Concorrência

Mutações de saldo de carteira usam **SELECT FOR UPDATE + optimistic locking**:

1. `SELECT ... FOR UPDATE` adquire um lock de linha na carteira.
2. Regras de negócio (saldo suficiente, não-negativo) são validadas contra a linha bloqueada.
3. `UPDATE wallets SET balance=$1, version=$2 WHERE id=$3 AND version=$4` realiza a verificação otimista.
4. Se `RowsAffected() == 0`, `ErrConcurrentUpdate` é retornada e a mensagem SQS volta a ficar visível para retry.

Isso evita perdas de atualização enquanto evita locks prolongados durante toda a transação.

## 7. Idempotência

A idempotência é persistente via tabela `wager_transactions`:

- **Chave**: índice único `(provider, idempotency_key)`. Chave padrão é `provider_id:external_transaction_id`.
- **Hash do payload**: SHA-256 de campos JSON ordenados. Em recebimento duplicado, se o hash armazenado difere, `ErrPayloadConflict` é retornada (erro terminal).
- **Replay**: requisições duplicadas retornam o resultado original sem reprocessamento.

### Algoritmo de Idempotência

Os campos usados para cálculo do hash são: `providerId`, `externalTransactionId`, `playerId`, `walletId`, `roundId`, `gameId`, `kind`, `money.amount`, `money.currency`, `referenceExternalTransactionId`. A `idempotencyKey` é **excluída** do cálculo.

Valores são normalizados: strings trimmed, chaves ordenadas alfabeticamente. O resultado é um hash SHA-256 em hexadecimal (64 caracteres).

### Detecção de Conflito

Se uma requisição chega com a mesma `idempotencyKey` mas payload diferente (hash divergente), o sistema retorna erro de conflito (409). Isso previne que um provedor reutilize uma chave para transações diferentes.

## 8. Padrão Inbox/Outbox

### Inbox (`inbox_messages` table)

Deduplica mensagens SQS por `(consumer_name, message_id)`. Na recepção, `RecordReceived` é chamado dentro da mesma transação SQL que processa o domínio. Se a mensagem já foi recebida e completada, o processamento é pulado. Mensagens incompletas disparam replay da transação existente.

**Importante**: O registro de inbox ocorre exclusivamente dentro da transação SQL (via `RecordReceivedTx`), garantindo atomicidade entre o registro de recebimento e as mudanças de domínio. Isso previne perda de mensagens em caso de crash.

### Outbox (`outbox_events` table)

Todas as mudanças de estado (inserções de transação, atualizações de saldo, entradas de ledger) e inserções de eventos acontecem na **mesma transação do banco de dados**. Um `OutboxPublisher` em background polls eventos não publicados e os envia para a fila SQS de saída FIFO, marcando-os como publicados. Isso garante entrega at-least-once sem dual writes.

### Contenção entre Publicadores

O `FindPending` usa `FOR UPDATE SKIP LOCKED`, que previne que múltiplas instâncias do publisher contentionem pelo mesmo evento. Cada publisher pega um lote diferente de eventos.

## 9. Autenticação e Autorização

- **Keycloak** serve como provedor de identidade (OAuth2/OIDC).
- O middleware HTTP (`internal/adapters/middleware/auth.go`) valida JWTs usando RS256, buscando chaves JWKS com cache (TTL de 5 minutos, cooldown de 30 segundos).
- O JWT deve conter um claim `provider_id` (fallback para `sub`).
- Rotas protegidas: `/wagering`, `/providers/*`, `/wallets/*`. Endpoints de health e metrics são desprotegidos.
- O `provider_id` extraído é armazenado no contexto da requisição e usado para impor **isolamento entre provedores** — requisições só podem operar em carteiras/transações do seu próprio provedor.

### Extração do Provider ID

O middleware tenta extrair o `provider_id` do JWT na seguinte ordem:
1. Claim `provider_id` no JWT
2. Claim `sub` (Keycloak subject)

O valor é armazenado no contexto e acessado via `middleware.GetProviderID(ctx)`.

## 10. Códigos de Falha

| Código | Significado | Retry? |
|--------|------------|-----------|
| `INSUFFICIENT_BALANCE` | Saldo insuficiente para BET ou WIN | Não |
| `REVERSAL_INSUFFICIENT_BALANCE` | Saldo insuficiente para ROLLBACK (deveria reverter um WIN/REFUND que consumiu saldo) | Não |
| `CURRENCY_MISMATCH` | Moeda da carteira difere da moeda da requisição | Não |
| `WALLET_ERROR` | Erro inesperado no serviço de carteira | Sim |
| `MISSING_REFERENCE` | Reversão enviada sem `referenceExternalTransactionId` | Não |
| `REFERENCE_NOT_FOUND` | Transação referenciada não encontrada por `(provider, externalId)` | Não |
| `REFERENCE_NOT_SUCCESSFUL` | Transação referenciada não está em estado `PROCESSED` | Não |
| `DOUBLE_REVERSAL` | Já existe uma reversão bem-sucedida (REFUND ou ROLLBACK) para a mesma referência | Não |
| `REVERSAL_VALUE_MISMATCH` | Valor da reversão não corresponde ao valor da transação referenciada | Não |
| `EXTERNAL_ID_REUSE` | Mesmo (providerId, externalTransactionId) reprocessado com chave de idempotência diferente | Não |
| `INVALID_REFERENCE_TYPE` | Tipo de transação referenciada inválido para reversão (ex: `LOSS`) | Não |
| `PROVIDER_MISMATCH` | JWT `provider_id` não corresponde ao `providerId` da requisição | Não |
| `PLAYER_MISMATCH` | IDs de jogador não coincidem entre transação e referência | Não |
| `WALLET_MISMATCH` | IDs de carteira não coincidem entre transação e referência | Não |
| `ROUND_MISMATCH` | IDs de rodada não coincidem entre transação e referência | Não |
| `VALIDATION_ERROR` | Falha genérica de validação (fallback em `mapValidationError`) | Não |
| `INVALID_REFERENCE_TYPE` | Tipo de transação referenciada inválido para reversão (ex: `LOSS`) | Não |
| `OPENING_REJECTED_EXTERNAL` | Tentativa de criar transação OPENING via API externa | Não |
| `CONCURRENT_UPDATE` | Conflito de lock otimista | Sim |

**Terminal vs retryable**: `IsTerminalBusinessError` em `internal/wagertransaction/errors.go` marca a maioria dos erros de negócio como terminais (mensagem SQS deletada). Apenas `WALLET_ERROR` e conflitos de concorrência (`ErrConcurrentUpdate`) são retryáveis.

## 11. Tipos de Evento

Todos os eventos são gravados em `outbox_events` dentro da mesma transação do banco e publicados assincronamente.

| Tipo de Evento | Agregado | Trigger |
|---------------|----------|---------|
| `WagerTransactionProcessed` | `WagerTransaction` | Transação processada com sucesso (BET, WIN, LOSS, REFUND, ROLLBACK) |
| `WalletBalanceChanged` | `Wallet` | Saldo mutado por BET, WIN, REFUND ou ROLLBACK |
| `WagerTransactionRejected` | `WagerTransaction` | Transação rejeitada por violação de regra de negócio |
| `WagerTransactionPendingReference` | `WagerTransaction` | Reversão enfileirada porque a transação referenciada ainda está `PENDING` |

### Envelope do Evento

Cada evento contém:
- `eventId`: identificador único do evento (ex: `evt-<txID>-processed`)
- `eventType`: tipo do evento
- `aggregateType` / `aggregateId`: agregado afetado
- `correlationId`: ID da transação (para tracing)
- `causationId`: ID do comando/causa (opcional, para tracing em cascata)
- `occurredAt`: timestamp UTC
- `version`: versão do schema do evento (atualmente sempre 1)
- `data`: payload específico do tipo de evento

## 12. Máquina de Estados das Transações

```
                    ┌─────────────────────────┐
                    │         PENDING         │
                    │   (intermediário)       │
                    └──────────┬──────────────┘
                               │
              ┌────────────────┼────────────────┐
              │                │                │
              ▼                ▼                ▼
       ┌─────────────┐  ┌─────────────┐  ┌──────────────────┐
       │  PROCESSED  │  │  REJECTED   │  │ PENDING_REFERENCE│
       │  (terminal) │  │  (terminal) │  │  (aguardando     │
       └─────────────┘  └─────────────┘  │   referência)    │
                                         └────────┬─────────┘
                                                  │
                                          ┌───────┼────────┐
                                          │                │
                                          ▼                ▼
                                   ┌─────────────┐  ┌─────────────┐
                                   │  PROCESSED  │  │  REJECTED   │
                                   │  (resolvido)│  │  (excedeu   │
                                   └─────────────┘  │   retries)  │
                                                    └─────────────┘
```

### Transições Permitidas

- `PENDING` → `PROCESSED` (BET/WIN/REFUND/ROLLBACK processado com sucesso)
- `PENDING` → `REJECTED` (falha de negócio: saldo insuficiente, validação, etc.)
- `REFUND/ROLLBACK` → `PENDING_REFERENCE` (transação referenciada ainda está PENDING)
- `PENDING_REFERENCE` → `PROCESSED` (referência chegou e está PROCESSED)
- `PENDING_REFERENCE` → `REJECTED` (referência rejeitada/falhou OU excedeu max retries)

## 12.1. Combinações de REFUND e ROLLBACK

### Regras

| Transação Original | Reversão 1 | Reversão 2 | Resultado |
|--------------------|------------|------------|-----------|
| BET | REFUND | — | OK |
| BET | ROLLBACK | — | OK |
| BET | REFUND | ROLLBACK | **REJECTED** (double reversal) |
| BET | ROLLBACK | REFUND | **REJECTED** (double reversal) |
| WIN | REFUND | — | OK |
| WIN | ROLLBACK | — | OK |
| WIN | REFUND | ROLLBACK | **REJECTED** (double reversal) |

### Implementação

`HasSuccessfulReversal(ctx, referenceID)` verifica se **qualquer** reversão (REFUND ou ROLLBACK) já foi processada para a mesma referência, independentemente do tipo. A query SQL usa `IN ('REFUND', 'ROLLBACK')` na cláusula WHERE.

### Código de Falha para Saldo Insuficiente

- **BET/WIN**: `INSUFFICIENT_BALANCE` — a carteira não tem saldo para debitar
- **ROLLBACK (revertendo WIN ou REFUND)**: `REVERSAL_INSUFFICIENT_BALANCE` — a reversão tentou debitar mas o saldo não permite. Código diferente para diferenciar no frontend/monitoramento.

## 13. Resolução de Referências Pendentes

Quando uma reversão (REFUND/ROLLBACK) é submetida antes da transação referenciada estar em estado `PROCESSED`, a reversão fica em `PENDING_REFERENCE`.

### Ciclo de Vida

1. Reversão recebida → transação referenciada em PENDING → reversão salva com status `PENDING_REFERENCE`
2. `ReferenceWorker` polls transações `PENDING_REFERENCE` a cada 10 segundos
3. Para cada transação, tenta resolver chamando `ResolvePendingReference`
4. Se a referência ainda está PENDING → incrementa `ref_attempts`, aguarda próximo ciclo
5. Se a referência está PROCESSED → debita/credita carteira e resolve
6. Se `ref_attempts >= 10` → rejeita com `REFERENCE_NOT_FOUND`

### Backoff Exponencial

O intervalo entre tentativas segue `2^min(ref_attempts, 6)` segundos (máximo 64 segundos). Após 10 tentativas, a reversão é rejeitada.

### Prevenção de Dupla Reversão

O método `HasSuccessfulReversal` verifica se **qualquer** reversão (REFUND ou ROLLBACK) já foi processada para a mesma referência. Isso impede que um BET seja tanto REFUND quanto ROLLBACKado.

## 14. Topologia de Filas

| Fila | Tipo | Propósito |
|------|------|-----------|
| `wager-transactions.fifo` | SQS FIFO | Requisições de transação de entrada dos provedores |
| `wager-transactions-dlq.fifo` | SQS FIFO | Dead-letter queue para mensagens que excedem max receives |
| `wager-events-outbound.fifo` | SQS FIFO | Eventos de domínio de saída (processed, rejected, balance changed) |

Todas as filas são FIFO para garantir ordenação por message group.

### Redrive Policy

A fila principal usa `maxReceiveCount: 3`. Após 3 falhas, a mensagem vai para a DLQ. A DLQ é destino final (sem redrive policy). A fila de saída (`wager-events-outbound.fifo`) não possui redrive policy — eventos abandonados após 5 tentativas são logados com warning.

### Visibility Timeout

O `VisibilityTimeout` das filas é configurado em 30 segundos. Mensagens que falham temporariamente ficam invisíveis por 30s antes de retornarem à fila para reprocessamento.

### MessageGroupId e MessageDeduplicationId

**Entrada (wager-transactions.fifo)**: O `MessageGroupId` é definido pelo produtor. A deduplicação usa `ContentBasedDeduplication: true` + inbox no banco (`inbox_messages` com PK `(consumer_name, message_id)`).

**Saída (wager-events-outbound.fifo)**:
- `MessageGroupId`: `{aggregateType}-{aggregateID}` — agrupa eventos do mesmo agregado
- `MessageDeduplicationId`: `{eventType}-{aggregateID}` — deduplica por tipo+agregado

Isso garante que eventos do mesmo agregado cheguem em ordem e que republicações (após crash) não resultem em duplicatas no SQS.

### Consumidor SQS

O consumidor (`WagerSQSWorker`) recebe mensagens com o envelope:
```json
{
  "messageId": "msg-123",
  "type": "WagerTransactionRequested",
  "occurredAt": "2026-09-08T12:00:00.000Z",
  "data": {
    "providerId": "provider-a",
    "externalTransactionId": "transaction-123",
    "idempotencyKey": "provider-a:transaction-123",
    ...
  }
}
```

A chave de idempotência é `data.idempotencyKey`. O `messageId` do envelope é usado como identidade durável para rastreamento. Mensagens inválidas (JSON malformado) são removidas imediatamente. Rejeições de negócio terminais também resultam em remoção. Falhas transitórias utilizam o visibility timeout para retry.

## 15. Métricas (Prometheus)

| Métrica | Tipo | Descrição |
|---------|------|-----------|
| `wager_transactions_total` | CounterVec (by status) | Total de transações por status |
| `wager_transactions_duplicate_total` | Counter | Total de transações duplicadas recebidas |
| `wager_transactions_retry_total` | Counter | Total de retries de resolução de referência |
| `wager_transactions_dlq_total` | Counter | Total de eventos que excederam max attempts na outbox |
| `wager_concurrency_conflict_total` | Counter | Total de conflitos de lock otimista |
| `wager_outbox_pending_count` | Gauge | Eventos outbox não publicados |
| `wager_outbox_publish_latency_seconds` | Histogram | Latência de publicação da outbox |
| `wager_processing_duration_seconds` | Histogram | Duração do processamento de transações |
| `wager_reconciliation_divergence_total` | Counter | Total de divergências de reconciliação |

## 16. Migrations e Schema do Banco

Gerenciado por `migrate/migrate` (10 migrations):

| Migration | Descrição |
|-----------|-----------|
| `000001` | `wallets` — id, player_id, provider_id, currency, balance (BIGINT), version, timestamps. Único em `(player_id, currency, provider_id)`. |
| `000002` | `wager_transactions` — id, origin, external_id, provider, idempotency_key, payload_hash, wallet_id (FK), player_id, round_id, game_id, transaction_type, amount, currency, references, status, failure_code, result_balance, timestamps. Único em `(provider, idempotency_key)`. |
| `000003` | `wallet_ledger_entries` — log de auditoria imutável com CHECK constraint garantindo `balance_after = balance_before +/- amount`. Triggers impedem UPDATE e DELETE. |
| `000004` | `inbox_messages` — `(consumer_name, message_id)` PK, payload_hash, received_at, completed_at. |
| `000005` | `outbox_events` — id, aggregate_type/id, event_type, payload (JSONB), attempts, next_attempt_at, published_at. |
| `000006` | Adiciona `ref_attempts` e `ref_next_attempt_at` em `wager_transactions` para resolução de referência pendente. |
| `000007` | Adiciona `result_balance` em `wager_transactions` para cache do saldo após processamento. |
| `000008` | Adiciona `provider_id` em `wallets` e atualiza a constraint única. |
| `000009` | Restaura `DEFAULT gen_random_uuid()` em todas as tabelas (id, wallet_id). |
| `000010` | Remove `wallets_player_currency_uq` — suporte a múltiplos provedores por jogador. A identidade da carteira passa a ser `(player_id, currency, provider_id)` ao invés de apenas `(player_id, currency)`. A única carteira por jogador continua garantida dentro do mesmo provedor. |
| `000011` | Unique index parcial `wager_transactions_one_successful_reversal_uq` — impede que duas reversões (REFUND ou ROLLBACK) bem-sucedidas apontem para a mesma referência. Previne race conditions na verificação de dupla reversão. |

## 17. Observabilidade

### Logs
Logging estruturado via `zap` (formato JSON em produção, console em desenvolvimento).

### Health Endpoints
- `GET /health/live` — sempre 200 (liveness probe).
- `GET /health/ready` — verifica ping do PostgreSQL e acessibilidade da fila SQS (readiness probe).

### Reconciliação
`wallet.Service.Reconcile` computa o saldo somando todas as entradas do ledger de uma carteira e compara com o saldo armazenado. Divergências incrementam `wager_reconciliation_divergence_total`.

## 18. Limitações e Trabalho Não Concluído

### Limitações Técnicas

1. **Apenas BRL**: A moeda BRL é a única suportada. Outras moedas podem ser adicionadas removendo a validação em `money.NewMoney`.
2. **Max retries fixo**: O número máximo de tentativas de resolução de referência é fixo em 10 (hardcoded). Poderia ser configurável via variável de ambiente.
3. **Sem TTL**: Não há expiração baseada em tempo. Se a referência nunca chegar, a reversão será rejeitada após ~10 minutos (10 tentativas com backoff).
4. **Outbox max attempts**: Após 5 falhas na publicação, o evento é abandonado com log de warning. Não há mecanismo de alerta.
5. **Visibility timeout fixo**: O timeout de visibilidade SQS é fixo em 30 segundos, não configurável.
6. **Sem DLQ processor**: A DLQ é criada mas nenhum worker consome dela. Mensagens que excedem maxReceiveCount ficam na DLQ. Por design, a DLQ é destino final.
7. **Event version**: O `version` dos eventos é sempre 1. Não há esquema de evolução de eventos.
8. **CausationID**: O campo `causationId` no envelope de evento existe mas não é preenchido em todos os eventos.

### Features Opcionais Não Implementadas (Diferenciais)

As seguintes features são classificadas como diferenciais no spec e não foram implementadas:

| Feature | Justificativa |
|---------|---------------|
| **Double-entry ledger** | Opcional no spec. O ledger atual já é append-only com CHECK constraints e triggers de imutabilidade. |
| **OpenTelemetry tracing** | Opcional. Logs já incluem `correlation_id` para rastreamento de requisições. |
| **Dashboards Grafana** | Opcional. Métricas Prometheus já estão expostas em `/metrics`. |
| **Load tests** | Opcional. Spec diz "não há meta mínima de RPS". |
| **Client credentials flow entre serviços** | O worker acessa PostgreSQL diretamente (decisão de design, ver seção 19). |

## 19. Decisões de Design

### Por que Fx?
Uber Fx permite composição declarativa de dependências, lifecycle management (OnStart/OnStop), e injeção automática. Isso reduz boilerplate e facilita testes (containers de teste podem substituir implementações concretas).

### Por que optimistic locking ao invés de transações longas?
SELECT FOR UPDATE segura a lock apenas durante o SELECT + UPDATE, não durante toda a transação SQL. Isso reduz contention e permite throughput maior.

### Por que inbox/outbox no mesmo commit?
Garante atomicidade: ou a mensagem é recebida E o domínio é mutado, ou nada acontece. Previne:
- Mensagens perdidas (inbox marcado mas domínio não commitado)
- Processamento duplicado (domínio commitado mas inbox não marcado)

### Por que money como int64?
Floats causam imprecisão decimal (0.1 + 0.2 ≠ 0.3). Usar centavos como int64 elimina esse problema completamente. O JSON usa string decimal para compatibilidade com clientes.

### Por que o Worker acessa PostgreSQL diretamente?
O worker (`cmd/worker`) é um componente interno que não expõe API HTTP. Ele acessa PostgreSQL diretamente via `db.NewTxManager` em vez de usar `client_credentials` flow para autenticar com a API. Razões:

1. **Simplicidade**: Não há necessidade de autenticação entre componentes internos que rodam na mesma rede.
2. **Performance**: Evita overhead de HTTP + JWT token exchange para cada operação.
3. **Atomicidade**: O worker pode usar a mesma transação SQL para processar mensagens SQS e mutar o domínio, garantindo consistência.
4. **Segurança**: O worker não é exposto externamente; apenas a API recebe tráfego externo.

O trade-off é que o worker não é um microserviço independente — ele está acoplado ao schema do banco. Isso é aceitável porque worker e API são parte do mesmo sistema e são deployados juntos.

### Por que keycloak-init como container separado?
O provisionamento do Keycloak é feito por um container Alpine (`keycloak-init`) que usa a REST API admin para criar realm, client e usuários. Alternativas consideradas:

1. **Keycloak import/export**: Requer arquivos JSON de exportação e configuração específica do Keycloak. Menos flexível.
2. **Init container na inicialização do Keycloak**: O Keycloak não suporta hooks de init nativos.
3. **REST API (escolhido)**: Flexível, funciona com Keycloak limpo, não depende de versão específica do Keycloak.

## 20. Estratégia de Testes

### Categorias

| Categoria | Build Tag | Quantidade | Infra |
|-----------|-----------|:----------:|-------|
| **Unitários** | (nenhuma) | 106 | Nenhuma — testam lógica pura de domínio |
| **Integração** | `integration` | 20 | PostgreSQL real via `TEST_DATABASE_URL` |
| **E2E** | `e2e` | 19 | Stack completa: PostgreSQL, Keycloak, SQS (ministack), API, Worker |

### Como Rodar

```bash
# Unitários (rápido, sem infra)
go test ./...

# Unitários com race detector
go test -race ./...

# Integração (requer PostgreSQL rodando)
docker compose -f docker-compose.test.yml up -d postgres migrate
export TEST_DATABASE_URL=postgresql://api_test:api_test@localhost:5433/api_test?sslmode=disable
go test -v -race -count=1 -tags=integration -timeout=300s ./...

# E2E (requer Docker)
make test_e2e
```

### O que Cada Categoria Testa

**Unitários** (106 testes):
- `money`: parsing, aritmética, overflow, formatação, JSON
- `wallet`: criação, débito, crédito, validação, ledger
- `wagertransaction`: criação, máquina de estados, tipos
- `domain`: hash de idempotência
- `handlers`: validação de entrada, HTTP status codes
- `middleware`: autenticação OIDC, JWT, provider_id
- `workers`: publicação de outbox, lifecycle de workers

**Integração** (20 testes):
- Concorrência: débito duplo, múltiplas instâncias competindo
- Idempotência: replay com 50 goroutines, conflito de payload
- Ledger: imutabilidade (UPDATE/DELETE bloqueados), consistência matemática
- Referências: resolução PENDING_REFERENCE, max retries
- Dupla reversão: REFUND+ROLLBACK rejeitado
- Fx: lifecycle de DI, conexão com banco

**E2E** (19 testes):
- Fluxo completo HTTP: criar carteira → BET → WIN → consultar
- Isolamento entre provedores (provider1 ≠ provider2)
- Replays idempotentes via HTTP
- Rejeição de BET sem saldo
- Dupla refúnd rejeitada
- Concorrência via HTTP (duas bets de 80 em carteira de 100)
- Cadeia de referências
- Health endpoints, métricas

### Infraestrutura de Testes

- **Unitários**: Não dependem de infraestrutura externa
- **Integração**: Usam `pgxpool` real contra PostgreSQL (container `postgres` no compose de teste, porta 5433)
- **E2E**: Usam compose completo com `test-runner` que executa `go test` dentro de Docker, contra Keycloak real, SQS real (ministack), API real

Mocks são usados apenas em testes unitários para isolar lógica de handlers e workers. Testes de integração e E2E usam infraestrutura real.
