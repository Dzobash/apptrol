package mixer

import "testing"

// FuzzMixer drives the mixer with random event sequences and checks, after
// every event, rules that must always hold (QA-08).
func FuzzMixer(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9})
	f.Add([]byte{3, 0, 3, 1, 3, 0, 4, 2, 9, 9, 3, 0})
	f.Add([]byte{10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21})
	pool := []Stream{spotify, firefox, ffTab2, discord, steam, viber}
	devices := []Device{goxlr, webcam}
	f.Fuzz(func(t *testing.T, data []byte) {
		w := newWorld(t, testSetup(), State{})
		w.do(ControllerConnected{})
		for i := 0; i+1 < len(data); i += 2 {
			op, arg := data[i]%13, int(data[i+1])
			var ev Event
			switch op {
			case 0:
				ev = ControlMoved{Control{Slider, arg%10 - 0}, arg}
			case 1:
				ev = ControlMoved{Control{Knob, arg % 9}, arg}
			case 2:
				ev = ButtonPressed{ButtonM, arg % 10}
			case 3:
				ev = ButtonPressed{ButtonS, arg % 10}
			case 4:
				ev = StreamAdded{pool[arg%len(pool)]}
			case 5:
				ev = StreamRemoved{pool[arg%len(pool)].ID}
			case 6:
				ev = DeviceAdded{devices[arg%2]}
			case 7:
				ev = DeviceRemoved{devices[arg%2].Name}
			case 8:
				s := testSetup()
				if arg%2 == 0 {
					s.Assignments[Control{Slider, 1}], s.Assignments[Control{Slider, 2}] = "browser", "spotify"
				}
				if arg%3 == 0 {
					delete(s.Assignments, Control{Slider, 3})
				}
				ev = ConfigChanged{s}
			case 9:
				ev = AudioSnapshot{Streams: pool[:arg%len(pool)], Devices: devices[:arg%3]}
			case 10:
				ev = ButtonPressed{ButtonR, arg % 10}
			case 11:
				// Muted or unmuted outside Apptrol; the server only reports
				// streams it has, and Apptrol only cares about assigned ones.
				s := pool[arg%len(pool)]
				if info, ok := w.m.streams[s.ID]; !ok || info.target == "" {
					continue
				}
				ev = StreamMuteChanged{ID: s.ID, Muted: arg%2 == 0}
			case 12:
				ev = DeviceMuteChanged{Name: devices[arg%2].Name, Muted: arg%3 == 0}
			}
			w.do(ev)
			checkInvariants(t, w)
		}
	})
}

func checkInvariants(t *testing.T, w *world) {
	t.Helper()
	m := w.m
	// At most one solo LED, and it matches the solo column.
	lit := 0
	for col := 1; col <= NumColumns; col++ {
		id, ok := m.setup.Assignments[Control{Slider, col}]
		if ok && m.setup.Targets[id].Kind == Input {
			continue // input columns light S permanently
		}
		if w.led(ButtonS, col) {
			lit++
			if m.solo != col {
				t.Fatalf("S%d lit but solo = %d", col, m.solo)
			}
		}
	}
	if lit > 1 {
		t.Fatalf("%d solo LEDs lit", lit)
	}
	// Every known stream's audible mute matches the model.
	for id, s := range m.streams {
		want := s.target != "" && m.effectiveMute(s.target)
		if w.muted(id) != want && (s.target != "" || w.muted(id)) {
			t.Fatalf("stream %d (%s) muted=%v, model says %v", id, s.AppName, w.muted(id), want)
		}
	}
	// Unassigned apps are never touched.
	if _, ok := w.streamVol[viber.ID]; ok {
		t.Fatal("viber's volume was changed")
	}
	if w.streamMute[viber.ID] {
		t.Fatal("viber was muted")
	}
}
