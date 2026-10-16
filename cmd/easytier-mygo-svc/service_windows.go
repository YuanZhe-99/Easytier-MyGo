package main

import (
	"context"

	"golang.org/x/sys/windows/svc"
)

// runService runs the daemon under the Windows service manager when it
// started us; ok is false when running from a console.
func runService(run func(ctx context.Context) error) (ok bool, err error) {
	if is, _ := svc.IsWindowsService(); !is {
		return false, nil
	}
	return true, svc.Run("EasyTierMyGoHelper", handler{run})
}

type handler struct {
	run func(ctx context.Context) error
}

// Execute reports the service running, and stops the daemon when the
// service manager asks.
func (h handler) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.run(ctx) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-done:
			cancel()
			if err != nil {
				return false, 1
			}
			return false, 0
		case r := <-req:
			switch r.Cmd {
			case svc.Interrogate:
				status <- r.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				return false, 0
			}
		}
	}
}
