package tests

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"github.com/debaucheryparty/packets/internal/provider"
	"github.com/debaucheryparty/packets/pkg/apitypes"
)

func TestProvider_Capabilities(t *testing.T) {
	gh := provider.NewGitHubActions(nil, "token", "owner/repo")
	cci := provider.NewCircleCI(nil, "token", "gh/owner/repo")

	ghCaps := gh.Capabilities()
	if !ghCaps.SupportsWebhooks {
		t.Errorf("expected GitHubActions to support webhooks")
	}

	cciCaps := cci.Capabilities()
	if cciCaps.SupportsWebhooks {
		t.Errorf("expected CircleCI to not support webhooks in current configuration")
	}
	if !cciCaps.SupportsArtifacts {
		t.Errorf("expected CircleCI to support artifacts")
	}
}

func TestProvider_GitHubWebhookVerification(t *testing.T) {
	secret := "webhook-secret-xyz"
	payload := []byte(`{"action":"completed","workflow_run":{"id":123,"status":"completed","conclusion":"success"}}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	validSig := fmt.Sprintf("sha256=%s", hex.EncodeToString(mac.Sum(nil)))

	if !provider.VerifyGitHubWebhookSignature(payload, validSig, secret) {
		t.Errorf("expected valid signature to pass verification")
	}

	tamperedSig := "sha256=0000000000000000000000000000000000000000000000000000000000000000"
	if provider.VerifyGitHubWebhookSignature(payload, tamperedSig, secret) {
		t.Errorf("expected tampered signature to fail verification")
	}

	tamperedPayload := []byte(`{"action":"completed","workflow_run":{"id":123,"status":"completed","conclusion":"failure"}}`)
	if provider.VerifyGitHubWebhookSignature(tamperedPayload, validSig, secret) {
		t.Errorf("expected tampered payload to fail verification")
	}

	if provider.VerifyGitHubWebhookSignature(payload, "invalid-prefix", secret) {
		t.Errorf("expected signature without sha256= prefix to fail verification")
	}
}

func TestProvider_NormalizeWebhookState(t *testing.T) {
	tests := []struct {
		status     string
		conclusion string
		want       apitypes.JobState
	}{
		{"completed", "success", apitypes.JobStateSucceeded},
		{"completed", "failure", apitypes.JobStateFailed},
		{"completed", "cancelled", apitypes.JobStateFailed},
		{"in_progress", "", apitypes.JobStateRunning},
		{"queued", "", apitypes.JobStateDispatched},
		{"unknown", "", apitypes.JobStatePending},
	}

	for _, tt := range tests {
		got := provider.NormalizeWebhookState(tt.status, tt.conclusion)
		if got != tt.want {
			t.Errorf("NormalizeWebhookState(%q, %q) = %v, want %v", tt.status, tt.conclusion, got, tt.want)
		}
	}
}

func TestProvider_CircleCI_EmptyConfig(t *testing.T) {
	ctx := context.Background()
	cci := provider.NewCircleCI(nil, "", "")

	_, err := cci.Dispatch(ctx, apitypes.Job{ID: "job-cci"})
	if !errors.Is(err, provider.ErrProviderExhausted) {
		t.Errorf("expected ErrProviderExhausted on Dispatch, got %v", err)
	}

	st, err := cci.Status(ctx, "job-cci")
	if err != nil || st != apitypes.JobStateSucceeded {
		t.Errorf("expected Status JobStateSucceeded, got %v, err=%v", st, err)
	}

	_, err = cci.FetchArtifact(ctx, "job-cci")
	if !errors.Is(err, provider.ErrProviderExhausted) {
		t.Errorf("expected ErrProviderExhausted on FetchArtifact, got %v", err)
	}
}
