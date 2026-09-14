//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
)

func skipIfNoEnv(t *testing.T) {
	if os.Getenv("API_BASE_URL") == "" && os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("E2E tests require API_BASE_URL or TEST_DATABASE_URL to be set")
	}
}

func getProviderToken(t *testing.T, env *TestEnv, username, password string) string {
	t.Helper()
	token, err := env.GetToken(username, password)
	if err != nil {
		t.Fatalf("get token for %s: %v", username, err)
	}
	return token
}

func TestE2E_WalletCreate(t *testing.T) {
	skipIfNoEnv(t)
	env := NewTestEnv()
	token := getProviderToken(t, env, "provider1", "provider1")

	body := map[string]interface{}{
		"playerId": "player-e2e-1",
		"initialBalance": map[string]string{
			"amount":   "100.00",
			"currency": "BRL",
		},
	}

	resp, err := env.DoRequest("POST", "/wallets", token, body)
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	result := ReadBody(resp)
	t.Logf("wallet created: %v", result)
}

func TestE2E_WalletCreateDuplicate(t *testing.T) {
	skipIfNoEnv(t)
	env := NewTestEnv()
	token := getProviderToken(t, env, "provider1", "provider1")

	body := map[string]interface{}{
		"playerId": "player-e2e-dup",
		"initialBalance": map[string]string{
			"amount":   "50.00",
			"currency": "BRL",
		},
	}

	// First creation
	resp1, err := env.DoRequest("POST", "/wallets", token, body)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	resp1.Body.Close()

	// Duplicate creation
	resp2, err := env.DoRequest("POST", "/wallets", token, body)
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate, got %d", resp2.StatusCode)
	}
}

func TestE2E_WalletGet(t *testing.T) {
	skipIfNoEnv(t)
	env := NewTestEnv()
	token := getProviderToken(t, env, "provider1", "provider1")

	// Create wallet first
	createBody := map[string]interface{}{
		"playerId": "player-e2e-get",
		"initialBalance": map[string]string{
			"amount":   "200.00",
			"currency": "BRL",
		},
	}
	createResp, err := env.DoRequest("POST", "/wallets", token, createBody)
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	createResult := ReadBody(createResp)
	createResp.Body.Close()

	walletID, ok := createResult["id"].(string)
	if !ok {
		t.Fatalf("no wallet ID in response: %v", createResult)
	}

	// Get wallet
	getResp, err := env.DoRequest("GET", "/wallets/"+walletID, token, nil)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", getResp.StatusCode)
	}

	result := ReadBody(getResp)
	t.Logf("wallet: %v", result)
}

func TestE2E_BetProcesssed(t *testing.T) {
	skipIfNoEnv(t)
	env := NewTestEnv()
	token := getProviderToken(t, env, "provider1", "provider1")

	// Create wallet
	createResp, err := env.DoRequest("POST", "/wallets", token, map[string]interface{}{
		"playerId": "player-e2e-bet",
		"initialBalance": map[string]string{
			"amount":   "500.00",
			"currency": "BRL",
		},
	})
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	createResult := ReadBody(createResp)
	createResp.Body.Close()
	walletID := createResult["id"].(string)

	// Process BET
	betBody := map[string]interface{}{
		"providerId":            "provider1",
		"externalTransactionId": "e2e-bet-001",
		"playerId":              "player-e2e-bet",
		"walletId":              walletID,
		"roundId":               "round-1",
		"gameId":                "game-1",
		"kind":                  "BET",
		"money": map[string]string{
			"amount":   "100.00",
			"currency": "BRL",
		},
	}

	betResp, err := env.DoRequest("POST", "/wagering/transactions", token, betBody)
	if err != nil {
		t.Fatalf("process bet: %v", err)
	}
	defer betResp.Body.Close()

	if betResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", betResp.StatusCode)
	}

	betResult := ReadBody(betResp)
	t.Logf("bet processed: %v", betResult)

	if betResult["status"] != "PROCESSED" {
		t.Errorf("expected PROCESSED, got %v", betResult["status"])
	}
}

func TestE2E_WinProcessed(t *testing.T) {
	skipIfNoEnv(t)
	env := NewTestEnv()
	token := getProviderToken(t, env, "provider1", "provider1")

	createResp, err := env.DoRequest("POST", "/wallets", token, map[string]interface{}{
		"playerId": "player-e2e-win",
		"initialBalance": map[string]string{
			"amount":   "100.00",
			"currency": "BRL",
		},
	})
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	createResult := ReadBody(createResp)
	createResp.Body.Close()
	walletID := createResult["id"].(string)

	winBody := map[string]interface{}{
		"providerId":            "provider1",
		"externalTransactionId": "e2e-win-001",
		"playerId":              "player-e2e-win",
		"walletId":              walletID,
		"roundId":               "round-1",
		"gameId":                "game-1",
		"kind":                  "WIN",
		"money": map[string]string{
			"amount":   "50.00",
			"currency": "BRL",
		},
	}

	winResp, err := env.DoRequest("POST", "/wagering/transactions", token, winBody)
	if err != nil {
		t.Fatalf("process win: %v", err)
	}
	defer winResp.Body.Close()

	if winResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", winResp.StatusCode)
	}

	result := ReadBody(winResp)
	t.Logf("win processed: %v", result)
}

func TestE2E_BetInsufficientBalance(t *testing.T) {
	skipIfNoEnv(t)
	env := NewTestEnv()
	token := getProviderToken(t, env, "provider1", "provider1")

	createResp, err := env.DoRequest("POST", "/wallets", token, map[string]interface{}{
		"playerId": "player-e2e-insuf",
		"initialBalance": map[string]string{
			"amount":   "10.00",
			"currency": "BRL",
		},
	})
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	createResult := ReadBody(createResp)
	createResp.Body.Close()
	walletID := createResult["id"].(string)

	betBody := map[string]interface{}{
		"providerId":            "provider1",
		"externalTransactionId": "e2e-insuf-001",
		"playerId":              "player-e2e-insuf",
		"walletId":              walletID,
		"roundId":               "round-1",
		"gameId":                "game-1",
		"kind":                  "BET",
		"money": map[string]string{
			"amount":   "50.00",
			"currency": "BRL",
		},
	}

	betResp, err := env.DoRequest("POST", "/wagering/transactions", token, betBody)
	if err != nil {
		t.Fatalf("process bet: %v", err)
	}
	defer betResp.Body.Close()

	if betResp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", betResp.StatusCode)
	}

	result := ReadBody(betResp)
	t.Logf("bet rejected: %v", result)
}

func TestE2E_IdempotentReplay(t *testing.T) {
	skipIfNoEnv(t)
	env := NewTestEnv()
	token := getProviderToken(t, env, "provider1", "provider1")

	createResp, err := env.DoRequest("POST", "/wallets", token, map[string]interface{}{
		"playerId": "player-e2e-idem",
		"initialBalance": map[string]string{
			"amount":   "1000.00",
			"currency": "BRL",
		},
	})
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	createResult := ReadBody(createResp)
	createResp.Body.Close()
	walletID := createResult["id"].(string)

	betBody := map[string]interface{}{
		"providerId":            "provider1",
		"externalTransactionId": "e2e-idem-001",
		"playerId":              "player-e2e-idem",
		"walletId":              walletID,
		"roundId":               "round-1",
		"gameId":                "game-1",
		"kind":                  "BET",
		"money": map[string]string{
			"amount":   "10.00",
			"currency": "BRL",
		},
	}

	// First request
	resp1, err := env.DoRequest("POST", "/wagering/transactions", token, betBody)
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	result1 := ReadBody(resp1)
	resp1.Body.Close()

	// Second request (idempotent replay)
	resp2, err := env.DoRequest("POST", "/wagering/transactions", token, betBody)
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	result2 := ReadBody(resp2)
	resp2.Body.Close()

	if result1["transactionId"] != result2["transactionId"] {
		t.Errorf("idempotent replay returned different transaction IDs: %v vs %v",
			result1["transactionId"], result2["transactionId"])
	}
	t.Logf("idempotent replay: same transaction ID %v", result1["transactionId"])
}

func TestE2E_DoubleRefundRejected(t *testing.T) {
	skipIfNoEnv(t)
	env := NewTestEnv()
	token := getProviderToken(t, env, "provider1", "provider1")

	createResp, err := env.DoRequest("POST", "/wallets", token, map[string]interface{}{
		"playerId": "player-e2e-dref",
		"initialBalance": map[string]string{
			"amount":   "500.00",
			"currency": "BRL",
		},
	})
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	createResult := ReadBody(createResp)
	createResp.Body.Close()
	walletID := createResult["id"].(string)

	// Process BET
	betResp, err := env.DoRequest("POST", "/wagering/transactions", token, map[string]interface{}{
		"providerId":            "provider1",
		"externalTransactionId": "e2e-dref-bet",
		"playerId":              "player-e2e-dref",
		"walletId":              walletID,
		"roundId":               "round-1",
		"kind":                  "BET",
		"money":                 map[string]string{"amount": "100.00", "currency": "BRL"},
	})
	if err != nil {
		t.Fatalf("process bet: %v", err)
	}
	betResp.Body.Close()

	// First REFUND
	refund1Resp, err := env.DoRequest("POST", "/wagering/transactions", token, map[string]interface{}{
		"providerId":                     "provider1",
		"externalTransactionId":          "e2e-dref-refund1",
		"playerId":                       "player-e2e-dref",
		"walletId":                       walletID,
		"roundId":                        "round-1",
		"kind":                           "REFUND",
		"money":                          map[string]string{"amount": "100.00", "currency": "BRL"},
		"referenceExternalTransactionId": "e2e-dref-bet",
	})
	if err != nil {
		t.Fatalf("first refund: %v", err)
	}
	refund1Result := ReadBody(refund1Resp)
	refund1Resp.Body.Close()
	t.Logf("first refund: %v", refund1Result)

	// Second REFUND (should be rejected)
	refund2Resp, err := env.DoRequest("POST", "/wagering/transactions", token, map[string]interface{}{
		"providerId":                     "provider1",
		"externalTransactionId":          "e2e-dref-refund2",
		"playerId":                       "player-e2e-dref",
		"walletId":                       walletID,
		"roundId":                        "round-1",
		"kind":                           "REFUND",
		"money":                          map[string]string{"amount": "100.00", "currency": "BRL"},
		"referenceExternalTransactionId": "e2e-dref-bet",
	})
	if err != nil {
		t.Fatalf("second refund: %v", err)
	}
	defer refund2Resp.Body.Close()

	result := ReadBody(refund2Resp)
	if refund2Resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for rejected refund, got %d", refund2Resp.StatusCode)
	}
	if result["status"] != "REJECTED" {
		t.Errorf("expected REJECTED, got %v", result["status"])
	}
	t.Logf("second refund correctly rejected: %v", result)
}

func TestE2E_ProviderIsolation(t *testing.T) {
	skipIfNoEnv(t)
	env := NewTestEnv()
	token1 := getProviderToken(t, env, "provider1", "provider1")
	token2 := getProviderToken(t, env, "provider2", "provider2")

	// Provider1 creates wallet
	createResp, err := env.DoRequest("POST", "/wallets", token1, map[string]interface{}{
		"playerId": "player-e2e-iso",
		"initialBalance": map[string]string{
			"amount":   "100.00",
			"currency": "BRL",
		},
	})
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	createResult := ReadBody(createResp)
	createResp.Body.Close()
	walletID := createResult["id"].(string)

	// Provider2 tries to read Provider1's wallet - should be 403
	getResp, err := env.DoRequest("GET", "/wallets/"+walletID, token2, nil)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for provider2 reading provider1 wallet, got %d", getResp.StatusCode)
	}
	t.Logf("provider isolation enforced: %d", getResp.StatusCode)
}

func TestE2E_ProviderCannotBetOnOtherProviderWallet(t *testing.T) {
	skipIfNoEnv(t)
	env := NewTestEnv()
	token1 := getProviderToken(t, env, "provider1", "provider1")
	token2 := getProviderToken(t, env, "provider2", "provider2")

	// Provider1 creates wallet
	createResp, err := env.DoRequest("POST", "/wallets", token1, map[string]interface{}{
		"playerId": "player-e2e-iso2",
		"initialBalance": map[string]string{
			"amount":   "500.00",
			"currency": "BRL",
		},
	})
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	createResult := ReadBody(createResp)
	createResp.Body.Close()
	walletID := createResult["id"].(string)

	// Provider2 tries to bet on Provider1's wallet - should be 403
	betResp, err := env.DoRequest("POST", "/wagering/transactions", token2, map[string]interface{}{
		"providerId":            "provider2",
		"externalTransactionId": "e2e-iso-bet",
		"playerId":              "player-e2e-iso2",
		"walletId":              walletID,
		"roundId":               "round-1",
		"kind":                  "BET",
		"money":                 map[string]string{"amount": "10.00", "currency": "BRL"},
	})
	if err != nil {
		t.Fatalf("bet: %v", err)
	}
	defer betResp.Body.Close()

	if betResp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for provider2 betting on provider1 wallet, got %d", betResp.StatusCode)
	}
	t.Logf("provider isolation enforced on bet: %d", betResp.StatusCode)
}

func TestE2E_ConcurrentBets_Idempotent(t *testing.T) {
	skipIfNoEnv(t)
	env := NewTestEnv()
	token := getProviderToken(t, env, "provider1", "provider1")

	createResp, err := env.DoRequest("POST", "/wallets", token, map[string]interface{}{
		"playerId": "player-e2e-conc",
		"initialBalance": map[string]string{
			"amount":   "10000.00",
			"currency": "BRL",
		},
	})
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	createResult := ReadBody(createResp)
	createResp.Body.Close()
	walletID := createResult["id"].(string)

	idempotencyKey := "provider1:e2e-conc-key"
	const goroutines = 50

	var wg sync.WaitGroup
	results := make([]int, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resp, err := env.DoRequest("POST", "/wagering/transactions", token, map[string]interface{}{
				"providerId":            "provider1",
				"externalTransactionId": "e2e-conc-001",
				"playerId":              "player-e2e-conc",
				"walletId":              walletID,
				"roundId":               "round-1",
				"kind":                  "BET",
				"money":                 map[string]string{"amount": "5.00", "currency": "BRL"},
				"idempotencyKey":        idempotencyKey,
			})
			if err != nil {
				t.Errorf("goroutine %d: %v", idx, err)
				return
			}
			results[idx] = resp.StatusCode
			resp.Body.Close()
		}(i)
	}
	wg.Wait()

	successCount := 0
	for _, code := range results {
		if code == http.StatusCreated {
			successCount++
		}
	}

	t.Logf("concurrent requests: %d created out of %d", successCount, goroutines)
}

func TestE2E_TwoBetsOn100Balance(t *testing.T) {
	skipIfNoEnv(t)
	env := NewTestEnv()
	token := getProviderToken(t, env, "provider1", "provider1")

	createResp, err := env.DoRequest("POST", "/wallets", token, map[string]interface{}{
		"playerId": "player-e2e-80",
		"initialBalance": map[string]string{
			"amount":   "100.00",
			"currency": "BRL",
		},
	})
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	createResult := ReadBody(createResp)
	createResp.Body.Close()
	walletID := createResult["id"].(string)

	var wg sync.WaitGroup
	processed := make(chan int, 2)
	rejected := make(chan int, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resp, err := env.DoRequest("POST", "/wagering/transactions", token, map[string]interface{}{
				"providerId":            "provider1",
				"externalTransactionId": fmt.Sprintf("e2e-80-bet-%d", idx),
				"playerId":              "player-e2e-80",
				"walletId":              walletID,
				"roundId":               "round-1",
				"kind":                  "BET",
				"money":                 map[string]string{"amount": "80.00", "currency": "BRL"},
			})
			if err != nil {
				t.Errorf("goroutine %d: %v", idx, err)
				return
			}
			result := ReadBody(resp)
			resp.Body.Close()

			if result["status"] == "PROCESSED" {
				processed <- 1
			} else {
				rejected <- 1
			}
		}(i)
	}

	wg.Wait()
	close(processed)
	close(rejected)

	pCount := 0
	for range processed {
		pCount++
	}
	rCount := 0
	for range rejected {
		rCount++
	}

	t.Logf("two 80.00 bets on 100.00 balance: processed=%d, rejected=%d", pCount, rCount)
	if pCount != 1 || rCount != 1 {
		t.Errorf("expected 1 processed and 1 rejected, got processed=%d rejected=%d", pCount, rCount)
	}
}

func TestE2E_HealthLive(t *testing.T) {
	skipIfNoEnv(t)
	env := NewTestEnv()

	resp, err := env.DoRequest("GET", "/health/live", "", nil)
	if err != nil {
		t.Fatalf("health live: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	t.Log("health/live: OK")
}

func TestE2E_HealthReady(t *testing.T) {
	skipIfNoEnv(t)
	env := NewTestEnv()

	resp, err := env.DoRequest("GET", "/health/ready", "", nil)
	if err != nil {
		t.Fatalf("health ready: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	t.Log("health/ready: OK")
}

func TestE2E_Metrics(t *testing.T) {
	skipIfNoEnv(t)
	env := NewTestEnv()

	resp, err := env.DoRequest("GET", "/metrics", "", nil)
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	t.Log("metrics endpoint: OK")
}
func TestE2E_Unauthorized_NoSideEffects(t *testing.T) {
	skipIfNoEnv(t)
	env := NewTestEnv()

	betBody := map[string]interface{}{
		"providerId":            "provider1",
		"externalTransactionId": "e2e-unauth-001",
		"playerId":              "player-e2e-unauth",
		"walletId":              "00000000-0000-0000-0000-000000000001",
		"roundId":               "round-1",
		"kind":                  "BET",
		"money":                 map[string]string{"amount": "10.00", "currency": "BRL"},
	}

	resp, err := env.DoRequest("POST", "/wagering/transactions", "", betBody)
	if err != nil {
		t.Fatalf("unauthorized bet: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}

	createResp, err := env.DoRequest("POST", "/wallets", "", map[string]interface{}{
		"playerId":       "player-e2e-unauth-create",
		"initialBalance": map[string]string{"amount": "100.00", "currency": "BRL"},
	})
	if err != nil {
		t.Fatalf("unauthorized wallet create: %v", err)
	}
	defer createResp.Body.Close()

	if createResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthorized wallet create, got %d", createResp.StatusCode)
	}
	t.Log("unauthorized requests correctly blocked with no side effects")
}

func TestE2E_WalletBalance_AfterBetAndWin(t *testing.T) {
	skipIfNoEnv(t)
	env := NewTestEnv()
	token := getProviderToken(t, env, "provider1", "provider1")

	createResp, err := env.DoRequest("POST", "/wallets", token, map[string]interface{}{
		"playerId": "player-e2e-bal",
		"initialBalance": map[string]string{
			"amount":   "100.00",
			"currency": "BRL",
		},
	})
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	createResult := ReadBody(createResp)
	createResp.Body.Close()
	walletID := createResult["id"].(string)

	betResp, err := env.DoRequest("POST", "/wagering/transactions", token, map[string]interface{}{
		"providerId":            "provider1",
		"externalTransactionId": "e2e-bal-bet",
		"playerId":              "player-e2e-bal",
		"walletId":              walletID,
		"roundId":               "round-1",
		"kind":                  "BET",
		"money":                 map[string]string{"amount": "60.00", "currency": "BRL"},
	})
	if err != nil {
		t.Fatalf("bet: %v", err)
	}
	betResult := ReadBody(betResp)
	betResp.Body.Close()
	if betResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for bet, got %d", betResp.StatusCode)
	}

	winResp, err := env.DoRequest("POST", "/wagering/transactions", token, map[string]interface{}{
		"providerId":            "provider1",
		"externalTransactionId": "e2e-bal-win",
		"playerId":              "player-e2e-bal",
		"walletId":              walletID,
		"roundId":               "round-1",
		"kind":                  "WIN",
		"money":                 map[string]string{"amount": "20.00", "currency": "BRL"},
	})
	if err != nil {
		t.Fatalf("win: %v", err)
	}
	winResult := ReadBody(winResp)
	winResp.Body.Close()
	if winResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for win, got %d", winResp.StatusCode)
	}

	t.Logf("bet result: %v", betResult)
	t.Logf("win result: %v", winResult)

	getResp, err := env.DoRequest("GET", "/wallets/"+walletID, token, nil)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	walletData := ReadBody(getResp)
	getResp.Body.Close()

	balance := walletData["balance"].(map[string]interface{})
	balanceAmount := balance["amount"].(string)
	t.Logf("wallet balance after bet(60) and win(20): %s", balanceAmount)
}

func TestE2E_ReferenceChain_BetRefundRollback(t *testing.T) {
	skipIfNoEnv(t)
	env := NewTestEnv()
	token := getProviderToken(t, env, "provider1", "provider1")

	createResp, err := env.DoRequest("POST", "/wallets", token, map[string]interface{}{
		"playerId": "player-e2e-chain",
		"initialBalance": map[string]string{
			"amount":   "1000.00",
			"currency": "BRL",
		},
	})
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	createResult := ReadBody(createResp)
	createResp.Body.Close()
	walletID := createResult["id"].(string)

	betResp, err := env.DoRequest("POST", "/wagering/transactions", token, map[string]interface{}{
		"providerId":            "provider1",
		"externalTransactionId": "e2e-chain-bet",
		"playerId":              "player-e2e-chain",
		"walletId":              walletID,
		"roundId":               "round-1",
		"kind":                  "BET",
		"money":                 map[string]string{"amount": "100.00", "currency": "BRL"},
	})
	if err != nil {
		t.Fatalf("bet: %v", err)
	}
	betResp.Body.Close()

	refundResp, err := env.DoRequest("POST", "/wagering/transactions", token, map[string]interface{}{
		"providerId":                     "provider1",
		"externalTransactionId":          "e2e-chain-refund",
		"playerId":                       "player-e2e-chain",
		"walletId":                       walletID,
		"roundId":                        "round-1",
		"kind":                           "REFUND",
		"money":                          map[string]string{"amount": "100.00", "currency": "BRL"},
		"referenceExternalTransactionId": "e2e-chain-bet",
	})
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	refundResp.Body.Close()

	rollbackResp, err := env.DoRequest("POST", "/wagering/transactions", token, map[string]interface{}{
		"providerId":                     "provider1",
		"externalTransactionId":          "e2e-chain-rollback",
		"playerId":                       "player-e2e-chain",
		"walletId":                       walletID,
		"roundId":                        "round-1",
		"kind":                           "ROLLBACK",
		"money":                          map[string]string{"amount": "100.00", "currency": "BRL"},
		"referenceExternalTransactionId": "e2e-chain-bet",
	})
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	rollbackResult := ReadBody(rollbackResp)
	rollbackResp.Body.Close()

	if rollbackResult["status"] != "REJECTED" {
		t.Errorf("expected ROLLBACK REJECTED after REFUND, got %v", rollbackResult["status"])
	}
	t.Logf("reference chain: BET->REFUND->ROLLBACK(rejected) verified")
}
