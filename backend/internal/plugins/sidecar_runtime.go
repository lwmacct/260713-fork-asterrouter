package plugins

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrPluginRuntimeUnavailable = errors.New("plugin runtime is unavailable")
	ErrPluginDisabled           = errors.New("plugin is not enabled")
)

type sidecarTargetState string

const (
	sidecarTargetReady       sidecarTargetState = "ready"
	sidecarTargetMissing     sidecarTargetState = "missing"
	sidecarTargetDisabled    sidecarTargetState = "disabled"
	sidecarTargetUninstalled sidecarTargetState = "uninstalled"
	sidecarTargetNotSidecar  sidecarTargetState = "not_sidecar"
)

type SidecarRuntimeStatus struct {
	PluginID        string     `json:"plugin_id"`
	Enabled         bool       `json:"enabled"`
	Installed       bool       `json:"installed"`
	Running         bool       `json:"running"`
	Supervised      bool       `json:"supervised"`
	Version         string     `json:"version,omitempty"`
	Endpoint        string     `json:"endpoint,omitempty"`
	SupervisorState string     `json:"supervisor_state,omitempty"`
	RestartCount    int        `json:"restart_count,omitempty"`
	LastStartedAt   *time.Time `json:"last_started_at,omitempty"`
	LastExitedAt    *time.Time `json:"last_exited_at,omitempty"`
	NextRestartAt   *time.Time `json:"next_restart_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	Error           string     `json:"error,omitempty"`
}

type sidecarProcess struct {
	PluginID string
	Version  string
	Endpoint string
	Token    string
	Command  *exec.Cmd
	done     chan struct{}
	exitMu   sync.RWMutex
	exitErr  error
}

func (s *Service) SidecarRuntimeStatus(ctx context.Context, pluginID string) (SidecarRuntimeStatus, error) {
	pluginID = strings.TrimSpace(pluginID)
	status := SidecarRuntimeStatus{PluginID: pluginID}
	plugin, ok, err := s.repo.FindPlugin(ctx, pluginID)
	if err != nil {
		return status, err
	}
	if !ok {
		status.Error = ErrPluginNotFound.Error()
		return status, nil
	}
	status.Enabled = plugin.Status == StatusEnabled
	installation, ok, err := s.repo.FindPackageInstallation(ctx, pluginID)
	if err != nil {
		return status, err
	}
	if ok && installation.Status == PackageInstallInstalled {
		status.Installed = true
		status.Version = installation.Version
	}

	var supervisor *sidecarSupervisor
	s.sidecarsMu.Lock()
	if proc := s.sidecars[pluginID]; proc != nil && proc.running() {
		status.Running = true
		status.Endpoint = proc.Endpoint
	}
	supervisor = s.supervisors[pluginID]
	s.sidecarsMu.Unlock()
	if supervisor != nil {
		snapshot := supervisor.snapshot()
		status.Supervised = true
		status.SupervisorState = snapshot.Phase
		status.RestartCount = snapshot.RestartCount
		status.LastStartedAt = snapshot.LastStartedAt
		status.LastExitedAt = snapshot.LastExitedAt
		status.NextRestartAt = snapshot.NextRestartAt
		status.LastError = snapshot.LastError
		if status.Endpoint == "" {
			status.Endpoint = snapshot.Endpoint
		}
		if status.Version == "" {
			status.Version = snapshot.Version
		}
	}
	return status, nil
}

func (s *Service) ProxySidecarHTTP(ctx context.Context, pluginID string, proxyPath string, source *http.Request) (*http.Response, error) {
	if err := s.ensureSidecarSupervisor(ctx, pluginID); err != nil {
		return nil, err
	}
	proc, err := s.waitSidecar(ctx, pluginID, 6*time.Second)
	if err != nil {
		return nil, err
	}
	targetURL, err := sidecarProxyURL(proc.Endpoint, proxyPath, source.URL.RawQuery)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, source.Method, targetURL, source.Body)
	if err != nil {
		return nil, err
	}
	copyProxyRequestHeaders(req.Header, source.Header)
	req.Header.Set("Authorization", "Bearer "+proc.Token)
	response, err := s.httpClient.Do(req)
	if err != nil {
		s.removeSidecarProcess(pluginID, proc)
		_ = proc.stop(context.Background())
		s.wakeSidecarSupervisor(pluginID)
		return nil, fmt.Errorf("%w: %v", ErrPluginRuntimeUnavailable, err)
	}
	return response, nil
}

func (s *Service) sidecarTarget(ctx context.Context, pluginID string) (packageInstallationRecord, bool, error) {
	installation, state, err := s.sidecarTargetState(ctx, pluginID)
	if err != nil {
		return packageInstallationRecord{}, false, err
	}
	return installation, state == sidecarTargetReady, nil
}

func (s *Service) sidecarTargetState(ctx context.Context, pluginID string) (packageInstallationRecord, sidecarTargetState, error) {
	pluginID = strings.TrimSpace(pluginID)
	plugin, ok, err := s.repo.FindPlugin(ctx, pluginID)
	if err != nil {
		return packageInstallationRecord{}, sidecarTargetMissing, err
	}
	if !ok {
		return packageInstallationRecord{}, sidecarTargetMissing, ErrPluginNotFound
	}
	if plugin.Status != StatusEnabled {
		return packageInstallationRecord{}, sidecarTargetDisabled, nil
	}
	installation, ok, err := s.repo.FindPackageInstallation(ctx, pluginID)
	if err != nil {
		return packageInstallationRecord{}, sidecarTargetUninstalled, err
	}
	if !ok || installation.Status != PackageInstallInstalled {
		return packageInstallationRecord{}, sidecarTargetUninstalled, nil
	}
	manifest, err := readSidecarManifest(filepath.Join(s.activePackageDir(pluginID, installation.Version), "plugin.json"))
	if err != nil {
		if runtime, ok, inspectErr := inspectPackageRuntime(installation.CachePath); inspectErr == nil && (!ok || runtime != "sidecar") {
			return packageInstallationRecord{}, sidecarTargetNotSidecar, nil
		}
		return packageInstallationRecord{}, sidecarTargetUninstalled, err
	}
	if manifest.Runtime != "sidecar" {
		return packageInstallationRecord{}, sidecarTargetNotSidecar, nil
	}
	return installation, sidecarTargetReady, nil
}

func (s *Service) startSidecar(ctx context.Context, installation packageInstallationRecord) (*sidecarProcess, error) {
	baseDir := s.activePackageDir(installation.PluginID, installation.Version)
	manifest, err := readSidecarManifest(filepath.Join(baseDir, "plugin.json"))
	if err != nil {
		return nil, err
	}
	if manifest.ID != installation.PluginID {
		return nil, fmt.Errorf("plugin manifest id mismatch")
	}
	entrypoint, err := s.sidecarEntrypointFromManifest(baseDir, manifest)
	if err != nil {
		return nil, err
	}
	entrypoint, err = filepath.Abs(entrypoint)
	if err != nil {
		return nil, err
	}
	addr, err := reserveLocalAddr()
	if err != nil {
		return nil, err
	}
	token := randomToken()
	cmd := exec.CommandContext(context.Background(), entrypoint)
	cmd.Dir = filepath.Dir(entrypoint)
	cmd.Env = append(os.Environ(),
		"ASTER_PLUGIN_ID="+installation.PluginID,
		"ASTER_PLUGIN_ADDR="+addr,
		"ASTER_PLUGIN_TOKEN="+token,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPluginRuntimeUnavailable, err)
	}
	proc := &sidecarProcess{
		PluginID: installation.PluginID,
		Version:  installation.Version,
		Endpoint: "http://" + addr,
		Token:    token,
		Command:  cmd,
		done:     make(chan struct{}),
	}
	go proc.wait()
	if err := proc.waitHealthy(ctx, s.httpClient); err != nil {
		_ = proc.stop(context.Background())
		return nil, err
	}
	return proc, nil
}

func (s *Service) stopSidecar(ctx context.Context, pluginID string) error {
	s.sidecarsMu.Lock()
	proc := s.sidecars[pluginID]
	delete(s.sidecars, pluginID)
	s.sidecarsMu.Unlock()
	if proc == nil {
		return nil
	}
	return proc.stop(ctx)
}

func (s *Service) waitSidecar(ctx context.Context, pluginID string, timeout time.Duration) (*sidecarProcess, error) {
	waitCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		s.sidecarsMu.Lock()
		proc := s.sidecars[pluginID]
		supervisor := s.supervisors[pluginID]
		s.sidecarsMu.Unlock()
		if proc != nil && proc.running() {
			return proc, nil
		}
		if supervisor != nil {
			supervisor.wakeNow()
		}
		select {
		case <-waitCtx.Done():
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("%w: sidecar did not become ready within %s", ErrPluginRuntimeUnavailable, timeout)
			}
			return nil, waitCtx.Err()
		case <-ticker.C:
		}
	}
}

func (p *sidecarProcess) running() bool {
	if p == nil || p.Command == nil || p.Command.Process == nil || p.done == nil {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *sidecarProcess) wait() {
	err := p.Command.Wait()
	p.exitMu.Lock()
	p.exitErr = err
	p.exitMu.Unlock()
	close(p.done)
}

func (p *sidecarProcess) exitError() error {
	p.exitMu.RLock()
	defer p.exitMu.RUnlock()
	return p.exitErr
}

func (p *sidecarProcess) waitHealthy(ctx context.Context, client *http.Client) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if !p.running() {
			if err := p.exitError(); err != nil {
				return fmt.Errorf("%w: %v", ErrPluginRuntimeUnavailable, err)
			}
			return ErrPluginRuntimeUnavailable
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.Endpoint+"/health", nil)
		if err == nil {
			resp, err := client.Do(req)
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("%w: health check timed out", ErrPluginRuntimeUnavailable)
}

func (p *sidecarProcess) stop(ctx context.Context) error {
	if p == nil || p.Command == nil || p.Command.Process == nil || p.done == nil {
		return nil
	}
	if !p.running() {
		return nil
	}
	_ = p.Command.Process.Signal(os.Interrupt)
	select {
	case <-ctx.Done():
		_ = p.Command.Process.Kill()
		return ctx.Err()
	case <-time.After(2 * time.Second):
		_ = p.Command.Process.Kill()
		<-p.done
		return nil
	case <-p.done:
		return nil
	}
}

func reserveLocalAddr() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return "", err
	}
	return addr, nil
}

func randomToken() string {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw[:])
}

func sidecarProxyURL(endpoint string, proxyPath string, rawQuery string) (string, error) {
	base, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	cleanPath := "/" + strings.TrimLeft(path.Clean("/"+proxyPath), "/")
	if cleanPath == "/." {
		cleanPath = "/"
	}
	base.Path = cleanPath
	base.RawQuery = rawQuery
	return base.String(), nil
}

func copyProxyRequestHeaders(target http.Header, source http.Header) {
	for key, values := range source {
		if shouldDropProxyHeader(key) {
			continue
		}
		for _, value := range values {
			target.Add(key, value)
		}
	}
}

func shouldDropProxyHeader(key string) bool {
	switch strings.ToLower(key) {
	case "authorization", "cookie", "connection", "host", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}
