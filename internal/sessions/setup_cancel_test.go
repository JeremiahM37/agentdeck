package sessions

import (
	"context"
	"testing"
)

func TestCancellationAndAgentLaunchHaveOneWinner(t *testing.T) {
	for _, order := range []string{"cancel-first", "launch-first", "concurrent"} {
		t.Run(order, func(t *testing.T) {
			m, row := pollRig(t)
			if err := m.DB.Update("sessions", row.ID, map[string]any{"setup_state": "creating"}); err != nil {
				t.Fatal(err)
			}
			m.activeSetups = map[int64]bool{row.ID: true}
			var launchErr, cancelErr error
			switch order {
			case "cancel-first":
				cancelErr = m.CancelSetup(context.Background(), row.ID)
				launchErr = m.beginSetupAgent(row.ID)
			case "launch-first":
				launchErr = m.beginSetupAgent(row.ID)
				cancelErr = m.CancelSetup(context.Background(), row.ID)
			default:
				start := make(chan struct{})
				launch := make(chan error, 1)
				cancel := make(chan error, 1)
				go func() { <-start; launch <- m.beginSetupAgent(row.ID) }()
				go func() { <-start; cancel <- m.CancelSetup(context.Background(), row.ID) }()
				close(start)
				launchErr = <-launch
				cancelErr = <-cancel
			}
			current, err := m.DB.Session(row.ID)
			if err != nil {
				t.Fatal(err)
			}
			if (launchErr == nil) == (cancelErr == nil) {
				t.Fatalf("launch and cancellation were not exclusive: %v / %v", launchErr, cancelErr)
			}
			if current.SetupCancelRequested != (cancelErr == nil) {
				t.Fatal("accepted cancellation was not durable")
			}
			if current.SetupCancelRequested {
				// An empty pre-checkout reservation has no target process to contact; its
				// durable flag must still stop both checkout and later agent startup.
				if m.checkSetupCancellation(row.ID) == nil || m.beginSetupAgent(row.ID) == nil {
					t.Fatal("accepted cancellation allowed subsequent setup")
				}
			}
		})
	}
}
