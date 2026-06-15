// Package pioneer implements ProviderCapabilities for pioneer.ai.
package pioneer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/taciturnaxolotl/potluck/internal/pool"
)

func init() {
	pool.RegisterProvider("openai_compat", &Pioneer{})
}

// Pioneer implements pool.ProviderCapabilities for pioneer.ai keys.
type Pioneer struct{}

func (Pioneer) HealthChecker() pool.HealthChecker    { return pool.PioneerHealthChecker{} }
func (Pioneer) BillingIngestor() pool.BillingIngestor { return pool.PioneerBillingIngestor{} }
func (Pioneer) ModelFetcher() pool.ModelFetcher       { return pool.PioneerModelFetcher{} }

func (Pioneer) AcceptedPlan(plan string) bool {
	switch plan {
	case "pro", "pro_legacy", "partner":
		return true
	default:
		return false
	}
}

func (Pioneer) ValidateKey(ctx context.Context, httpClient *http.Client, baseURL, apiKey string) (*pool.KeyValidation, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	do := func(url string) ([]byte, int, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return b, resp.StatusCode, nil
	}

	tsURL := baseURL + "/billing/usage/timeseries?full_history=false&interval_minutes=1440"
	body, status, err := do(tsURL)
	if err != nil {
		return nil, fmt.Errorf("billing request failed: %w", err)
	}
	switch status {
	case http.StatusUnauthorized:
		return &pool.KeyValidation{
			PendingReason: "pioneer returned 401 — key may be exhausted or not yet active; we'll retry automatically",
		}, nil
	case http.StatusServiceUnavailable:
		return &pool.KeyValidation{
			PendingReason: "pioneer auth service is temporarily down; we'll retry automatically",
		}, nil
	}
	if status/100 != 2 {
		return nil, fmt.Errorf("pioneer returned HTTP %d during validation", status)
	}

	var tsBody struct {
		Points []struct {
			BucketDate   string  `json:"bucket_date"`
			TotalCredits float64 `json:"total_credits"`
		} `json:"points"`
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&tsBody); err != nil {
		return nil, fmt.Errorf("could not decode billing response: %w", err)
	}

	body2, status2, err2 := do(baseURL + "/billing/plan-info")
	body3, status3, err3 := do(baseURL + "/billing/billing-status")

	var planInfo struct {
		PaymentPlan      string  `json:"payment_plan"`
		CreditLimit      float64 `json:"credit_limit"`
		RemainingCredits float64 `json:"remaining_credits"`
	}
	if err2 == nil && status2 == http.StatusOK {
		_ = json.NewDecoder(bytes.NewReader(body2)).Decode(&planInfo)
	}

	var billingStatus struct {
		TeamID string `json:"team_id"`
	}
	if err3 == nil && status3 == http.StatusOK {
		_ = json.NewDecoder(bytes.NewReader(body3)).Decode(&billingStatus)
	}

	todayUTC := time.Now().UTC().Format("2006-01-02")
	var todayMicros int64
	for _, p := range tsBody.Points {
		if p.BucketDate == todayUTC {
			todayMicros = int64(p.TotalCredits * 10_000)
			break
		}
	}

	val := &pool.KeyValidation{
		Valid:             true,
		CreditLimitMicros: int64(planInfo.CreditLimit * 10_000),
		RemainingMicros:   int64(planInfo.RemainingCredits * 10_000),
		TodayMicros:       todayMicros,
		PaymentPlan:       planInfo.PaymentPlan,
		TeamID:            billingStatus.TeamID,
	}

	if planInfo.PaymentPlan != "" && !AcceptedPlan(planInfo.PaymentPlan) {
		return nil, fmt.Errorf("unsupported pioneer plan %q (accepted: pro, pro_legacy, partner)", planInfo.PaymentPlan)
	}

	return val, nil
}

func AcceptedPlan(plan string) bool {
	switch plan {
	case "pro", "pro_legacy", "partner":
		return true
	default:
		return false
	}
}
