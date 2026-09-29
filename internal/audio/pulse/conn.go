// Package pulse connects Apptrol to the audio server through the PulseAudio
// protocol. PipeWire serves this protocol through pipewire-pulse (ADR 0003),
// and plain PulseAudio speaks it natively (NFR-01).
//
// The package tracks playback streams ("sink inputs") and capture devices
// ("sources"), turns changes into mixer events, and carries out the mixer's
// volume and mute actions. Backend reconnects on its own when the server
// restarts (SVC-04). List reads everything once for `apptrol list` (CFG-10).
package pulse

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jfreymuth/pulse/proto"

	"github.com/Dzobash/apptrol/internal/mixer"
	"github.com/Dzobash/apptrol/internal/version"
)

// requestTimeout bounds every request to the server.
const requestTimeout = 2 * time.Second

// eventBuffer is how many change notifications may queue up before the
// backend stops tracking single changes and reads everything again.
const eventBuffer = 1024

// conn is one connection to the audio server.
type conn struct {
	c     *proto.Client
	close func() error

	events   chan proto.SubscribeEvent
	overflow atomic.Bool // events were dropped; a full re-read is needed

	closed    chan struct{} // closed when the connection is lost or closed
	closeOnce sync.Once
}

// dial connects to the server. An empty server means the default: the
// PULSE_SERVER variable, or the socket in $XDG_RUNTIME_DIR/pulse/.
func dial(server string) (*conn, error) {
	c, nc, err := proto.Connect(server)
	if err != nil {
		return nil, fmt.Errorf("connecting to the audio server: %w", err)
	}
	k := &conn{
		c:      c,
		close:  nc.Close,
		events: make(chan proto.SubscribeEvent, eventBuffer),
		closed: make(chan struct{}),
	}
	c.SetTimeout(requestTimeout)
	// The callback runs on the library's read goroutine. It must never block
	// or make requests itself, so it only queues events.
	c.Callback = k.callback

	ver, _, _ := version.Info()
	props := proto.PropList{
		"application.name":           proto.PropListString("Apptrol"),
		"application.id":             proto.PropListString("io.github.dzobash.apptrol"),
		"application.version":        proto.PropListString(ver),
		"application.icon_name":      proto.PropListString("apptrol"),
		"application.process.id":     proto.PropListString(fmt.Sprint(os.Getpid())),
		"application.process.binary": proto.PropListString("apptrol"),
	}
	if err := k.request(&proto.SetClientName{Props: props}, &proto.SetClientNameReply{}); err != nil {
		k.shutdown()
		return nil, fmt.Errorf("connecting to the audio server: %w", err)
	}
	return k, nil
}

func (k *conn) callback(msg interface{}) {
	switch msg := msg.(type) {
	case *proto.SubscribeEvent:
		select {
		case k.events <- *msg:
		default:
			k.overflow.Store(true)
		}
	case *proto.ConnectionClosed:
		k.lost()
	}
}

// request sends one request. An error that is not an answer from the server
// (a broken socket, a timeout) marks the connection as lost.
func (k *conn) request(req proto.RequestArgs, rpl proto.Reply) error {
	err := k.c.Request(req, rpl)
	if err != nil && !isServerError(err) {
		k.lost()
	}
	return err
}

func (k *conn) lost() { k.closeOnce.Do(func() { close(k.closed) }) }

func (k *conn) shutdown() {
	_ = k.close()
	k.lost()
}

// isServerError reports whether err is an error code sent by the server, as
// opposed to a failure of the connection itself.
func isServerError(err error) bool {
	var pe proto.Error
	return errors.As(err, &pe)
}

// IsGone reports whether err means the stream or device no longer exists. It
// happens when an app stops just as its volume is set, and is harmless.
func IsGone(err error) bool { return errors.Is(err, proto.ErrNoSuchEntity) }

func (k *conn) subscribe() error {
	return k.request(&proto.Subscribe{Mask: proto.SubscriptionMaskSinkInput | proto.SubscriptionMaskSource}, nil)
}

func (k *conn) serverInfo() (*proto.GetServerInfoReply, error) {
	var r proto.GetServerInfoReply
	return &r, k.request(&proto.GetServerInfo{}, &r)
}

func (k *conn) streams() ([]StreamInfo, error) {
	var r proto.GetSinkInputInfoListReply
	if err := k.request(&proto.GetSinkInputInfoList{}, &r); err != nil {
		return nil, err
	}
	out := make([]StreamInfo, 0, len(r))
	for _, s := range r {
		out = append(out, streamFromReply(s))
	}
	return out, nil
}

func (k *conn) stream(index uint32) (StreamInfo, error) {
	var r proto.GetSinkInputInfoReply
	if err := k.request(&proto.GetSinkInputInfo{SinkInputIndex: index}, &r); err != nil {
		return StreamInfo{}, err
	}
	return streamFromReply(&r), nil
}

// sources returns the capture devices, without monitors of outputs.
func (k *conn) sources() ([]SourceInfo, error) {
	var r proto.GetSourceInfoListReply
	if err := k.request(&proto.GetSourceInfoList{}, &r); err != nil {
		return nil, err
	}
	out := make([]SourceInfo, 0, len(r))
	for _, s := range r {
		if info, ok := sourceFromReply(s); ok {
			out = append(out, info)
		}
	}
	return out, nil
}

// source returns one capture device; ok is false for a monitor.
func (k *conn) source(index uint32) (info SourceInfo, ok bool, err error) {
	var r proto.GetSourceInfoReply
	if err := k.request(&proto.GetSourceInfo{SourceIndex: index}, &r); err != nil {
		return SourceInfo{}, false, err
	}
	info, ok = sourceFromReply(&r)
	return info, ok, nil
}

func (k *conn) sourceByName(name string) (info SourceInfo, ok bool, err error) {
	var r proto.GetSourceInfoReply
	if err := k.request(&proto.GetSourceInfo{SourceIndex: proto.Undefined, SourceName: name}, &r); err != nil {
		return SourceInfo{}, false, err
	}
	info, ok = sourceFromReply(&r)
	return info, ok, nil
}

func (k *conn) sinkNames() (map[uint32]string, error) {
	var r proto.GetSinkInfoListReply
	if err := k.request(&proto.GetSinkInfoList{}, &r); err != nil {
		return nil, err
	}
	names := make(map[uint32]string, len(r))
	for _, s := range r {
		names[s.SinkIndex] = prop(s.Properties, "device.description", s.SinkName)
	}
	return names, nil
}

func (k *conn) setStreamVolume(index uint32, channels int, v float64) error {
	return k.request(&proto.SetSinkInputVolume{SinkInputIndex: index, ChannelVolumes: channelVolumes(channels, v)}, nil)
}

func (k *conn) setStreamMute(index uint32, muted bool) error {
	return k.request(&proto.SetSinkInputMute{SinkInputIndex: index, Mute: muted}, nil)
}

func (k *conn) setSourceVolume(name string, channels int, v float64) error {
	return k.request(&proto.SetSourceVolume{SourceIndex: proto.Undefined, SourceName: name,
		ChannelVolumes: channelVolumes(channels, v)}, nil)
}

func (k *conn) setSourceMute(name string, muted bool) error {
	return k.request(&proto.SetSourceMute{SourceIndex: proto.Undefined, SourceName: name, Mute: muted}, nil)
}

// Snapshot converts listed streams and sources into the mixer's event.
func Snapshot(streams []StreamInfo, sources []SourceInfo) mixer.AudioSnapshot {
	ev := mixer.AudioSnapshot{Streams: make([]mixer.Stream, 0, len(streams)), Devices: make([]mixer.Device, 0, len(sources))}
	for _, s := range streams {
		ev.Streams = append(ev.Streams, s.Stream)
	}
	for _, d := range sources {
		ev.Devices = append(ev.Devices, d.Device)
	}
	return ev
}
