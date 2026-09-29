package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/Dzobash/apptrol/internal/config"
	"github.com/Dzobash/apptrol/internal/mixer"
)

// device is the controller as `apptrol test` uses it.
type device interface {
	Run(ctx context.Context, out chan<- mixer.Event) error
	SetLED(l mixer.LED, on bool) error
	HasLED(l mixer.LED) bool
}

// allLEDs lists every LED of the controller.
func allLEDs() []mixer.LED {
	var leds []mixer.LED
	for col := 1; col <= mixer.NumColumns; col++ {
		for _, b := range []mixer.ButtonKind{mixer.ButtonS, mixer.ButtonM, mixer.ButtonR} {
			leds = append(leds, mixer.LED{Button: b, Column: col})
		}
	}
	for _, t := range mixer.AllTransport {
		leds = append(leds, mixer.LED{Transport: t})
	}
	return leds
}

// testPort returns the controller's card id from the configuration, or the
// default when there is no valid configuration.
func testPort(configPath string) string {
	if path, err := resolveConfigPath(configPath); err == nil {
		if cfg, _, err := config.Load(path); err == nil {
			return cfg.Controller.Port
		}
	}
	return "nanoKONTROL2"
}

// cmdTest shows what the controller sends and toggles the LED of every button
// pressed, to check the controller and its settings (HW-07). It runs until ctx
// is canceled (Ctrl+C) and turns the LEDs off before it stops.
func cmdTest(ctx context.Context, port string, stdout io.Writer, log *slog.Logger, newDevice func(*slog.Logger, string) device) error {
	fmt.Fprintf(stdout, "Looking for the controller (sound card id %q).\n", port)
	fmt.Fprintln(stdout, "Move sliders and knobs and press buttons. Each press turns the button's LED on or off.")
	fmt.Fprintln(stdout, "Press Ctrl+C to stop.")
	fmt.Fprintln(stdout)

	d := newDevice(log, port)
	devCtx, stopDevice := context.WithCancel(context.Background())
	defer stopDevice()
	events := make(chan mixer.Event, 64)
	done := make(chan error, 1)
	go func() { done <- d.Run(devCtx, events) }()

	lit := map[mixer.LED]bool{}
	hinted := false
	setAll := func(on bool) {
		for _, l := range allLEDs() {
			if d.HasLED(l) {
				_ = d.SetLED(l, on)
			}
		}
	}
	toggle := func(l mixer.LED, name string) {
		if !d.HasLED(l) {
			fmt.Fprintf(stdout, "%-12s pressed  (this button has no LED)\n", name)
			return
		}
		lit[l] = !lit[l]
		state := "off"
		if lit[l] {
			state = "on"
		}
		if err := d.SetLED(l, lit[l]); err != nil {
			state += fmt.Sprintf(" (could not send: %v)", err)
		}
		fmt.Fprintf(stdout, "%-12s pressed  LED %s\n", name, state)
		if !hinted {
			hinted = true
			fmt.Fprintln(stdout, "             If the LED lights only while you hold the button, the controller's")
			fmt.Fprintln(stdout, "             LED mode is Internal. Set it to External, see the README.")
		}
	}

	for {
		select {
		case <-ctx.Done():
			setAll(false)
			stopDevice()
			<-done
			fmt.Fprintln(stdout, "\nStopped.")
			return nil
		case ev := <-events:
			switch e := ev.(type) {
			case mixer.ControllerConnected:
				lit = map[mixer.LED]bool{}
				setAll(false)
				fmt.Fprintln(stdout, "Controller connected. All LEDs are off.")
			case mixer.ControlMoved:
				fmt.Fprintf(stdout, "%-12s %3d  (%d %%)\n", e.Control, e.Value, mixer.Percent(e.Value))
			case mixer.ButtonPressed:
				toggle(mixer.LED{Button: e.Button, Column: e.Column}, fmt.Sprintf("%s%d", e.Button, e.Column))
			case mixer.TransportPressed:
				toggle(mixer.LED{Transport: e.Button}, e.Button.String())
			}
		}
	}
}
