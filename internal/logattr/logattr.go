// Package logattr names the attributes of Apptrol's log records (ADR 0016).
//
// Names follow the OpenTelemetry semantic conventions: an attribute the
// conventions define is used with its name and meaning (error.type,
// exception.message, file.path, url.full, service.*, process.executable.name);
// everything specific to Apptrol lives in the apptrol.* namespace. Names are
// lower case, dot-separated namespaces, words within a part joined by
// underscores (LOG-11).
//
// Every record carries apptrol.component (LOG-10); every record about an error
// carries error.type and, when there is an error value, exception.message
// (LOG-12).
package logattr

import (
	"log/slog"
	"strings"
)

// Components: the part of Apptrol a record comes from (apptrol.component).
const (
	Service    = "service"    // start, stop, wiring
	Config     = "config"     // configuration file
	State      = "state"      // saved state
	Audio      = "audio"      // audio server connection
	Controller = "controller" // MIDI controller
	Desktop    = "desktop"    // session bus: media players (ADR 0017)
	Mixer      = "mixer"      // volume, mute, solo, matching
)

// Attribute names from the OpenTelemetry semantic conventions.
const (
	KeyErrorType        = "error.type"              // class of error, low cardinality
	KeyExceptionMessage = "exception.message"       // the error's text
	KeyFilePath         = "file.path"               // a file Apptrol reads or writes
	KeyURL              = "url.full"                // a link to help
	KeyServiceName      = "service.name"            // "apptrol"
	KeyServiceVersion   = "service.version"         // Apptrol's version
	KeyExecutableName   = "process.executable.name" // an app's binary
)

// Attribute names specific to Apptrol.
const (
	KeyComponent = "apptrol.component" // see the component constants

	KeyApp         = "apptrol.app.name"              // the app's name from the configuration
	KeyAppID       = "apptrol.app.id"                // the app's id from the configuration
	KeyMatch       = "apptrol.app.match"             // the app's match list, comma-separated
	KeyLayout      = "apptrol.layout"                // the active layout, e.g. default
	KeyControl     = "apptrol.control"               // slider1 … knob8
	KeyAppType     = "apptrol.app.type"              // app or input
	KeyButton      = "apptrol.button"                // a pressed button, e.g. M3 or Cycle
	KeySoloFrom    = "apptrol.solo.previous_control" // where solo was before it moved
	KeySoloFromApp = "apptrol.solo.previous_app"     // the app solo moved away from
	KeyVolume      = "apptrol.volume_percent"        // a volume, in percent of normal
	KeyWanted      = "apptrol.wanted_percent"        // the volume Apptrol set, in percent
	KeyMuted       = "apptrol.muted"                 // a mute state
	KeyWantMut     = "apptrol.wanted_muted"          // the mute state Apptrol set
	KeyAction      = "apptrol.action"                // an action that could not be carried out
	KeyLED         = "apptrol.led"                   // an LED, e.g. S1

	KeyStreamID   = "apptrol.stream.id"       // the audio server's stream index
	KeyStreamName = "apptrol.stream.app_name" // the stream's application.name

	KeyDeviceName        = "apptrol.device.name"        // an input device's unique name
	KeyDeviceDescription = "apptrol.device.description" // an input device's description
	KeyDeviceMatches     = "apptrol.device.matches"     // devices that match, comma-separated

	KeyAudioServer  = "apptrol.audio.server"  // audio server name and version
	KeyRetryDelay   = "apptrol.retry.delay_s" // seconds until the next attempt
	KeyPort         = "apptrol.controller.port"
	KeyPortInUse    = "apptrol.controller.port_in_use"
	KeyMIDIDevice   = "apptrol.controller.device" // e.g. /dev/snd/midiC1D0
	KeySoundCards   = "apptrol.controller.sound_cards"
	KeyMIDICC       = "apptrol.midi.controller"
	KeyMIDIValue    = "apptrol.midi.value"
	KeyMIDIChannel  = "apptrol.midi.channel"
	KeyControls     = "apptrol.config.controls" // number of assigned controls
	KeyWarning      = "apptrol.warning"         // a warning about a file's contents
	KeyPositions    = "apptrol.state.positions" // number of restored positions
	KeyMutes        = "apptrol.state.mutes"     // number of restored mutes
	KeySolo         = "apptrol.state.solo"      // restored soloed control, "" = none
	KeyDisconnected = "apptrol.controller.reason"

	KeyStreamCorked   = "apptrol.stream.corked"     // the stream is paused by its app
	KeyLogLevel       = "apptrol.log.level"         // the log level in use
	KeyLogLevelSource = "apptrol.log.level_source"  // where it comes from: flag or config
	KeyHeldReason     = "apptrol.held.reason"       // why a held state ended without a release
	KeyTalkOver       = "apptrol.talk_over_percent" // the volume apps go down to during talk-over

	KeyButtonMode        = "apptrol.button_mode"         // what a configured button does: a mode, or launcher
	KeyButtonTalkOver    = "apptrol.button_talk_over"    // talk_over = true on an M button
	KeyLauncherDesktopID = "apptrol.launcher.desktop_id" // the app a launcher starts
	KeyLauncherCommand   = "apptrol.launcher.command"    // the command a launcher runs

	KeyBusAddress         = "apptrol.desktop.bus_address" // the session bus address in use
	KeyPlayerBusName      = "apptrol.player.bus_name"     // a media player's bus name
	KeyPlayerIdentity     = "apptrol.player.identity"     // its Identity, a name for people
	KeyPlayerDesktopEntry = "apptrol.player.desktop_entry"
	KeyPlayerStatus       = "apptrol.player.status"   // Playing, Paused or Stopped
	KeyPlayers            = "apptrol.desktop.players" // number of media players found
)

// Error types (error.type): what went wrong, in a few fixed words, so records
// can be filtered by it.
const (
	ErrConfigInvalid      = "config_invalid"
	ErrConfigUnreadable   = "config_unreadable"
	ErrConfigRemoved      = "config_removed"
	ErrExampleNotCreated  = "example_not_created"
	ErrLogSetup           = "log_setup_failed"
	ErrStateUnreadable    = "state_unreadable"
	ErrStateNotSaved      = "state_not_saved"
	ErrAudioUnreachable   = "audio_server_unreachable"
	ErrAudioLost          = "audio_connection_lost"
	ErrAudioApply         = "audio_change_failed"
	ErrControllerBusy     = "controller_busy"
	ErrControllerDenied   = "controller_permission_denied"
	ErrControllerOpen     = "controller_open_failed"
	ErrLEDFailed          = "led_failed"
	ErrDesktopUnreachable = "desktop_bus_unreachable"
	ErrDesktopLost        = "desktop_bus_lost"
	ErrPlayerUnreadable   = "media_player_unreadable"
)

// Component returns the apptrol.component attribute.
func Component(name string) slog.Attr { return slog.String(KeyComponent, name) }

// Error returns the attributes of an error: its type and, if err is not nil,
// its text as exception.message.
func Error(errType string, err error) slog.Attr {
	attrs := []slog.Attr{slog.String(KeyErrorType, errType)}
	if err != nil {
		attrs = append(attrs, slog.String(KeyExceptionMessage, OneLine(err.Error())))
	}
	// An empty group key inlines the attributes into the record (log/slog).
	return slog.Attr{Key: "", Value: slog.GroupValue(attrs...)}
}

// OneLine joins the lines of s with "; " so that every record stays on one
// line (LOG-13). Leading list markers such as "  - " are dropped.
func OneLine(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	var parts []string
	for _, l := range strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' }) {
		l = strings.TrimSpace(l)
		l = strings.TrimSpace(strings.TrimPrefix(l, "- "))
		if l != "" {
			parts = append(parts, l)
		}
	}
	return strings.Join(parts, "; ")
}

// ValidKey reports whether name follows the naming rules (LOG-11): lower-case
// letters, digits and underscores, in dot-separated parts.
func ValidKey(name string) bool {
	if name == "" {
		return false
	}
	for _, part := range strings.Split(name, ".") {
		if part == "" || part[0] == '_' || part[len(part)-1] == '_' {
			return false
		}
		for _, r := range part {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
				return false
			}
		}
	}
	return true
}
