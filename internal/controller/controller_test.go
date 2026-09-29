package controller

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/Dzobash/apptrol/internal/mixer"
)

func parse(b ...byte) []CC {
	var p Parser
	var out []CC
	p.Feed(b, func(cc CC) { out = append(out, cc) })
	return out
}

func TestParser(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want []CC
	}{
		{"one CC", []byte{0xB0, 3, 90}, []CC{{0, 3, 90}}},
		{"channel 16", []byte{0xBF, 1, 2}, []CC{{15, 1, 2}}},
		{"running status", []byte{0xB0, 0, 10, 0, 11, 0, 12}, []CC{{0, 0, 10}, {0, 0, 11}, {0, 0, 12}}},
		{"real-time inside a message", []byte{0xB0, 0xF8, 5, 0xFE, 64}, []CC{{0, 5, 64}}},
		{"sysex is skipped", []byte{0xF0, 0x42, 0x40, 0xB0, 0xF7, 0xB0, 1, 1}, []CC{{0, 1, 1}}},
		{"sysex cancels running status", []byte{0xB0, 1, 1, 0xF0, 0x7E, 0xF7, 2, 2}, []CC{{0, 1, 1}}},
		{"note messages are ignored", []byte{0x90, 60, 100, 0x80, 60, 0, 0xB0, 7, 7}, []CC{{0, 7, 7}}},
		{"program change has one data byte", []byte{0xC0, 5, 0xB0, 1, 2}, []CC{{0, 1, 2}}},
		{"song position does not leave running status", []byte{0xF2, 1, 2, 3, 4}, nil},
		{"tune request", []byte{0xB0, 1, 1, 0xF6, 2, 2}, []CC{{0, 1, 1}}},
		{"data without status", []byte{1, 2, 3, 0xB0, 4, 5}, []CC{{0, 4, 5}}},
		{"status interrupts a message", []byte{0xB0, 1, 0xB1, 2, 3}, []CC{{1, 2, 3}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parse(tt.in...); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParserSplitReads(t *testing.T) {
	// A message may arrive in pieces across reads.
	var p Parser
	var out []CC
	emit := func(cc CC) { out = append(out, cc) }
	p.Feed([]byte{0xB0}, emit)
	p.Feed([]byte{16}, emit)
	p.Feed([]byte{100, 17}, emit)
	p.Feed([]byte{101}, emit)
	want := []CC{{0, 16, 100}, {0, 17, 101}}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got %v, want %v", out, want)
	}
}

func TestEncode(t *testing.T) {
	if got := (CC{Channel: 2, Controller: 48, Value: 127}).Encode(); !bytes.Equal(got, []byte{0xB2, 48, 127}) {
		t.Errorf("Encode = % x", got)
	}
	// Out-of-range values are masked, never producing a status byte.
	if got := (CC{Channel: 17, Controller: 200, Value: 255}).Encode(); got[1] >= 0x80 || got[2] >= 0x80 || got[0] != 0xB1 {
		t.Errorf("Encode = % x", got)
	}
}

func TestHW01_Decode(t *testing.T) {
	m := NanoKONTROL2
	tests := []struct {
		cc   CC
		want mixer.Event
	}{
		{CC{0, 0, 90}, mixer.ControlMoved{Control: mixer.Control{Kind: mixer.Slider, Column: 1}, Value: 90}},
		{CC{0, 7, 0}, mixer.ControlMoved{Control: mixer.Control{Kind: mixer.Slider, Column: 8}, Value: 0}},
		{CC{0, 16, 127}, mixer.ControlMoved{Control: mixer.Control{Kind: mixer.Knob, Column: 1}, Value: 127}},
		{CC{0, 23, 5}, mixer.ControlMoved{Control: mixer.Control{Kind: mixer.Knob, Column: 8}, Value: 5}},
		{CC{0, 32, 127}, mixer.ButtonPressed{Button: mixer.ButtonS, Column: 1}},
		{CC{0, 55, 127}, mixer.ButtonPressed{Button: mixer.ButtonM, Column: 8}},
		{CC{0, 66, 127}, mixer.ButtonPressed{Button: mixer.ButtonR, Column: 3}},
		{CC{0, 58, 127}, mixer.TransportPressed{Button: mixer.TrackPrev}},
		{CC{0, 45, 127}, mixer.TransportPressed{Button: mixer.Record}},
		{CC{5, 1, 64}, mixer.ControlMoved{Control: mixer.Control{Kind: mixer.Slider, Column: 2}, Value: 64}}, // any channel
	}
	for _, tt := range tests {
		got, ok := m.Decode(tt.cc)
		if !ok || got != tt.want {
			t.Errorf("Decode(%v) = %v, %v; want %v", tt.cc, got, ok, tt.want)
		}
	}
	for _, cc := range []CC{{0, 32, 0}, {0, 58, 0}, {0, 8, 10}, {0, 100, 127}} {
		if ev, ok := m.Decode(cc); ok {
			t.Errorf("Decode(%v) = %v, want nothing (release or unknown)", cc, ev)
		}
	}
}

func TestHW04_EveryControlHasItsOwnCC(t *testing.T) {
	m := NanoKONTROL2
	want := 5*mixer.NumColumns + len(mixer.AllTransport)
	if len(m.lookup) != want {
		t.Errorf("%d distinct CC numbers, want %d (a number is used twice)", len(m.lookup), want)
	}
	for _, b := range mixer.AllTransport {
		if _, ok := m.Transport[b]; !ok {
			t.Errorf("transport button %v has no CC", b)
		}
	}
}

func TestHW02_LED(t *testing.T) {
	m := NanoKONTROL2
	tests := []struct {
		led  mixer.LED
		on   bool
		want CC
	}{
		{mixer.LED{Button: mixer.ButtonS, Column: 1}, true, CC{0, 32, 127}},
		{mixer.LED{Button: mixer.ButtonM, Column: 8}, false, CC{0, 55, 0}},
		{mixer.LED{Button: mixer.ButtonR, Column: 4}, true, CC{0, 67, 127}},
		{mixer.LED{Transport: mixer.Play}, false, CC{0, 41, 0}},
	}
	for _, tt := range tests {
		got, ok := m.LED(tt.led, tt.on, 0)
		if !ok || got != tt.want {
			t.Errorf("LED(%v, %v) = %v, %v; want %v", tt.led, tt.on, got, ok, tt.want)
		}
	}
	if cc, _ := m.LED(mixer.LED{Button: mixer.ButtonS, Column: 1}, true, 3); cc.Channel != 3 {
		t.Errorf("channel not used: %v", cc)
	}
	for _, l := range []mixer.LED{{Column: 0}, {Column: 9}, {Button: 7, Column: 1}, {Transport: 99},
		{Transport: mixer.TrackPrev}, {Transport: mixer.MarkerSet}} {
		if cc, ok := m.LED(l, true, 0); ok {
			t.Errorf("LED(%+v) = %v, want none", l, cc)
		}
	}
}

func TestHW02_WhichButtonsHaveLEDs(t *testing.T) {
	m := NanoKONTROL2
	var with []string
	for _, b := range mixer.AllTransport {
		if m.HasLED(mixer.LED{Transport: b}) {
			with = append(with, b.String())
		}
	}
	if want := []string{"cycle", "◀◀", "▶▶", "■", "▶", "●"}; strings.Join(with, " ") != strings.Join(want, " ") {
		t.Errorf("transport LEDs %v, want %v", with, want)
	}
	if !m.HasLED(mixer.LED{Button: mixer.ButtonR, Column: 8}) {
		t.Error("R8 has an LED")
	}
}

func FuzzParser(f *testing.F) {
	f.Add([]byte{0xB0, 0, 10, 0, 11})
	f.Add([]byte{0xF0, 1, 2, 0xF7, 0xB5, 3, 4})
	f.Add([]byte{0xF2, 1, 2, 0xF8, 0xB0, 1})
	f.Fuzz(func(t *testing.T, data []byte) {
		var p Parser
		var n int
		p.Feed(data, func(cc CC) {
			n++
			if cc.Channel > 15 || cc.Controller > 127 || cc.Value > 127 {
				t.Fatalf("invalid message %v", cc)
			}
			// Every decoded message encodes and parses back to itself.
			if got := parse(cc.Encode()...); len(got) != 1 || got[0] != cc {
				t.Fatalf("round trip of %v gave %v", cc, got)
			}
			NanoKONTROL2.Decode(cc)
		})
		if n > len(data)/2 {
			t.Fatalf("%d messages from %d bytes", n, len(data))
		}
	})
}
