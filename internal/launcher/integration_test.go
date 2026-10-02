package launcher

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	sd "github.com/coreos/go-systemd/v22/dbus"

	"github.com/Dzobash/apptrol/internal/mixer"
)

// Integration tests against the real systemd user manager. They run only
// with APPTROL_SYSTEMD_TEST=1 (`make test-launcher`); they start harmless
// programs (`true`) in the user's own systemd and check that their units are
// removed. CI has no user systemd, so it does not run them.

func requireSystemd(t *testing.T) *sd.Conn {
	t.Helper()
	if os.Getenv("APPTROL_SYSTEMD_TEST") != "1" {
		t.Skip("set APPTROL_SYSTEMD_TEST=1 (make test-launcher) to start test units in your systemd user manager")
	}
	sys, err := connectSystemd(context.Background())
	if err != nil {
		t.Fatalf("no systemd user manager: %v", err)
	}
	conn := sys.(*sd.Conn)
	t.Cleanup(conn.Close)
	return conn
}

func TestIntegration_LAUNCH04_AUnitStartsAndIsRemoved(t *testing.T) {
	conn := requireSystemd(t)
	var log bytes.Buffer
	s := NewStarter(slog.New(slog.NewTextHandler(&log, nil)))
	s.start(mixer.LaunchApp{Launch: mixer.Launch{Command: []string{"true"}}})
	if strings.Contains(log.String(), "could not be started") || !strings.Contains(log.String(), "starting app") {
		t.Fatalf("log:\n%s", log.String())
	}
	// `true` exits at once; CollectMode removes its unit (LAUNCH-04).
	deadline := time.Now().Add(5 * time.Second)
	for {
		units, err := conn.ListUnitsByPatternsContext(context.Background(), nil, []string{"app-apptrol-true@*"})
		if err != nil {
			t.Fatal(err)
		}
		if len(units) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("unit not removed: %+v", units)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestIntegration_LAUNCH11_AMissingProgramFails(t *testing.T) {
	requireSystemd(t)
	var log bytes.Buffer
	s := NewStarter(slog.New(slog.NewTextHandler(&log, nil)))
	s.start(mixer.LaunchApp{Launch: mixer.Launch{Command: []string{"/nonexistent/apptrol-test-program"}}})
	for _, want := range []string{"app could not be started", "app_start_failed"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, log.String())
		}
	}
}
