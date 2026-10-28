package pulse

import (
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/Dzobash/apptrol/internal/mixer"
)

// TestIntegration_OUT14_OutputBalance checks actual per-channel reads on
// active silent stereo and surround outputs (OUT-14 to OUT-16, ADR 0039).
func TestIntegration_OUT14_OutputBalance(t *testing.T) {
	needServer(t)
	for _, positions := range [][]string{
		{"front-left", "front-right"},
		{"front-left", "front-right", "rear-left", "rear-right", "front-center", "lfe"},
	} {
		t.Run(fmt.Sprintf("%d_channels", len(positions)), func(t *testing.T) {
			b, name := newOutput(t, "apptrol_test_balance", positions...)
			// A server may initialize surround channels at zero. Establish the
			// synthetic starting vector, and wait until the adapter observed it
			// before moving the control (OUT-15, ADR 0039).
			initial := []string{"set-sink-volume", name}
			for range positions {
				initial = append(initial, "100%")
			}
			if output, err := exec.Command("pactl", initial...).CombinedOutput(); err != nil {
				t.Fatalf("seed channels: %v %s", err, output)
			}
			for deadline := time.Now().Add(time.Second); ; time.Sleep(time.Millisecond) {
				b.mu.Lock()
				s := b.outBalance[name]
				ready := s != nil && len(s.ratios) == len(positions)
				if ready {
					for _, ratio := range s.ratios {
						ready = ready && ratio == 1
					}
				}
				b.mu.Unlock()
				if ready {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("adapter did not observe initial equal channels")
				}
			}
			move := func(level float64) {
				t.Helper()
				if err := b.Apply(mixer.SetOutputVolume{Device: name, Volume: level}); err != nil {
					t.Fatal(err)
				}
			}
			want := []float64{0.5, 0.375, 0.25, 0.25, 0.125, 0}[:len(positions)]
			read := func(levels []float64) {
				t.Helper()
				waitSink(t, name, func(s SinkInfo) bool {
					if len(s.Volumes) != len(levels) {
						return false
					}
					for i, v := range levels {
						if !near(s.Volumes[i].Norm(), v) {
							return false
						}
					}
					return true
				})
			}
			move(0.5)
			flat := make([]float64, len(positions))
			for i := range flat {
				flat[i] = 0.5
			}
			read(flat)
			b.mu.Lock()
			until := b.outGuard[name].until
			b.mu.Unlock()
			if time.Until(until) < time.Second {
				t.Fatal("server too slow to exercise new-output guard")
			}
			args := append([]string{"set-sink-volume", name}, []string{"80%", "60%", "40%", "40%", "20%", "0%"}[:len(positions)]...)
			if output, err := exec.Command("pactl", args...).CombinedOutput(); err != nil {
				t.Fatalf("set synthetic balance: %v %s", err, output)
			}
			read(want) // guard corrects level and accepts the restored balance
			move(0)
			read(make([]float64, len(positions)))
			move(0.5)
			read(want)
			// Repeated real echoes must keep the same nonzero proportions.
			for range 3 {
				move(0.1)
				quiet := make([]float64, len(want))
				for i := range quiet {
					quiet[i] = want[i] / 5
				}
				read(quiet)
				move(0.5)
				read(want)
			}
		})
	}
}
