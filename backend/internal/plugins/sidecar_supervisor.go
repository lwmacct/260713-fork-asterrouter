package plugins

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	sidecarSupervisorStarting    = "starting"
	sidecarSupervisorRunning     = "running"
	sidecarSupervisorBackingOff  = "backing_off"
	sidecarSupervisorStopped     = "stopped"
	sidecarSupervisorDisabled    = "disabled"
	sidecarSupervisorUninstalled = "uninstalled"
	sidecarSupervisorFailed      = "failed"

	sidecarRestartInitialDelay = time.Second
	sidecarRestartMaxDelay     = 30 * time.Second
)

type sidecarSupervisorSnapshot struct {
	Phase         string
	Version       string
	Endpoint      string
	RestartCount  int
	LastStartedAt *time.Time
	LastExitedAt  *time.Time
	NextRestartAt *time.Time
	LastError     string
}

type sidecarSupervisor struct {
	service       *Service
	pluginID      string
	targetVersion string
	ctx           context.Context
	cancel        context.CancelFunc
	wake          chan struct{}
	done          chan struct{}

	mu            sync.RWMutex
	phase         string
	version       string
	endpoint      string
	restartCount  int
	lastStartedAt *time.Time
	lastExitedAt  *time.Time
	nextRestartAt *time.Time
	lastError     string
	current       *sidecarProcess
}

func newSidecarSupervisor(service *Service, pluginID string, targetVersion string) *sidecarSupervisor {
	ctx, cancel := context.WithCancel(context.Background())
	return &sidecarSupervisor{
		service:       service,
		pluginID:      pluginID,
		targetVersion: targetVersion,
		ctx:           ctx,
		cancel:        cancel,
		wake:          make(chan struct{}, 1),
		done:          make(chan struct{}),
		phase:         sidecarSupervisorStarting,
		version:       targetVersion,
	}
}

func (s *Service) StartEnabledSidecars(ctx context.Context) error {
	plugins, err := s.repo.ListPlugins(ctx)
	if err != nil {
		return err
	}
	for _, plugin := range plugins {
		if plugin.Status != StatusEnabled {
			continue
		}
		_, state, err := s.sidecarTargetState(ctx, plugin.ID)
		if err != nil || state != sidecarTargetReady {
			continue
		}
		if err := s.ensureSidecarSupervisor(ctx, plugin.ID); err != nil {
			continue
		}
	}
	return nil
}

func (s *Service) ensureSidecarSupervisor(ctx context.Context, pluginID string) error {
	installation, state, err := s.sidecarTargetState(ctx, pluginID)
	if err != nil {
		return err
	}
	switch state {
	case sidecarTargetReady:
	case sidecarTargetDisabled:
		return ErrPluginDisabled
	case sidecarTargetUninstalled:
		return ErrPackageNotInstalled
	case sidecarTargetNotSidecar:
		return fmt.Errorf("%w: plugin %s is not a sidecar runtime", ErrPluginRuntimeUnavailable, pluginID)
	default:
		return ErrPluginRuntimeUnavailable
	}

	s.sidecarsMu.Lock()
	supervisor := s.supervisors[pluginID]
	if supervisor == nil {
		supervisor = newSidecarSupervisor(s, pluginID, installation.Version)
		s.supervisors[pluginID] = supervisor
		s.sidecarsMu.Unlock()
		supervisor.start()
		return nil
	}
	s.sidecarsMu.Unlock()

	supervisor.setTargetVersion(installation.Version)
	supervisor.wakeNow()
	return nil
}

func (s *Service) stopSidecarSupervisor(ctx context.Context, pluginID string) error {
	s.sidecarsMu.Lock()
	supervisor := s.supervisors[pluginID]
	delete(s.supervisors, pluginID)
	proc := s.sidecars[pluginID]
	delete(s.sidecars, pluginID)
	s.sidecarsMu.Unlock()

	var stopErr error
	if supervisor != nil {
		stopErr = supervisor.stop(ctx)
	}
	if proc != nil {
		if err := proc.stop(ctx); err != nil && stopErr == nil {
			stopErr = err
		}
	}
	return stopErr
}

func (s *Service) wakeSidecarSupervisor(pluginID string) {
	s.sidecarsMu.Lock()
	supervisor := s.supervisors[pluginID]
	s.sidecarsMu.Unlock()
	if supervisor != nil {
		supervisor.wakeNow()
	}
}

func (s *Service) removeSidecarProcess(pluginID string, proc *sidecarProcess) {
	if proc == nil {
		return
	}
	s.sidecarsMu.Lock()
	if s.sidecars[pluginID] == proc {
		delete(s.sidecars, pluginID)
	}
	s.sidecarsMu.Unlock()
}

func (s *Service) publishSidecarProcess(pluginID string, proc *sidecarProcess) {
	var stale *sidecarProcess
	s.sidecarsMu.Lock()
	if existing := s.sidecars[pluginID]; existing != nil && existing != proc {
		stale = existing
	}
	s.sidecars[pluginID] = proc
	s.sidecarsMu.Unlock()
	if stale != nil {
		_ = stale.stop(context.Background())
	}
}

func (sup *sidecarSupervisor) start() {
	go sup.run()
}

func (sup *sidecarSupervisor) stop(ctx context.Context) error {
	sup.cancel()
	sup.wakeNow()
	stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stopErr := sup.stopCurrent(stopCtx, sidecarSupervisorStopped)
	select {
	case <-sup.done:
		return stopErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (sup *sidecarSupervisor) run() {
	defer close(sup.done)
	defer sup.setStopped()

	consecutiveFailures := 0
	for {
		select {
		case <-sup.ctx.Done():
			_ = sup.stopCurrent(context.Background(), sidecarSupervisorStopped)
			return
		default:
		}

		installation, state, err := sup.service.sidecarTargetState(sup.ctx, sup.pluginID)
		if err != nil {
			consecutiveFailures++
			if !sup.backoff(sidecarSupervisorFailed, err, consecutiveFailures) {
				return
			}
			continue
		}
		if state != sidecarTargetReady {
			consecutiveFailures = 0
			phase := sidecarSupervisorUninstalled
			if state == sidecarTargetDisabled {
				phase = sidecarSupervisorDisabled
			}
			_ = sup.stopCurrent(context.Background(), phase)
			if !sup.waitForWake() {
				return
			}
			continue
		}

		sup.setTargetVersion(installation.Version)
		if proc := sup.currentProcess(); proc != nil && proc.running() && proc.Version == installation.Version {
			sup.setRunning(proc)
			if !sup.waitRunning(proc) {
				return
			}
			if proc.running() {
				continue
			}
			if !sup.isCurrentProcess(proc) {
				consecutiveFailures = 0
				continue
			}
			consecutiveFailures++
			sup.forgetCurrentProcess(proc, proc.exitError())
			if !sup.backoff(sidecarSupervisorBackingOff, proc.exitError(), consecutiveFailures) {
				return
			}
			continue
		}

		_ = sup.stopCurrent(context.Background(), sidecarSupervisorStarting)
		sup.setStarting(installation.Version)
		proc, err := sup.service.startSidecar(sup.ctx, installation)
		if err != nil {
			consecutiveFailures++
			if !sup.backoff(sidecarSupervisorBackingOff, err, consecutiveFailures) {
				return
			}
			continue
		}
		consecutiveFailures = 0
		sup.attachProcess(proc)
		sup.service.publishSidecarProcess(sup.pluginID, proc)
	}
}

func (sup *sidecarSupervisor) waitRunning(proc *sidecarProcess) bool {
	select {
	case <-sup.ctx.Done():
		_ = sup.stopCurrent(context.Background(), sidecarSupervisorStopped)
		return false
	case <-sup.wake:
		return true
	case <-proc.done:
		return true
	}
}

func (sup *sidecarSupervisor) waitForWake() bool {
	select {
	case <-sup.ctx.Done():
		return false
	case <-sup.wake:
		return true
	}
}

func (sup *sidecarSupervisor) backoff(phase string, err error, failures int) bool {
	delay := sidecarRestartDelay(failures)
	next := sup.service.now().UTC().Add(delay)
	sup.mu.Lock()
	sup.phase = phase
	sup.restartCount++
	sup.nextRestartAt = cloneTimePtr(&next)
	sup.lastError = errorString(err)
	sup.mu.Unlock()

	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-sup.ctx.Done():
		return false
	case <-sup.wake:
		return true
	case <-timer.C:
		return true
	}
}

func sidecarRestartDelay(failures int) time.Duration {
	if failures <= 1 {
		return sidecarRestartInitialDelay
	}
	delay := sidecarRestartInitialDelay
	for i := 1; i < failures; i++ {
		if delay >= sidecarRestartMaxDelay/2 {
			return sidecarRestartMaxDelay
		}
		delay *= 2
	}
	if delay > sidecarRestartMaxDelay {
		return sidecarRestartMaxDelay
	}
	return delay
}

func (sup *sidecarSupervisor) attachProcess(proc *sidecarProcess) {
	now := sup.service.now().UTC()
	sup.mu.Lock()
	sup.phase = sidecarSupervisorRunning
	sup.version = proc.Version
	sup.endpoint = proc.Endpoint
	sup.lastStartedAt = cloneTimePtr(&now)
	sup.nextRestartAt = nil
	sup.lastError = ""
	sup.current = proc
	sup.mu.Unlock()
}

func (sup *sidecarSupervisor) forgetCurrentProcess(proc *sidecarProcess, err error) bool {
	now := sup.service.now().UTC()
	sup.mu.Lock()
	if sup.current != proc {
		sup.mu.Unlock()
		return false
	}
	sup.current = nil
	sup.endpoint = ""
	sup.lastExitedAt = cloneTimePtr(&now)
	sup.lastError = errorString(err)
	sup.mu.Unlock()
	sup.service.removeSidecarProcess(sup.pluginID, proc)
	return true
}

func (sup *sidecarSupervisor) stopCurrent(ctx context.Context, phase string) error {
	now := sup.service.now().UTC()
	sup.mu.Lock()
	proc := sup.current
	sup.current = nil
	sup.endpoint = ""
	sup.phase = phase
	sup.nextRestartAt = nil
	if proc != nil {
		sup.lastExitedAt = cloneTimePtr(&now)
	}
	if phase == sidecarSupervisorDisabled || phase == sidecarSupervisorUninstalled || phase == sidecarSupervisorStopped {
		sup.lastError = ""
	}
	sup.mu.Unlock()
	if proc == nil {
		return nil
	}
	sup.service.removeSidecarProcess(sup.pluginID, proc)
	return proc.stop(ctx)
}

func (sup *sidecarSupervisor) currentProcess() *sidecarProcess {
	sup.mu.RLock()
	defer sup.mu.RUnlock()
	return sup.current
}

func (sup *sidecarSupervisor) isCurrentProcess(proc *sidecarProcess) bool {
	sup.mu.RLock()
	defer sup.mu.RUnlock()
	return sup.current == proc
}

func (sup *sidecarSupervisor) setTargetVersion(version string) {
	sup.mu.Lock()
	sup.targetVersion = version
	if sup.version == "" {
		sup.version = version
	}
	sup.mu.Unlock()
}

func (sup *sidecarSupervisor) setStarting(version string) {
	sup.mu.Lock()
	sup.phase = sidecarSupervisorStarting
	sup.version = version
	sup.endpoint = ""
	sup.nextRestartAt = nil
	sup.lastError = ""
	sup.mu.Unlock()
}

func (sup *sidecarSupervisor) setRunning(proc *sidecarProcess) {
	sup.mu.Lock()
	sup.phase = sidecarSupervisorRunning
	sup.version = proc.Version
	sup.endpoint = proc.Endpoint
	sup.nextRestartAt = nil
	sup.mu.Unlock()
}

func (sup *sidecarSupervisor) setStopped() {
	sup.mu.Lock()
	sup.phase = sidecarSupervisorStopped
	sup.current = nil
	sup.endpoint = ""
	sup.nextRestartAt = nil
	sup.mu.Unlock()
}

func (sup *sidecarSupervisor) wakeNow() {
	select {
	case sup.wake <- struct{}{}:
	default:
	}
}

func (sup *sidecarSupervisor) snapshot() sidecarSupervisorSnapshot {
	sup.mu.RLock()
	defer sup.mu.RUnlock()
	return sidecarSupervisorSnapshot{
		Phase:         sup.phase,
		Version:       defaultString(sup.version, sup.targetVersion),
		Endpoint:      sup.endpoint,
		RestartCount:  sup.restartCount,
		LastStartedAt: cloneTimePtr(sup.lastStartedAt),
		LastExitedAt:  cloneTimePtr(sup.lastExitedAt),
		NextRestartAt: cloneTimePtr(sup.nextRestartAt),
		LastError:     sup.lastError,
	}
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
