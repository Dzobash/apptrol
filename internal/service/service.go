// Package service runs Apptrol: one event loop that owns the mixer, feeds it
// events from the controller, the audio server and the configuration file,
// and carries out the actions it returns (ADR 0015).
package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Dzobash/apptrol/examples"
	"github.com/Dzobash/apptrol/internal/audio/pulse"
	"github.com/Dzobash/apptrol/internal/config"
	"github.com/Dzobash/apptrol/internal/controller/rawmidi"
	"github.com/Dzobash/apptrol/internal/launcher"
	"github.com/Dzobash/apptrol/internal/logattr"
	"github.com/Dzobash/apptrol/internal/mixer"
	"github.com/Dzobash/apptrol/internal/state"
	"github.com/Dzobash/apptrol/internal/version"
)

// DefaultPort is the controller's sound card id when there is no valid configuration.
const DefaultPort = "nanoKONTROL2"

// Audio is the audio server (internal/audio/pulse).
type Audio interface {
	Run(ctx context.Context, out chan<- mixer.Event) error
	Apply(a mixer.Action) error
}

// Desktop is the session bus: media players (internal/desktop, ADR 0017).
type Desktop interface {
	Run(ctx context.Context, out chan<- mixer.Event) error
	Apply(a mixer.Action) error // does not wait for the player's answer
}

// Launcher starts apps for launcher buttons (internal/launcher, ADR 0019);
// it does not wait for them.
type Launcher interface {
	Launch(l mixer.LaunchApp)
}

// recordFlash is how long the Record LED lights up when it starts an app (LAUNCH-08).
const recordFlash = 300 * time.Millisecond

// Controller is the MIDI controller (internal/controller/rawmidi).
type Controller interface {
	Run(ctx context.Context, out chan<- mixer.Event) error
	SetLED(l mixer.LED, on bool) error
	HasLED(l mixer.LED) bool
}

// Logs applies the logging part of the configuration (LOG-08).
type Logs interface {
	Reconfigure(cfg config.Log) error
}

// Options configures Run.
type Options struct {
	ConfigPath string
	StatePath  string
	Log        *slog.Logger
	Logs       Logs // may be nil
	// LogLevelFlag is the level given with --log-level, "" if none; it is
	// named on the start record (LOG-16).
	LogLevelFlag string

	Audio         Audio
	NewController func(port string) Controller
	Desktop       Desktop // may be nil: no media players
	// InstalledApps lists the installed apps, for the launchers' warnings
	// (LAUNCH-10); nil means launcher.Installed.
	InstalledApps func() launcher.Apps
	Launcher      Launcher // may be nil: launcher buttons do nothing

	// Zero values mean the defaults: the file is checked every second, and the
	// state is saved at most once per second.
	WatchInterval time.Duration
	SaveDelay     time.Duration
	LEDResync     time.Duration // zero: audioLEDResync
}

// maxBatch bounds how many queued events are read before handling them.
const maxBatch = 256

// audioLEDResync is how long after the audio server (re)connects the LEDs are
// sent once more. A restart of PipeWire can reset the controller's LEDs while
// its MIDI support starts up, which may end after the audio server accepts
// connections (LED-07).
const audioLEDResync = 2 * time.Second

type service struct {
	o    Options
	log  *slog.Logger // apptrol.component=service; see logFor for the others
	logs map[string]*slog.Logger
	m    *mixer.Mixer
	ctl  Controller
	port string
	// flashEnd fires when the Record LED's flash is over (LAUNCH-08).
	flashEnd <-chan time.Time
	saver    *state.Saver
}

// Run runs Apptrol until ctx is canceled (SIGTERM or Ctrl+C), then saves the
// state, ends solo, turns the LEDs off and returns (SVC-05, SVC-07).
func Run(ctx context.Context, o Options) error {
	if o.WatchInterval == 0 {
		o.WatchInterval = time.Second
	}
	if o.SaveDelay == 0 {
		o.SaveDelay = state.DefaultDelay
	}
	if o.LEDResync == 0 {
		o.LEDResync = audioLEDResync
	}
	s := &service{o: o, logs: map[string]*slog.Logger{}}
	s.log = s.logFor(logattr.Service)
	ver, _, _ := version.Info()
	start := []any{logattr.KeyServiceName, "apptrol", logattr.KeyServiceVersion, ver}
	if o.LogLevelFlag != "" {
		start = append(start, logattr.KeyLogLevel, o.LogLevelFlag, logattr.KeyLogLevelSource, "flag")
	} else {
		start = append(start, logattr.KeyLogLevelSource, "config")
	}
	s.log.Info("Apptrol starting", start...)

	cfg, cfgData := s.loadConfig()
	setup := mixer.Setup{}
	s.port = DefaultPort
	if cfg != nil {
		setup = cfg.Setup()
		s.port = cfg.Controller.Port
	}

	saved := s.loadState()
	s.m = mixer.New(setup, saved)
	s.saver = state.NewSaver(o.StatePath, o.SaveDelay, func(err error) {
		s.logFor(logattr.State).Warn("could not save the state",
			logattr.KeyFilePath, o.StatePath, logattr.Error(logattr.ErrStateNotSaved, err))
	})
	s.saver.Loaded(saved)
	s.ctl = o.NewController(s.port)

	// The adapters get their own context: on shutdown they must keep running
	// until solo has been ended and the LEDs are off.
	actx, stopAdapters := context.WithCancel(context.Background())
	defer stopAdapters()
	events := make(chan mixer.Event, maxBatch)
	changes := make(chan config.Change, 4)
	var wg sync.WaitGroup
	runs := []func(){
		func() { _ = o.Audio.Run(actx, events) },
		func() { _ = s.ctl.Run(actx, events) },
		func() {
			config.Watch(actx, o.ConfigPath, cfgData, o.WatchInterval, o.WatchInterval/4, changes)
		},
	}
	if o.Desktop != nil {
		runs = append(runs, func() { _ = o.Desktop.Run(actx, events) })
	}
	for _, run := range runs {
		wg.Add(1)
		go func() { defer wg.Done(); run() }()
	}

	s.log.Info("Apptrol running")
	var ledResync <-chan time.Time // see audioLEDResync
	for {
		select {
		case <-ctx.Done():
			s.shutdown()
			stopAdapters()
			waitTimeout(&wg, 3*time.Second)
			s.log.Info("Apptrol stopped")
			return nil
		case ch := <-changes:
			s.configChanged(ch)
		case <-ledResync:
			ledResync = nil
			s.handle(mixer.ControllerConnected{Resync: true})
		case <-s.flashEnd:
			s.flashEnd = nil
			_ = s.ctl.SetLED(mixer.LED{Transport: mixer.Record}, false)
		case ev := <-events:
			batch := []mixer.Event{ev}
		more:
			for len(batch) < maxBatch {
				select {
				case e := <-events:
					batch = append(batch, e)
				default:
					break more
				}
			}
			for _, e := range Coalesce(batch) {
				if _, ok := e.(mixer.AudioSnapshot); ok {
					ledResync = time.After(o.LEDResync)
				}
				s.handle(e)
			}
		}
	}
}

// loadConfig loads the configuration at start. Without a file it creates the
// example (CFG-09). An invalid file is logged, and Apptrol runs without
// assignments until the file is fixed (CFG-07). It returns the configuration
// (nil if none is valid) and the file contents it read.
func (s *service) loadConfig() (*config.Config, []byte) {
	log := s.logFor(logattr.Config)
	path := s.o.ConfigPath
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if werr := writeExample(path); werr != nil {
			log.Error("no configuration file, and the example could not be created",
				logattr.KeyFilePath, path, logattr.Error(logattr.ErrExampleNotCreated, werr))
			return nil, nil
		}
		log.Info("created an example configuration; edit it to assign your apps", logattr.KeyFilePath, path)
		data, err = os.ReadFile(path)
	}
	if err != nil {
		log.Error("cannot read the configuration", logattr.KeyFilePath, path, logattr.Error(logattr.ErrConfigUnreadable, err))
		return nil, nil
	}
	cfg, warnings, err := config.Parse(path, data)
	if err != nil {
		s.invalidConfig("invalid configuration; waiting for a valid one (run `apptrol check`)", path, err)
		return nil, data
	}
	log.Info("configuration loaded", logattr.KeyFilePath, path, logattr.KeyControls, len(cfg.Layout().Assignments))
	s.applyLogging(cfg)
	s.configWarnings(path, warnings)
	s.logButtons(cfg)
	s.launcherWarnings(cfg)
	return cfg, data
}

// logFor returns the logger for a component: every record says which part of
// Apptrol it comes from (LOG-10).
func (s *service) logFor(component string) *slog.Logger {
	l, ok := s.logs[component]
	if !ok {
		l = s.o.Log.With(logattr.Component(component))
		s.logs[component] = l
	}
	return l
}

// invalidConfig logs one record per problem of an invalid configuration, so
// each stays on one line and can be read on its own (LOG-13).
func (s *service) invalidConfig(msg, path string, err error) {
	log := s.logFor(logattr.Config)
	var ve *config.ValidationError
	if !errors.As(err, &ve) {
		log.Error(msg, logattr.KeyFilePath, path, logattr.Error(logattr.ErrConfigInvalid, err))
		return
	}
	for _, p := range ve.Problems {
		log.Error(msg, logattr.KeyFilePath, path, logattr.Error(logattr.ErrConfigInvalid, errors.New(p)))
	}
}

func (s *service) configWarnings(path string, warnings []string) {
	for _, w := range warnings {
		s.logFor(logattr.Config).Warn("configuration warning", logattr.KeyFilePath, path, logattr.KeyWarning, w)
	}
}

// launcherWarnings warns about launchers whose desktop ID is not installed;
// the configuration stays valid, as the app may be installed later (LAUNCH-10).
func (s *service) launcherWarnings(cfg *config.Config) {
	installed := s.o.InstalledApps
	if installed == nil {
		installed = launcher.Installed
	}
	for _, m := range installed().Missing(cfg.LauncherApps()) {
		s.logFor(logattr.Config).Warn("desktop ID not installed; the button will not start anything (`apptrol list apps` shows the installed ones)",
			logattr.KeyLayout, m.Layout, logattr.KeyButton, buttonLabel(m.Button), logattr.KeyLauncherDesktopID, m.DesktopID)
	}
}

// buttonLabel names a button from the configuration as the mixer's log does:
// "R3", or "●" for record.
func buttonLabel(name string) string {
	if l := transportLabels[name]; l != "" {
		return l
	}
	return strings.ToUpper(name)
}

// writeExample creates the example configuration, but never overwrites a file.
func writeExample(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(examples.Config); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func (s *service) applyLogging(cfg *config.Config) {
	if s.o.Logs == nil {
		return
	}
	if err := s.o.Logs.Reconfigure(cfg.Log); err != nil {
		s.logFor(logattr.Config).Error("could not apply the log settings", logattr.Error(logattr.ErrLogSetup, err))
	}
}

// loadState restores positions and mutes (STATE-03); a missing or damaged
// file means starting without them (STATE-05).
func (s *service) loadState() mixer.State {
	log := s.logFor(logattr.State)
	path := s.o.StatePath
	st, warnings, err := state.Load(path)
	switch {
	case state.IsFirstStart(err):
		log.Debug("no saved state yet", logattr.KeyFilePath, path)
	case err != nil:
		log.Warn("cannot use the saved state; starting without it",
			logattr.KeyFilePath, path, logattr.Error(logattr.ErrStateUnreadable, err))
	default:
		solo := ""
		if st.Solo != 0 {
			solo = mixer.Control{Kind: mixer.Slider, Column: st.Solo}.String()
		}
		log.Info("state restored", logattr.KeyFilePath, path,
			logattr.KeyPositions, len(st.Positions), logattr.KeyMutes, len(st.Muted), logattr.KeySolo, solo)
	}
	for _, w := range warnings {
		log.Warn("saved state warning", logattr.KeyFilePath, path, logattr.KeyWarning, w)
	}
	return st
}

// configChanged handles a change of the configuration file (CFG-06, CFG-07).
func (s *service) configChanged(ch config.Change) {
	log := s.logFor(logattr.Config)
	path := s.o.ConfigPath
	switch {
	case ch.Removed:
		log.Warn("the configuration file was removed; keeping the current settings",
			logattr.KeyFilePath, path, logattr.Error(logattr.ErrConfigRemoved, nil))
		return
	case ch.Err != nil:
		log.Error("cannot read the changed configuration; keeping the current settings",
			logattr.KeyFilePath, path, logattr.Error(logattr.ErrConfigUnreadable, ch.Err))
		return
	}
	cfg, warnings, err := config.Parse(path, ch.Data)
	if err != nil {
		s.invalidConfig("the changed configuration is invalid; keeping the current settings", path, err)
		return
	}
	log.Info("configuration reloaded", logattr.KeyFilePath, path, logattr.KeyControls, len(cfg.Layout().Assignments))
	s.applyLogging(cfg)
	s.configWarnings(path, warnings)
	s.logButtons(cfg)
	s.launcherWarnings(cfg)
	if cfg.Controller.Port != s.port {
		log.Warn("the controller setting changed; restart Apptrol to use it",
			logattr.KeyPort, cfg.Controller.Port, logattr.KeyPortInUse, s.port)
	}
	s.handle(mixer.ConfigChanged{Setup: cfg.Setup()})
}

// handle passes one event to the mixer and carries out its actions.
func (s *service) handle(ev mixer.Event) {
	for _, a := range s.m.Handle(ev) {
		s.do(a)
	}
}

func (s *service) do(a mixer.Action) {
	switch a := a.(type) {
	case mixer.SetStreamVolume, mixer.SetStreamMute, mixer.SetDeviceVolume, mixer.SetDeviceMute:
		err := s.o.Audio.Apply(a)
		switch {
		case err == nil:
		case pulse.IsGone(err), errors.Is(err, pulse.ErrNotConnected):
			// The app just ended, or the audio server is away; everything is
			// applied again when it returns (SVC-04).
			s.logFor(logattr.Audio).Debug("audio change not applied",
				logattr.KeyAction, fmt.Sprintf("%+v", a), logattr.Error(logattr.ErrAudioApply, err))
		default:
			s.logFor(logattr.Audio).Warn("could not change the audio setting",
				logattr.KeyAction, fmt.Sprintf("%+v", a), logattr.Error(logattr.ErrAudioApply, err))
		}
	case mixer.SetLED:
		if err := s.ctl.SetLED(a.LED, a.On); err != nil && !errors.Is(err, rawmidi.ErrNotConnected) {
			s.logFor(logattr.Controller).Debug("could not set an LED",
				logattr.KeyLED, a.LED.String(), logattr.Error(logattr.ErrLEDFailed, err))
		}
	case mixer.LaunchApp:
		if s.o.Launcher != nil {
			s.o.Launcher.Launch(a) // logs what it starts, and failures
		}
		if a.Button == (mixer.LED{Transport: mixer.Record}) {
			// The mixer has no clock: the service flashes the LED (LAUNCH-08).
			_ = s.ctl.SetLED(a.Button, true)
			s.flashEnd = time.After(recordFlash)
		}
	case mixer.PlayerCommand:
		if s.o.Desktop == nil {
			return
		}
		// Refusals are logged by the adapter when the answer arrives.
		if err := s.o.Desktop.Apply(a); err != nil {
			s.logFor(logattr.Desktop).Debug("media player command not sent", logattr.KeyPlayerBusName, a.BusName,
				logattr.KeyPlayerCommand, a.Command, logattr.Error(logattr.ErrMediaCommand, err))
		}
	case mixer.StateChanged:
		s.saver.Request(s.m.Snapshot())
	case mixer.Notice:
		s.logFor(logattr.Mixer).Log(context.Background(), a.Level, a.Msg, a.Attrs...)
	}
}

// transportLabels name the launcher buttons as the mixer's log does.
var transportLabels = map[string]string{
	"record":      mixer.Record.String(),
	"marker_set":  mixer.MarkerSet.String(),
	"marker_prev": mixer.MarkerPrev.String(),
	"marker_next": mixer.MarkerNext.String(),
}

// logButtons logs every button set in the active layout, so the log shows why
// a button acts as it does (CFG-13, LOG-15).
func (s *service) logButtons(cfg *config.Config) {
	log := s.logFor(logattr.Config)
	layout := cfg.Layout()
	names := make([]string, 0, len(layout.Buttons))
	for n := range layout.Buttons {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		b := layout.Buttons[n]
		attrs := []any{logattr.KeyLayout, layout.Name, logattr.KeyButton, buttonLabel(n)}
		switch {
		case b.App != "":
			attrs = append(attrs, logattr.KeyButtonMode, "launcher", logattr.KeyLauncherDesktopID, b.App)
		case len(b.Command) > 0:
			attrs = append(attrs, logattr.KeyButtonMode, "launcher", logattr.KeyLauncherCommand, strings.Join(b.Command, " "))
		default:
			attrs = append(attrs, logattr.KeyButtonMode, b.Mode)
		}
		if b.TalkOver {
			attrs = append(attrs, logattr.KeyButtonTalkOver, true)
		}
		log.Info("button configured", attrs...)
	}
}

// shutdown saves the state (SVC-05), ends solo so no app stays silenced by it
// while Apptrol is not running (SVC-07) and turns every LED off (LED-08).
func (s *service) shutdown() {
	s.log.Info("Apptrol stopping")
	// Save before ending solo: the next start restores it (STATE-04).
	s.saver.Request(s.m.Snapshot())
	for _, a := range s.m.Shutdown() {
		if _, ok := a.(mixer.StateChanged); !ok {
			s.do(a)
		}
	}
	if err := s.saver.Flush(); err != nil {
		s.logFor(logattr.State).Error("could not save the state",
			logattr.KeyFilePath, s.o.StatePath, logattr.Error(logattr.ErrStateNotSaved, err))
	}
	for col := 1; col <= mixer.NumColumns; col++ {
		for _, b := range []mixer.ButtonKind{mixer.ButtonS, mixer.ButtonM, mixer.ButtonR} {
			_ = s.ctl.SetLED(mixer.LED{Button: b, Column: col}, false)
		}
	}
	for _, t := range mixer.AllTransport {
		if l := (mixer.LED{Transport: t}); s.ctl.HasLED(l) {
			_ = s.ctl.SetLED(l, false)
		}
	}
}

// Coalesce drops every slider or knob position that a later event in the same
// batch replaces, so a fast slider move does not flood the audio server. The
// last position of each control is always kept (CTRL-07); all other events are
// kept in order.
func Coalesce(batch []mixer.Event) []mixer.Event {
	last := map[mixer.Control]int{}
	for i, ev := range batch {
		if e, ok := ev.(mixer.ControlMoved); ok {
			last[e.Control] = i
		}
	}
	out := batch[:0:0]
	for i, ev := range batch {
		if e, ok := ev.(mixer.ControlMoved); ok && last[e.Control] != i {
			continue
		}
		out = append(out, ev)
	}
	return out
}

func waitTimeout(wg *sync.WaitGroup, d time.Duration) {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
	}
}
