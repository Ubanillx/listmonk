package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeploymentPinsTimezone guards the deployment half of the resume-time
// defect. The systemd unit used to start listmonk without TZ, so the process
// inherited whatever zone the host had; a host running on UTC persisted a
// 09:00 Asia/Shanghai campaign as 09:00 UTC and the admin UI then showed a
// resume at 17:00. The unit must therefore always set TZ from the pipeline's
// APP_TIMEZONE parameter.
func TestDeploymentPinsTimezone(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "Jenkinsfile"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)

	required := []struct {
		name    string
		needle  string
		because string
	}{
		{
			name:    "timezone parameter",
			needle:  "name: 'APP_TIMEZONE'",
			because: "the deployment needs a configurable zone instead of the host default",
		},
		{
			name:    "timezone default",
			needle:  "defaultValue: 'Asia/Shanghai'",
			because: "the platform is operated in China",
		},
		{
			name:    "timezone reaches the remote script",
			needle:  "APP_TIMEZONE=%q",
			because: "the unit is written by a script that only sees exported variables",
		},
		{
			name:    "unit exports TZ",
			needle:  "Environment=TZ=$app_timezone",
			because: "without TZ the process inherits the host zone",
		},
	}

	for _, want := range required {
		if !strings.Contains(src, want.needle) {
			t.Errorf("Jenkinsfile is missing %s (%q): %s", want.name, want.needle, want.because)
		}
	}

	// A TZ line that is present but empty would silently restore the defect.
	if strings.Contains(src, "Environment=TZ=\n") || strings.Contains(src, "Environment=TZ= ") {
		t.Error("Jenkinsfile writes an empty TZ into the systemd unit")
	}
}
