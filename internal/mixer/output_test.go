package mixer

import (
	"log/slog"
	"testing"

	"github.com/Dzobash/apptrol/internal/logattr"
)

// Output devices used in the tests (placeholders, not real hardware).
var (
	speakers   = Device{Name: "alsa_output.platform-test_speakers.analog-stereo", Description: "Built-in Audio Analog Stereo"}
	headphones = Device{Name: "alsa_output.usb-Brand_Headset-00.analog-stereo", Description: "USB Headset"}
)

// idleOutputs returns the "output device not on a control" records among
// notices, in order, as device name → description; each must be at debug.
func idleOutputs(t *testing.T, notices []Notice) (names, descriptions []string) {
	t.Helper()
	for _, n := range notices {
		if n.Msg != "output device not on a control" {
			continue
		}
		if n.Level != slog.LevelDebug {
			t.Errorf("%q logged at %v, want debug", n.Msg, n.Level)
		}
		attrs := map[any]any{}
		for i := 0; i+1 < len(n.Attrs); i += 2 {
			attrs[n.Attrs[i]] = n.Attrs[i+1]
		}
		name, _ := attrs[logattr.KeyDeviceName].(string)
		desc, _ := attrs[logattr.KeyDeviceDescription].(string)
		names, descriptions = append(names, name), append(descriptions, desc)
	}
	return names, descriptions
}

// Every output device Apptrol sees is logged at debug with its unique name
// and description, so the log shows what a match list can use (OUT-13,
// LOG-15, ADR 0027).
func TestOUT13_OutputDevicesOnNoControlAreLoggedAtDebug(t *testing.T) {
	w := newWorld(t, testSetup(), State{})
	w.do(AudioSnapshot{Outputs: []Device{headphones, speakers}})
	names, descs := idleOutputs(t, w.notices)
	if len(names) != 2 || names[0] != speakers.Name || names[1] != headphones.Name {
		t.Errorf("logged %v, want both outputs sorted by name", names)
	}
	if len(descs) == 2 && (descs[0] != speakers.Description || descs[1] != headphones.Description) {
		t.Errorf("descriptions = %v", descs)
	}

	w.notices = nil
	dock := Device{Name: "alsa_output.usb-Brand_Dock-00.analog-stereo", Description: "USB Dock"}
	w.do(OutputAdded{Device: dock})
	if names, _ := idleOutputs(t, w.notices); len(names) != 1 || names[0] != dock.Name {
		t.Errorf("a new output logged as %v", names)
	}

	w.notices = nil
	w.do(OutputRemoved{Name: dock.Name})
	if names, _ := idleOutputs(t, w.notices); len(names) != 0 {
		t.Errorf("a removed output logged as %v", names)
	}
}

// The mixer keeps the output devices the audio server reports, and replaces
// them all on a snapshot after a reconnect (OUT-12, SVC-04).
func TestOUT12_MixerKeepsOutputDevices(t *testing.T) {
	w := newWorld(t, testSetup(), State{})
	w.do(AudioSnapshot{Outputs: []Device{speakers}})
	w.do(OutputAdded{Device: headphones})
	if len(w.m.outputs) != 2 {
		t.Fatalf("outputs = %v", w.m.outputs)
	}
	renamed := Device{Name: headphones.Name, Description: "Headset (renamed)"}
	w.do(OutputAdded{Device: renamed})
	if w.m.outputs[headphones.Name] != renamed {
		t.Errorf("renamed output = %v", w.m.outputs[headphones.Name])
	}
	w.do(OutputRemoved{Name: speakers.Name})
	if _, still := w.m.outputs[speakers.Name]; still || len(w.m.outputs) != 1 {
		t.Errorf("after removal: %v", w.m.outputs)
	}
	w.do(AudioSnapshot{Outputs: []Device{speakers}})
	if len(w.m.outputs) != 1 || w.m.outputs[speakers.Name] != speakers {
		t.Errorf("after a snapshot: %v", w.m.outputs)
	}
}
