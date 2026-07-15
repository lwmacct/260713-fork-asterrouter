package plugins

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
)

var (
	ErrPluginHostUnauthorized = errors.New("plugin host request is unauthorized")
	ErrPluginHostPermission   = errors.New("plugin host data feed permission is missing")
)

// AuthorizeSidecarHost verifies the loopback sidecar runtime token. Provider
// callback signatures are intentionally not handled here: the adapter must
// verify vendor-specific signatures before sending this normalized request.
func (s *Service) AuthorizeSidecarHost(ctx context.Context, pluginID, token string) error {
	if s == nil {
		return ErrPluginHostUnauthorized
	}
	pluginID = strings.TrimSpace(pluginID)
	token = strings.TrimSpace(token)
	if pluginID == "" || token == "" {
		return ErrPluginHostUnauthorized
	}
	s.sidecarsMu.Lock()
	proc := s.sidecars[pluginID]
	authorized := proc != nil && proc.running() && constantTimeStringEqual(proc.Token, token)
	s.sidecarsMu.Unlock()
	if !authorized {
		return ErrPluginHostUnauthorized
	}
	_, state, err := s.sidecarTargetState(ctx, pluginID)
	if err != nil || state != sidecarTargetReady {
		return ErrPluginHostUnauthorized
	}
	return nil
}

// AuthorizeSidecarProviderCallback additionally requires the installed
// adapter manifest to opt into the normalized callback contract.
func (s *Service) AuthorizeSidecarProviderCallback(ctx context.Context, pluginID, token string) error {
	if err := s.AuthorizeSidecarHost(ctx, pluginID, token); err != nil {
		return err
	}
	installation, state, err := s.sidecarTargetState(ctx, strings.TrimSpace(pluginID))
	if err != nil || state != sidecarTargetReady {
		return ErrPluginHostUnauthorized
	}
	activeDir, err := s.activePackageDir(pluginID, installation.Version)
	if err != nil {
		return ErrPluginHostUnauthorized
	}
	manifest, err := readSidecarManifest(filepath.Join(activeDir, "plugin.json"))
	if err != nil {
		return ErrPluginHostUnauthorized
	}
	for _, capability := range manifest.ProviderAdapters {
		if capability.SupportsCallbacks {
			return nil
		}
	}
	return ErrPluginHostPermission
}

func (s *Service) SidecarFeedPayload(ctx context.Context, pluginID string, token string, serviceKey string) (json.RawMessage, error) {
	pluginID = strings.TrimSpace(pluginID)
	token = strings.TrimSpace(token)
	serviceKey = strings.TrimSpace(serviceKey)
	if pluginID == "" || token == "" || serviceKey == "" {
		return nil, ErrPluginHostUnauthorized
	}

	s.sidecarsMu.Lock()
	proc := s.sidecars[pluginID]
	authorized := proc != nil && proc.running() && constantTimeStringEqual(proc.Token, token)
	s.sidecarsMu.Unlock()
	if !authorized {
		return nil, ErrPluginHostUnauthorized
	}

	installation, state, err := s.sidecarTargetState(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	if state != sidecarTargetReady {
		return nil, ErrPluginHostUnauthorized
	}
	activeDir, err := s.activePackageDir(pluginID, installation.Version)
	if err != nil {
		return nil, ErrPluginHostUnauthorized
	}
	manifest, err := readSidecarManifest(filepath.Join(activeDir, "plugin.json"))
	if err != nil {
		return nil, err
	}
	if !manifestAllowsDataFeed(manifest, serviceKey) {
		return nil, ErrPluginHostPermission
	}
	return s.OfficialFeedPayload(ctx, serviceKey)
}

func manifestAllowsDataFeed(manifest sidecarManifest, serviceKey string) bool {
	for _, allowed := range manifest.DataFeeds {
		if allowed == serviceKey {
			return true
		}
	}
	return false
}

func constantTimeStringEqual(left string, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
