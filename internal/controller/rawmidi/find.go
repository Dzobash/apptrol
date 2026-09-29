// Package rawmidi reads the controller through the Linux raw MIDI device
// (/dev/snd/midiC<card>D<device>) in pure Go, finds it by its ALSA card id
// (HW-05), and follows plugging and unplugging (SVC-03).
package rawmidi

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ErrNotFound means no sound card with the configured id has a MIDI device.
var ErrNotFound = errors.New("controller not found")

// Paths where the kernel describes sound cards and their devices.
const (
	DefaultProcDir = "/proc/asound"
	DefaultDevDir  = "/dev/snd"
)

// Find returns the raw MIDI device of the card whose ALSA id equals port,
// ignoring case (HW-05). The id is the name in brackets in
// /proc/asound/cards, e.g. "nanoKONTROL2". The card number is never assumed:
// it changes with the order devices are plugged in.
func Find(procDir, devDir, port string) (path string, err error) {
	cards, err := filepath.Glob(filepath.Join(procDir, "card[0-9]*", "id"))
	if err != nil {
		return "", err
	}
	for _, idFile := range cards {
		b, err := os.ReadFile(idFile)
		if err != nil || !strings.EqualFold(strings.TrimSpace(string(b)), port) {
			continue
		}
		card := strings.TrimPrefix(filepath.Base(filepath.Dir(idFile)), "card")
		if _, err := strconv.Atoi(card); err != nil {
			continue
		}
		if dev := firstMIDIDevice(devDir, card); dev != "" {
			return dev, nil
		}
	}
	return "", fmt.Errorf("%w: no sound card with id %q has a MIDI device", ErrNotFound, port)
}

// firstMIDIDevice returns /dev/snd/midiC<card>D<n> with the lowest n.
func firstMIDIDevice(devDir, card string) string {
	prefix := "midiC" + card + "D"
	matches, _ := filepath.Glob(filepath.Join(devDir, prefix+"*"))
	best, bestN := "", -1
	for _, m := range matches {
		n, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(m), prefix))
		if err != nil {
			continue
		}
		if bestN < 0 || n < bestN {
			best, bestN = m, n
		}
	}
	return best
}

// Cards returns the ids of all sound cards, for the "not found" message.
func Cards(procDir string) []string {
	files, _ := filepath.Glob(filepath.Join(procDir, "card[0-9]*", "id"))
	var ids []string
	for _, f := range files {
		if b, err := os.ReadFile(f); err == nil {
			ids = append(ids, strings.TrimSpace(string(b)))
		}
	}
	sort.Strings(ids)
	return ids
}
