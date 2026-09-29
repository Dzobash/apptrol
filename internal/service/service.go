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
	"sync"
	"time"

	"github.com/Dzobash/apptrol/examples"
	"github.com/Dzobash/apptrol/internal/audio/pulse"
	"github.com/Dzobash/apptrol/internal/config"
	"github.com/Dzobash/apptrol/internal/controller/rawmidi"
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

	Audio         Audio
	NewController func(port string) Controller

	// Zero values mean the defaults: the file is checked every second, and the
	// state is saved at most once per second.
	WatchInterval time.Duration
	SaveDelay     time.Duration
}

// maxBatch bounds how many queued events are read before handling them.
const maxBatch = 256

type service struct {
	o     Options
	log   *slog.Logger
	m     *mixer.Mixer
	ctl   Controller
	port  string
	saver *state.Saver
}

// Run runs Apptrol until ctx is canceled (SIGTERM or Ctrl+C), then ends solo,
// saves the state, turns the LEDs off and returns (SVC-05, SVC-07).
func Run(ctx context.Context, o Options) error {
	if o.WatchInterval == 0 {
		o.WatchInterval = time.Second
	}
	if o.SaveDelay == 0 {
		o.SaveDelay = state.DefaultDelay
	}
	s := &service{o: o, log: o.Log}
	ver, _, _ := version.Info()
	s.log.Info("Apptrol starting", "version", ver, "config", o.ConfigPath)

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
		s.log.Warn("could not save the state", "file", o.StatePath, "err", err)
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
	for _, run := range []func(){
		func() { _ = o.Audio.Run(actx, events) },
		func() { _ = s.ctl.Run(actx, events) },
		func() {
			config.Watch(actx, o.ConfigPath, cfgData, o.WatchInterval, o.WatchInterval/4, changes)
		},
	} {
		wg.Add(1)
		go func() { defer wg.Done(); run() }()
	}

	s.log.Info("Apptrol running")
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
	path := s.o.ConfigPath
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if werr := writeExample(path); werr != nil {
			s.log.Error("no configuration file, and the example could not be created", "file", path, "err", werr)
			return nil, nil
		}
		s.log.Info("created an example configuration; edit it to assign your apps", "file", path)
		data, err = os.ReadFile(path)
	}
	if err != nil {
		s.log.Error("cannot read the configuration", "file", path, "err", err)
		return nil, nil
	}
	cfg, warnings, err := config.Parse(path, data)
	if err != nil {
		s.log.Error("invalid configuration; waiting for a valid one (run `apptrol check`)", "err", err)
		return nil, data
	}
	s.log.Info("configuration loaded", "file", path, "controls", len(cfg.Layout().Assignments))
	s.applyLogging(cfg)
	for _, w := range warnings {
		s.log.Warn("configuration: " + w)
	}
	return cfg, data
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
		s.log.Error("could not apply the log settings", "err", err)
	}
}

// loadState restores positions and mutes (STATE-03); a missing or damaged
// file means starting without them (STATE-05).
func (s *service) loadState() mixer.State {
	st, warnings, err := state.Load(s.o.StatePath)
	switch {
	case state.IsFirstStart(err):
		s.log.Debug("no saved state yet", "file", s.o.StatePath)
	case err != nil:
		s.log.Warn("cannot use the saved state; starting without it", "file", s.o.StatePath, "err", err)
	default:
		s.log.Info("state restored", "file", s.o.StatePath, "positions", len(st.Positions), "mutes", len(st.Muted))
	}
	for _, w := range warnings {
		s.log.Warn(w)
	}
	return st
}

// configChanged handles a change of the configuration file (CFG-06, CFG-07).
func (s *service) configChanged(ch config.Change) {
	path := s.o.ConfigPath
	switch {
	case ch.Removed:
		s.log.Warn("the configuration file was removed; keeping the current settings", "file", path)
		return
	case ch.Err != nil:
		s.log.Error("cannot read the changed configuration; keeping the current settings", "file", path, "err", ch.Err)
		return
	}
	cfg, warnings, err := config.Parse(path, ch.Data)
	if err != nil {
		s.log.Error("the changed configuration is invalid; keeping the current settings", "err", err)
		return
	}
	s.log.Info("configuration reloaded", "file", path, "controls", len(cfg.Layout().Assignments))
	s.applyLogging(cfg)
	for _, w := range warnings {
		s.log.Warn("configuration: " + w)
	}
	if cfg.Controller.Port != s.port {
		s.log.Warn("the controller setting changed; restart Apptrol to use it",
			"port", cfg.Controller.Port, "in_use", s.port)
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
			s.log.Debug("audio change not applied", "action", fmt.Sprintf("%T", a), "err", err)
		default:
			s.log.Warn("could not change the audio setting", "action", fmt.Sprintf("%+v", a), "err", err)
		}
	case mixer.SetLED:
		if err := s.ctl.SetLED(a.LED, a.On); err != nil && !errors.Is(err, rawmidi.ErrNotConnected) {
			s.log.Debug("could not set an LED", "led", a.LED.String(), "err", err)
		}
	case mixer.StateChanged:
		s.saver.Request(s.m.Snapshot())
	case mixer.Notice:
		s.log.Log(context.Background(), a.Level, a.Msg, a.Attrs...)
	}
}

// shutdown ends solo so no app stays silenced by it (SVC-07), saves the state
// (SVC-05) and turns every LED off (LED-08).
func (s *service) shutdown() {
	s.log.Info("Apptrol stopping")
	for _, a := range s.m.Shutdown() {
		s.do(a)
	}
	s.saver.Request(s.m.Snapshot())
	if err := s.saver.Flush(); err != nil {
		s.log.Error("could not save the state", "file", s.o.StatePath, "err", err)
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
