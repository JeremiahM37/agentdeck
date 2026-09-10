package sessions

import (
	"encoding/json"
	"fmt"

	"github.com/JeremiahM37/agentdeck/internal/store"
)

// LaunchConfiguration records declared settings, not the target's ambient
// environment or the contents of its credential/configuration files. It is
// private because explicitly configured environment values may contain secrets.
type LaunchConfiguration struct {
	Version int  `json:"version"`
	Spec    Spec `json:"spec"`
	Yolo    bool `json:"yolo"`
}

// SessionLaunchConfiguration falls back to current settings only for records
// predating snapshots or adopted terminals whose original settings are unknown.
func (m *Manager) SessionLaunchConfiguration(row *store.Session) (*LaunchConfiguration, error) {
	var saved *LaunchConfiguration
	if row.LaunchConfigJSON != "" {
		if err := json.Unmarshal([]byte(row.LaunchConfigJSON), &saved); err != nil || saved == nil {
			return nil, fmt.Errorf("session launch configuration is unreadable")
		}
	}
	return m.launchConfiguration(row.Agent, row.ProjectID, saved)
}

func (m *Manager) launchConfiguration(agent string, projectID *int64, saved *LaunchConfiguration) (*LaunchConfiguration, error) {
	if saved != nil {
		if saved.Version != 1 || saved.Spec.Name != agent || saved.Spec.Command == "" {
			return nil, fmt.Errorf("session launch configuration is unsupported or incomplete")
		}
		if _, err := EnvPrefix(saved.Spec.Env); err != nil {
			return nil, err
		}
		copy := *saved
		return &copy, nil
	}
	spec, ok := Find(m.specs(), agent)
	if !ok {
		return nil, fmt.Errorf("unknown agent %q", agent)
	}
	spec = m.Launcher.resolve(spec)
	env := map[string]string{}
	for k, v := range spec.Env {
		env[k] = v
	}
	for k, v := range m.ProjectEnv(projectID) {
		env[k] = v
	}
	spec.Env = env
	return &LaunchConfiguration{Version: 1, Spec: spec}, nil
}
