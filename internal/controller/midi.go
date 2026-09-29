// Package controller decodes the controller's MIDI messages into mixer events
// and encodes LED changes as MIDI messages. The nanoKONTROL2's CC numbers are
// defined in one place (HW-04), see nanokontrol2.go. Reading and writing the
// device itself is in the rawmidi subpackage.
package controller

// CC is a MIDI Control Change message.
type CC struct {
	Channel    byte // 0–15 (MIDI channel 1–16)
	Controller byte // 0–127
	Value      byte // 0–127
}

// Parser splits a raw MIDI byte stream into messages and reports Control
// Change messages. It handles running status (a status byte that is not
// repeated for following messages), real-time bytes that may appear anywhere,
// and System Exclusive blocks, which are skipped. The zero value is ready to use.
type Parser struct {
	status byte    // current status; 0 = none (data bytes are ignored)
	data   [2]byte // data bytes collected for the current message
	n      int     // number of data bytes collected
	sysex  bool    // inside a System Exclusive block
}

// dataLen returns how many data bytes follow a status byte.
func dataLen(status byte) int {
	switch status & 0xF0 {
	case 0xC0, 0xD0: // program change, channel pressure
		return 1
	case 0x80, 0x90, 0xA0, 0xB0, 0xE0:
		return 2
	}
	switch status {
	case 0xF1, 0xF3: // time code quarter frame, song select
		return 1
	case 0xF2: // song position
		return 2
	}
	return 0
}

// Feed processes bytes and calls emit for every complete Control Change.
func (p *Parser) Feed(b []byte, emit func(CC)) {
	for _, c := range b {
		switch {
		case c >= 0xF8:
			// Real-time (clock, start, stop, active sensing …): may appear in
			// the middle of other messages and changes nothing.
		case c == 0xF0:
			p.sysex, p.status, p.n = true, 0, 0
		case c == 0xF7:
			p.sysex = false
		case c >= 0x80:
			p.sysex, p.status, p.n = false, c, 0
			if dataLen(c) == 0 {
				p.status = 0 // tune request and undefined: no data, no running status
			}
		case p.sysex || p.status == 0:
			// Data byte without a status: skip it.
		default:
			p.data[p.n] = c
			p.n++
			if p.n < dataLen(p.status) {
				continue
			}
			p.n = 0
			if p.status&0xF0 == 0xB0 {
				emit(CC{Channel: p.status & 0x0F, Controller: p.data[0], Value: p.data[1]})
			}
			if p.status >= 0xF0 {
				p.status = 0 // system messages do not use running status
			}
		}
	}
}

// Encode returns the three bytes of a Control Change message.
func (cc CC) Encode() []byte {
	return []byte{0xB0 | cc.Channel&0x0F, cc.Controller & 0x7F, cc.Value & 0x7F}
}
