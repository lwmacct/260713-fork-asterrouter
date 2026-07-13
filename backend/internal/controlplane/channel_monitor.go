package controlplane

import (
	"context"
	"time"
)

// ChannelMonitorConfig is reloaded before every cycle so settings changes do
// not require a process restart.
type ChannelMonitorConfig struct {
	Enabled  bool
	Interval time.Duration
}

func (s *Service) RunChannelMonitor(ctx context.Context, loadConfig func(context.Context) (ChannelMonitorConfig, error), report func(string, error)) {
	const disabledPollInterval = 30 * time.Second
	for {
		config, err := loadConfig(ctx)
		if err != nil {
			report("load channel monitor settings", err)
			config.Interval = disabledPollInterval
		} else if config.Enabled {
			s.runChannelMonitorCycle(ctx, report)
		}

		interval := config.Interval
		if interval < 30*time.Second {
			interval = disabledPollInterval
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

func (s *Service) runChannelMonitorCycle(ctx context.Context, report func(string, error)) {
	providers, err := s.ListProviders(ctx)
	if err != nil {
		report("list providers for channel monitor", err)
		return
	}
	for _, provider := range providers {
		if provider.Status != ProviderStatusActive {
			continue
		}
		if _, err := s.CheckProvider(ctx, systemActor, provider.ID); err != nil {
			report("check provider "+provider.ID, err)
		}
	}

	accounts, err := s.ListProviderAccounts(ctx)
	if err != nil {
		report("list provider accounts for channel monitor", err)
		return
	}
	for _, account := range accounts {
		if account.Status != AccountStatusActive {
			continue
		}
		if _, err := s.CheckProviderAccount(ctx, systemActor, account.ID); err != nil {
			report("check provider account "+account.ID, err)
		}
	}
}
