package service

import (
	"testing"

	"github.com/Dzobash/apptrol/internal/mixer"
)

func TestSVC13_StatusNamesActiveSolo(t *testing.T) {
	e := newEnv(t, testConfig)
	e.status = &fakeStatus{}
	e.start()
	e.waitStatus(statusLine("controller connected · 3 controls assigned"))

	e.send(mixer.ButtonPressed{Button: mixer.ButtonS, Column: 1})
	e.waitStatus(statusLine("controller connected · solo on slider 1 (Spotify)"))
	e.send(mixer.StreamCorkChanged{ID: spotify.ID, Corked: true})
	e.waitStatus(statusLine("controller connected · solo on slider 1 (Spotify not playing)"))
	e.send(mixer.StreamCorkChanged{ID: spotify.ID, Corked: false})
	e.waitStatus(statusLine("controller connected · solo on slider 1 (Spotify)"))
	e.send(mixer.StreamRemoved{ID: spotify.ID})
	e.waitStatus(statusLine("controller connected · solo on slider 1 (Spotify not playing)"))
	e.send(mixer.StreamAdded{Stream: spotify})
	e.waitStatus(statusLine("controller connected · solo on slider 1 (Spotify)"))

	e.send(mixer.ButtonPressed{Button: mixer.ButtonS, Column: 2})
	e.waitStatus(statusLine("controller connected · solo on slider 2 (Browser)"))
	e.send(mixer.ControllerDisconnected{})
	e.waitStatus(statusLine("controller not found · solo on slider 2 (Browser)"))
	e.send(mixer.SeatChanged{Front: mixer.FrontOtherUser, SessionID: "test-session"})
	e.waitStatus(statusLine("controller released (another user in front) · solo on slider 2 (Browser)"))
	e.send(mixer.SeatChanged{Front: mixer.FrontThisUser})
	e.waitStatus(statusLine("controller not found · solo on slider 2 (Browser)"))
	e.send(mixer.ControllerConnected{})
	e.waitStatus(statusLine("controller connected · solo on slider 2 (Browser)"))
	e.send(mixer.ButtonPressed{Button: mixer.ButtonS, Column: 2})
	e.waitStatus(statusLine("controller connected · 3 controls assigned"))
}

func TestSVC13_StatusSoloFollowsMediaPlayer(t *testing.T) {
	e := newEnv(t, testConfig)
	e.audio.snapshot.Streams = nil
	e.desktop = &fakeDesktop{in: make(chan mixer.Event, 4)}
	e.status = &fakeStatus{}
	e.start()
	e.waitStatus(statusLine("controller connected · 3 controls assigned"))
	e.send(mixer.ButtonPressed{Button: mixer.ButtonS, Column: 2})
	e.waitStatus(statusLine("controller connected · solo on slider 2 (Browser not playing)"))

	p := mixer.Player{BusName: "org.mpris.MediaPlayer2.firefox", Identity: "Firefox", Status: mixer.StatusPlaying}
	e.desktop.in <- mixer.PlayerAdded{Player: p}
	e.waitStatus(statusLine("controller connected · solo on slider 2 (Browser)"))
	e.desktop.in <- mixer.PlayerStatusChanged{BusName: p.BusName, Status: mixer.StatusPaused}
	e.waitStatus(statusLine("controller connected · solo on slider 2 (Browser not playing)"))
	e.desktop.in <- mixer.PlayerStatusChanged{BusName: p.BusName, Status: mixer.StatusPlaying}
	e.waitStatus(statusLine("controller connected · solo on slider 2 (Browser)"))
	e.desktop.in <- mixer.PlayerRemoved{BusName: p.BusName}
	e.waitStatus(statusLine("controller connected · solo on slider 2 (Browser not playing)"))
}

func TestSVC13_StatusSoloUsesEitherPlayingSignalAndOnlySendsChanges(t *testing.T) {
	setup := mixer.Setup{
		Targets: map[string]mixer.Target{
			"app": {Name: "Example app", Match: []string{"example"}},
		},
		Assignments: map[mixer.Control]string{{Kind: mixer.Slider, Column: 5}: "app"},
	}
	f := &fakeStatus{}
	s := &service{m: mixer.New(setup, mixer.State{Solo: 5}), connected: true, controls: 1, o: Options{Status: f}}
	p := mixer.Player{BusName: "org.mpris.MediaPlayer2.example", Status: mixer.StatusPlaying}
	for i, step := range []struct {
		event mixer.Event
		name  string
	}{
		{mixer.PlayerAdded{Player: p}, "Example app"},
		{mixer.StreamAdded{Stream: mixer.Stream{ID: 1, AppName: "example"}}, "Example app"},
		{mixer.PlayerStatusChanged{BusName: p.BusName, Status: mixer.StatusPaused}, "Example app"},
		{mixer.StreamCorkChanged{ID: 1, Corked: true}, "Example app not playing"},
		{mixer.StreamAdded{Stream: mixer.Stream{ID: 2, AppName: "example"}}, "Example app"},
		{mixer.StreamRemoved{ID: 1}, "Example app"},
		{mixer.StreamRemoved{ID: 2}, "Example app not playing"},
		{mixer.StreamAdded{Stream: mixer.Stream{ID: 3, AppName: "unrelated"}}, "Example app not playing"},
		{mixer.PlayerAdded{Player: mixer.Player{BusName: "org.mpris.MediaPlayer2.unrelated", Status: mixer.StatusPlaying}}, "Example app not playing"},
	} {
		before, count := f.last(), len(f.list())
		s.m.Handle(step.event)
		s.updateStatus()
		want := statusLine("controller connected · solo on slider 5 (" + step.name + ")")
		if got := f.last(); got != want {
			t.Fatalf("step %d: status = %q, want %q", i, got, want)
		}
		if before != want {
			count++
		}
		if got := len(f.list()); got != count {
			t.Fatalf("step %d: sent %d lines, want %d", i, got, count)
		}
	}
	setup.Targets["app"] = mixer.Target{Name: "Renamed app", Match: []string{"example"}}
	s.m.Handle(mixer.ConfigChanged{Setup: setup})
	s.updateStatus()
	if got, want := f.last(), statusLine("controller connected · solo on slider 5 (Renamed app not playing)"); got != want {
		t.Fatalf("reloaded status = %q, want %q", got, want)
	}
	s.stopping = true
	s.updateStatus()
	if got := f.last(); got != statusLine("stopping") {
		t.Fatalf("shutdown status = %q", got)
	}
	s.stopping = false
	delete(setup.Assignments, mixer.Control{Kind: mixer.Slider, Column: 5})
	s.m.Handle(mixer.ConfigChanged{Setup: setup})
	s.controls = 0
	s.updateStatus()
	if got, want := f.last(), statusLine("controller connected · 0 controls assigned"); got != want {
		t.Fatalf("removed assignment status = %q, want %q", got, want)
	}
}

func TestSVC13_StatusSoloWaitsForValidConfiguration(t *testing.T) {
	s := &service{m: mixer.NewWithoutConfig(mixer.State{Solo: 5}), connected: true}
	if got := s.statusText(); got != "waiting for a valid configuration" {
		t.Fatalf("waiting status = %q", got)
	}
	s.m.Handle(mixer.ConfigChanged{Setup: mixer.Setup{
		Targets:     map[string]mixer.Target{"app": {Name: "Example app"}},
		Assignments: map[mixer.Control]string{{Kind: mixer.Slider, Column: 5}: "app"},
	}})
	if got, want := s.statusText(), "controller connected · solo on slider 5 (Example app not playing)"; got != want {
		t.Fatalf("restored status = %q, want %q", got, want)
	}
}

func TestSVC13_StatusRestoresSoloWithPlayback(t *testing.T) {
	setup := mixer.Setup{
		Targets:     map[string]mixer.Target{"app": {Name: "Example app", Match: []string{"example"}}},
		Assignments: map[mixer.Control]string{{Kind: mixer.Slider, Column: 5}: "app"},
	}
	for _, tc := range []struct {
		name  string
		event mixer.Event
	}{
		{"stream", mixer.AudioSnapshot{Streams: []mixer.Stream{{ID: 1, AppName: "example"}}}},
		{"player", mixer.PlayerSnapshot{Players: []mixer.Player{{BusName: "org.mpris.MediaPlayer2.example", Status: mixer.StatusPlaying}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &service{m: mixer.New(setup, mixer.State{Solo: 5}), connected: true}
			s.m.Handle(tc.event)
			if got, want := s.statusText(), "controller connected · solo on slider 5 (Example app)"; got != want {
				t.Fatalf("restored status = %q, want %q", got, want)
			}
		})
	}
}
