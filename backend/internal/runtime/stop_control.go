package runtime

import (
	"context"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

type GracefulStopController struct {
	ctx    context.Context
	cancel context.CancelFunc
	stops  atomic.Int32
}

func NewGracefulStopController(parent context.Context) *GracefulStopController {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	return &GracefulStopController{ctx: ctx, cancel: cancel}
}

func InstallSignalHandlers(parent context.Context) *GracefulStopController {
	controller := NewGracefulStopController(parent)
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer signal.Stop(ch)
		for range ch {
			controller.Stop()
			if controller.StopCount() >= 2 {
				os.Exit(130)
			}
		}
	}()
	return controller
}

func (c *GracefulStopController) Context() context.Context { return c.ctx }
func (c *GracefulStopController) Stop()                    { c.stops.Add(1); c.cancel() }
func (c *GracefulStopController) StopCount() int           { return int(c.stops.Load()) }
func (c *GracefulStopController) Stopping() bool           { return c.ctx.Err() != nil }

func SleepWithStop(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
