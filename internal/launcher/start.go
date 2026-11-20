package launcher

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sd "github.com/coreos/go-systemd/v22/dbus"
	"github.com/coreos/go-systemd/v22/unit"
	"github.com/godbus/dbus/v5"

	"github.com/Dzobash/apptrol/internal/desktop"
	"github.com/Dzobash/apptrol/internal/logattr"
	"github.com/Dzobash/apptrol/internal/mixer"
)

// startTimeout bounds the systemd connection and start job (LAUNCH-04, LAUNCH-11, ADR 0019).
const startTimeout = 10 * time.Second

// Starter starts the apps of launcher buttons. systemd starts each one in a
// transient unit of its own, so it is never a child of Apptrol and keeps
// running when Apptrol stops or restarts (LAUNCH-04, ADR 0019).
type Starter struct {
	log  *slog.Logger
	apps func() Apps // the installed apps, read on every press: an app may be installed later

	mu         sync.Mutex
	systemd    systemd       // shared by presses until the transport fails (LAUNCH-04, ADR 0019)
	connecting chan struct{} // closed when the in-flight connection attempt finishes
	connect    func(ctx context.Context) (systemd, error)
	// activate starts a DBusActivatable app without Exec (LAUNCH-03).
	activate func(ctx context.Context, desktopID string) error
	procDir  string // where the running processes are listed: /proc
}

// NewStarter returns a Starter for the user's systemd and session bus.
func NewStarter(log *slog.Logger) *Starter {
	return &Starter{log: log.With(logattr.Component(logattr.Launcher)), apps: Installed,
		connect: connectSystemd, activate: activateDBus, procDir: "/proc"}
}

// Launch starts the app of a launcher button without waiting for it; what
// happens is logged (LAUNCH-01, LAUNCH-11).
func (s *Starter) Launch(l mixer.LaunchApp) {
	go s.start(l)
}

// start does the work of Launch; tests call it directly.
func (s *Starter) start(l mixer.LaunchApp) {
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	attrs := []any{logattr.KeyButton, l.Button.String()}
	if l.Launch.DesktopID != "" {
		attrs = append(attrs, logattr.KeyLauncherDesktopID, l.Launch.DesktopID)
	} else {
		attrs = append(attrs, logattr.KeyLauncherCommand, strings.Join(l.Launch.Command, " "))
	}
	fail := func(err error) {
		s.log.Error("app could not be started", append(attrs, logattr.Error(logattr.ErrAppStart, err))...)
	}

	argv, name, err := s.command(l.Launch)
	if (err == nil || errors.Is(err, errActivate)) && l.Launch.SkipIfRunning && s.alreadyRunning(ctx, name, argv, attrs) {
		return
	}
	if errors.Is(err, errActivate) {
		s.log.Info("starting app", append(attrs, logattr.KeyLauncherUnit, "dbus-activation")...)
		if err := s.activate(ctx, l.Launch.DesktopID); err != nil {
			fail(err)
		}
		return
	}
	if err != nil {
		fail(err)
		return
	}
	unitName := UnitName(name)
	s.log.Info("starting app", append(attrs, logattr.KeyLauncherUnit, unitName)...)
	if err := s.startUnit(ctx, unitName, name, argv); err != nil {
		fail(err)
	}
}

// alreadyRunning checks whether the app runs, for if_running = "skip", and
// logs the outcome with how it was found or what was checked (LAUNCH-07).
func (s *Starter) alreadyRunning(ctx context.Context, name string, argv []string, attrs []any) bool {
	c := s.checkRunning(ctx, name, argv)
	switch {
	case c.steam:
		s.log.Debug("not checked whether the app runs: Steam itself does not start a running game twice", attrs...)
		return false
	case !c.running:
		s.log.Debug("app not running", append(attrs, logattr.KeyLauncherChecked, c.checked)...)
		return false
	case c.foundBy == foundByUnit:
		s.log.Info("app already running; not started", append(attrs,
			logattr.KeyLauncherRunningFoundBy, c.foundBy, logattr.KeyLauncherRunningUnit, c.what)...)
	default:
		s.log.Info("app already running; not started", append(attrs,
			logattr.KeyLauncherRunningFoundBy, c.foundBy, logattr.KeyExecutableName, c.what)...)
	}
	return true
}

// errActivate says that the app is started over D-Bus, not by a command.
var errActivate = errors.New("start by D-Bus activation")

// command returns the program and arguments to run and the name for the
// unit: the desktop ID, or a command's program name.
func (s *Starter) command(l mixer.Launch) (argv []string, name string, err error) {
	if l.DesktopID == "" {
		if len(l.Command) == 0 {
			return nil, "", errors.New("the launcher has neither app nor command")
		}
		argv = make([]string, len(l.Command))
		for i, a := range l.Command {
			argv[i] = expandHome(a) // LAUNCH-05
		}
		return argv, filepath.Base(argv[0]), nil
	}
	app, ok := s.apps()[l.DesktopID]
	if !ok {
		return nil, "", fmt.Errorf("desktop ID %q is not installed (`apptrol list apps` shows the installed ones)", l.DesktopID)
	}
	if app.Exec == "" {
		if app.DBusActivatable {
			return nil, l.DesktopID, errActivate // the ID still names its units
		}
		return nil, "", fmt.Errorf("%s has no Exec line", app.Path)
	}
	argv, err = ParseExec(app.Exec, app.Name)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", app.Path, err)
	}
	return argv, l.DesktopID, nil
}

// expandHome expands "~/" at the start of an argument, as in the
// configuration's paths.
func expandHome(a string) string {
	if !strings.HasPrefix(a, "~/") {
		return a
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return a
	}
	return filepath.Join(home, a[2:])
}

// UnitName is the transient unit an app runs in, following systemd's
// convention for desktop apps: app-apptrol-<id>@<random>.service. The id is
// escaped as systemd-escape does, so desktop IDs with spaces work too.
func UnitName(id string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "app-apptrol-" + unit.UnitNameEscape(id) + "@" + hex.EncodeToString(b) + ".service"
}

// unitProperties describe the unit. Type=exec makes a program that cannot be
// run fail the start (LAUNCH-11); ExitType=cgroup keeps the unit while any of
// its processes runs, so an app started through a wrapper that exits is not
// stopped with it; finished units are removed (CollectMode).
func unitProperties(desc string, argv []string, modern bool) []sd.Property {
	props := []sd.Property{
		sd.PropDescription(desc),
		sd.PropExecStart(argv, false),
		{Name: "CollectMode", Value: dbus.MakeVariant("inactive-or-failed")},
	}
	if modern {
		props = append(props,
			sd.Property{Name: "Type", Value: dbus.MakeVariant("exec")},
			sd.Property{Name: "ExitType", Value: dbus.MakeVariant("cgroup")})
	}
	return props
}

// startUnit asks systemd to start the unit and waits for the job to report
// whether the app could run (LAUNCH-04, LAUNCH-11, ADR 0019).
func (s *Starter) startUnit(ctx context.Context, unitName, desc string, argv []string) error {
	sys, err := s.conn(ctx)
	if err != nil {
		return err
	}
	err = startJob(ctx, sys, unitName, "Apptrol: "+desc, argv)
	if systemdTransportError(sys, err) {
		s.dropConn(sys)
	}
	return err
}

func startJob(ctx context.Context, sys systemd, unitName, desc string, argv []string) error {
	done := make(chan string, 1)
	_, err := sys.StartTransientUnitContext(ctx, unitName, "fail", unitProperties(desc, argv, true), done)
	// systemd older than 250 rejects ExitType. Retry the request without the
	// newer properties only when the request itself was rejected (LAUNCH-04, ADR 0019).
	if err != nil && strings.Contains(err.Error(), "ExitType") && !systemdTransportError(sys, err) {
		_, err = sys.StartTransientUnitContext(ctx, unitName, "fail", unitProperties(desc, argv, false), done)
	}
	if err != nil {
		return err
	}
	select {
	case result := <-done:
		if result != "done" {
			return fmt.Errorf("systemd start job %s: %s (journalctl --user -u %s shows why)", unitName, result, unitName)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("systemd did not start %s in time", unitName)
	}
}

// activateDBus starts an app through org.freedesktop.Application.Activate:
// its bus name is the desktop ID, its object path the ID with "." as "/" and
// "-" as "_" (Desktop Entry specification, D-Bus activation).
func activateDBus(ctx context.Context, desktopID string) error {
	addr, err := desktop.SessionBusAddress()
	if err != nil {
		return err
	}
	c, err := dbus.Connect(addr, dbus.WithContext(ctx))
	if err != nil {
		return err
	}
	defer c.Close()
	path := dbus.ObjectPath("/" + strings.NewReplacer(".", "/", "-", "_").Replace(desktopID))
	return c.Object(desktopID, path).CallWithContext(ctx, "org.freedesktop.Application.Activate", 0,
		map[string]dbus.Variant{}).Err
}
