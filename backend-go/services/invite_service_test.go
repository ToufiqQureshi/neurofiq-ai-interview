package services

import (
	"testing"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

func TestInviteRedeemable(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	revoked := now.Add(-time.Hour)

	cases := []struct {
		name   string
		invite models.InterviewInvite
		want   bool
	}{
		{"fresh single-use", models.InterviewInvite{MaxUses: 1, Uses: 0, ExpiresAt: now.Add(time.Hour)}, true},
		{"already used", models.InterviewInvite{MaxUses: 1, Uses: 1, ExpiresAt: now.Add(time.Hour)}, false},
		{"expired", models.InterviewInvite{MaxUses: 1, Uses: 0, ExpiresAt: now.Add(-time.Hour)}, false},
		{"revoked", models.InterviewInvite{MaxUses: 1, Uses: 0, ExpiresAt: now.Add(time.Hour), RevokedAt: &revoked}, false},
		{"unlimited uses", models.InterviewInvite{MaxUses: 0, Uses: 99, ExpiresAt: now.Add(time.Hour)}, true},
		{"multi-use with room", models.InterviewInvite{MaxUses: 10, Uses: 3, ExpiresAt: now.Add(time.Hour)}, true},
	}
	for _, tc := range cases {
		if got := tc.invite.Redeemable(now); got != tc.want {
			t.Errorf("%s: Redeemable = %v, want %v", tc.name, got, tc.want)
		}
	}
}
