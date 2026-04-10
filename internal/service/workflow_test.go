package service

import "testing"

func TestApplyOrderEvent(t *testing.T) {
	tests := []struct {
		name      string
		from      OrderState
		event     OrderEvent
		want      OrderState
		wantError bool
	}{
		{name: "detected to detail fetching", from: StateDetected, event: EventOrderIngested, want: StateDetailFetching},
		{name: "detail fetching to retrying", from: StateDetailFetching, event: EventDetailFetchRetryable, want: StateRetryingDetail},
		{name: "detail ready to payment pending", from: StateDetailReady, event: EventHandoffToPayment, want: StatePaymentPendingExternal},
		{name: "terminal state rejected", from: StateTimedOut, event: EventOrderExpired, want: StateTimedOut, wantError: true},
		{name: "invalid transition rejected", from: StateDetected, event: EventDetailFetchOK, want: StateDetected, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyOrderEvent(tt.from, tt.event)
			if tt.wantError {
				if err == nil {
					t.Fatalf("ApplyOrderEvent(%q, %q) expected error", tt.from, tt.event)
				}
				return
			}

			if err != nil {
				t.Fatalf("ApplyOrderEvent(%q, %q) unexpected error: %v", tt.from, tt.event, err)
			}

			if got != tt.want {
				t.Fatalf("ApplyOrderEvent(%q, %q) = %q, want %q", tt.from, tt.event, got, tt.want)
			}
		})
	}
}
