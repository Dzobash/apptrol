package service

import (
	"strings"
	"testing"

	"github.com/Dzobash/apptrol/internal/mixer"
)

func TestINPUT09_DefaultInputFollowedThroughTheService(t *testing.T) {
	src := strings.Replace(testConfig, `match = ["goxlr"]`, `device = "default"`, 1)
	e := newEnv(t, src)
	a, b := mixer.Device{Name: "test-mic-a"}, mixer.Device{Name: "test-mic-b"}
	e.audio.snapshot.Devices, e.audio.snapshot.DefaultInput = []mixer.Device{a, b}, a.Name
	e.start()
	e.waitApplied(mixer.SetDeviceMute{Device: a.Name, Muted: false})
	e.send(slider(8, 127))
	e.waitApplied(mixer.SetDeviceVolume{Device: a.Name, Volume: 1})
	e.send(mixer.DefaultInputChanged{Name: b.Name})
	e.waitApplied(mixer.SetDeviceMute{Device: b.Name, Muted: false})
	e.waitApplied(mixer.ReleaseDevice{Device: a.Name})
	e.send(slider(8, 0))
	e.waitApplied(mixer.SetDeviceVolume{Device: b.Name, Volume: 0})
}
