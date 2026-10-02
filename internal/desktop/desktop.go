// Package desktop is the adapter for the D-Bus session bus (ADR 0017): it
// finds the media players (MPRIS, ADR 0018) and follows them, as events for
// the mixer. Which player belongs to which control is the mixer's decision.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/Dzobash/apptrol/internal/logattr"
	"github.com/Dzobash/apptrol/internal/mixer"
)

// MPRIS names (https://specifications.freedesktop.org/mpris-spec/latest/).
const (
	mprisPrefix = "org.mpris.MediaPlayer2."
	mprisPath   = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	ifaceRoot   = "org.mpris.MediaPlayer2"
	ifacePlayer = "org.mpris.MediaPlayer2.Player"
	ifaceProps  = "org.freedesktop.DBus.Properties"
	busName     = "org.freedesktop.DBus"
)

// callTimeout bounds every call to a player, so a hanging player cannot
// stop Apptrol from following the others.
const callTimeout = 2 * time.Second

// Bus keeps a connection to the session bus and reports media players.
type Bus struct {
	log     *slog.Logger
	address string // "" = from the environment (busAddress)
	// Wait before reconnecting: starts at retryMin, doubles up to retryMax.
	retryMin, retryMax time.Duration

	mu   sync.Mutex
	conn *dbus.Conn // the current connection, nil while there is none
}

// ErrNotConnected is returned by Apply while there is no session bus.
var ErrNotConnected = errors.New("not connected to the session bus")

func (b *Bus) setConn(c *dbus.Conn) {
	b.mu.Lock()
	b.conn = c
	b.mu.Unlock()
}

// Apply carries out a mixer action: a PlayerCommand is sent to its player
// without waiting for the answer, so a slow or hanging player cannot hold up
// the controller. A player that refuses is logged when its answer arrives.
func (b *Bus) Apply(a mixer.Action) error {
	cmd, ok := a.(mixer.PlayerCommand)
	if !ok {
		return fmt.Errorf("desktop: unsupported action %T", a)
	}
	b.mu.Lock()
	conn := b.conn
	b.mu.Unlock()
	if conn == nil {
		return ErrNotConnected
	}
	ctx, cancel := context.WithTimeout(conn.Context(), callTimeout)
	done := make(chan *dbus.Call, 1)
	// FlagNoAutoStart: a player that has just gone is not started (DESK-03).
	conn.Object(cmd.BusName, mprisPath).GoWithContext(ctx, ifacePlayer+"."+cmd.Command, dbus.FlagNoAutoStart, done)
	go func() {
		defer cancel()
		if call := <-done; call.Err != nil {
			b.log.Error("media player command failed", logattr.KeyPlayerBusName, cmd.BusName,
				logattr.KeyPlayerCommand, cmd.Command, logattr.Error(logattr.ErrMediaCommand, call.Err))
		}
	}()
	return nil
}

// New returns a Bus. address is the session bus address; "" finds it as a
// desktop session does (busAddress).
func New(log *slog.Logger, address string) *Bus {
	return &Bus{log: log.With(logattr.Component(logattr.Desktop)), address: address,
		retryMin: 500 * time.Millisecond, retryMax: 30 * time.Second}
}

// busAddress returns the session bus address: DBUS_SESSION_BUS_ADDRESS, or
// the standard socket in XDG_RUNTIME_DIR. It never starts a bus.
func (b *Bus) busAddress() (string, error) {
	if b.address != "" {
		return b.address, nil
	}
	if a := os.Getenv("DBUS_SESSION_BUS_ADDRESS"); a != "" {
		return a, nil
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		socket := filepath.Join(dir, "bus")
		if _, err := os.Stat(socket); err == nil {
			return "unix:path=" + socket, nil
		}
	}
	return "", errors.New("no session bus: DBUS_SESSION_BUS_ADDRESS is not set and $XDG_RUNTIME_DIR/bus does not exist")
}

// Run connects and keeps the connection until ctx is canceled. After each
// (re)connect it sends a mixer.PlayerSnapshot, then single changes:
// PlayerAdded, PlayerRemoved, PlayerStatusChanged. Without a bus, or when the
// connection breaks, it logs it and reconnects (DESK-02). It returns ctx's error.
func (b *Bus) Run(ctx context.Context, out chan<- mixer.Event) error {
	wait := b.retryMin
	failed := false // the last attempt failed; don't repeat the error log
	for {
		addr, err := b.busAddress()
		var conn *dbus.Conn
		if err == nil {
			conn, err = dbus.Connect(addr, dbus.WithContext(ctx))
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !failed {
				b.log.Error("cannot connect to the session bus; media players are not available, retrying",
					logattr.Error(logattr.ErrDesktopUnreachable, err))
				failed = true
			} else {
				b.log.Debug("session bus still unreachable", logattr.Error(logattr.ErrDesktopUnreachable, err),
					logattr.KeyRetryDelay, wait.Seconds())
			}
		} else {
			wait = b.retryMin
			b.log.Info("connected to the session bus", logattr.KeyBusAddress, addr)
			b.setConn(conn)
			err = b.session(ctx, conn, out)
			b.setConn(nil)
			_ = conn.Close()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			b.log.Error("lost the connection to the session bus; reconnecting", logattr.Error(logattr.ErrDesktopLost, err))
			failed = true // already reported; retries are logged at debug level
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		wait = min(wait*2, b.retryMax)
	}
}

// session follows the players on one connection until it breaks or ctx ends.
func (b *Bus) session(ctx context.Context, conn *dbus.Conn, out chan<- mixer.Event) error {
	signals := make(chan *dbus.Signal, 64)
	conn.Signal(signals)
	defer conn.RemoveSignal(signals)

	// Subscribe before listing, so nothing that happens in between is missed.
	if err := conn.AddMatchSignalContext(ctx,
		dbus.WithMatchSender(busName), dbus.WithMatchInterface(busName), dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg0Namespace(strings.TrimSuffix(mprisPrefix, "."))); err != nil {
		return fmt.Errorf("subscribe to players appearing: %w", err)
	}
	if err := conn.AddMatchSignalContext(ctx,
		dbus.WithMatchObjectPath(mprisPath), dbus.WithMatchInterface(ifaceProps), dbus.WithMatchMember("PropertiesChanged"),
		dbus.WithMatchArg(0, ifacePlayer)); err != nil {
		return fmt.Errorf("subscribe to playback changes: %w", err)
	}

	t := &tracker{b: b, conn: conn, ctx: ctx, owners: map[string]string{}, players: map[string]mixer.Player{}}
	snapshot, err := t.list()
	if err != nil {
		return err
	}
	b.log.Info("media players found", logattr.KeyPlayers, len(snapshot))
	if !send(ctx, out, mixer.PlayerSnapshot{Players: snapshot}) {
		return ctx.Err()
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-conn.Context().Done():
			return errors.New("the session bus closed the connection")
		case sig, ok := <-signals:
			if !ok {
				return errors.New("the session bus closed the connection")
			}
			for _, ev := range t.signal(sig) {
				if !send(ctx, out, ev) {
					return ctx.Err()
				}
			}
		}
	}
}

func send(ctx context.Context, out chan<- mixer.Event, ev mixer.Event) bool {
	select {
	case out <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

// tracker remembers the players of one connection, so signals become events.
type tracker struct {
	b       *Bus
	conn    *dbus.Conn
	ctx     context.Context
	owners  map[string]string       // unique connection name -> player bus name
	players map[string]mixer.Player // by bus name
}

// list finds every player with a running owner (MEDIA-01). Names that are
// only activatable are not listed by ListNames, so none is started (DESK-03).
func (t *tracker) list() ([]mixer.Player, error) {
	var names []string
	ctx, cancel := context.WithTimeout(t.ctx, callTimeout)
	defer cancel()
	if err := t.conn.BusObject().CallWithContext(ctx, busName+".ListNames", 0).Store(&names); err != nil {
		return nil, fmt.Errorf("list bus names: %w", err)
	}
	sort.Strings(names)
	var out []mixer.Player
	for _, name := range names {
		if !strings.HasPrefix(name, mprisPrefix) {
			continue
		}
		var owner string
		if err := t.conn.BusObject().CallWithContext(ctx, busName+".GetNameOwner", 0, name).Store(&owner); err != nil {
			continue // gone in the meantime
		}
		if p, ok := t.add(name, owner); ok {
			out = append(out, p)
		}
	}
	return out, nil
}

// add reads a player's properties and remembers it. A player that does not
// answer is logged and left out.
func (t *tracker) add(name, owner string) (mixer.Player, bool) {
	p := mixer.Player{BusName: name}
	obj := t.conn.Object(name, mprisPath)
	ctx, cancel := context.WithTimeout(t.ctx, callTimeout)
	defer cancel()
	get := func(iface, prop string) (string, error) {
		var v dbus.Variant
		err := obj.CallWithContext(ctx, ifaceProps+".Get", dbus.FlagNoAutoStart, iface, prop).Store(&v)
		if err != nil {
			return "", err
		}
		s, _ := v.Value().(string)
		return s, nil
	}
	var err error
	if p.Identity, err = get(ifaceRoot, "Identity"); err != nil {
		t.b.log.Warn("media player does not answer; ignoring it", logattr.KeyPlayerBusName, name,
			logattr.Error(logattr.ErrPlayerUnreadable, err))
		return p, false
	}
	p.DesktopEntry, _ = get(ifaceRoot, "DesktopEntry") // optional in MPRIS
	p.Status, _ = get(ifacePlayer, "PlaybackStatus")
	t.owners[owner] = name
	t.players[name] = p
	attrs := []any{logattr.KeyPlayerBusName, name, logattr.KeyPlayerIdentity, p.Identity}
	if p.DesktopEntry != "" {
		attrs = append(attrs, logattr.KeyPlayerDesktopEntry, p.DesktopEntry)
	}
	t.b.log.Debug("media player found", append(attrs, logattr.KeyPlayerStatus, p.Status)...)
	return p, true
}

// remove forgets a player and returns the event for it.
func (t *tracker) remove(name string) []mixer.Event {
	if _, known := t.players[name]; !known {
		return nil
	}
	delete(t.players, name)
	for owner, n := range t.owners {
		if n == name {
			delete(t.owners, owner)
		}
	}
	t.b.log.Debug("media player gone", logattr.KeyPlayerBusName, name)
	return []mixer.Event{mixer.PlayerRemoved{BusName: name}}
}

// signal turns one D-Bus signal into mixer events.
func (t *tracker) signal(sig *dbus.Signal) []mixer.Event {
	switch sig.Name {
	case busName + ".NameOwnerChanged":
		var name, oldOwner, newOwner string
		if dbus.Store(sig.Body, &name, &oldOwner, &newOwner) != nil || !strings.HasPrefix(name, mprisPrefix) {
			return nil
		}
		var evs []mixer.Event
		if oldOwner != "" {
			evs = t.remove(name)
		}
		if newOwner != "" {
			if p, ok := t.add(name, newOwner); ok {
				evs = append(evs, mixer.PlayerAdded{Player: p})
			}
		}
		return evs
	case ifaceProps + ".PropertiesChanged":
		name, known := t.owners[sig.Sender]
		if !known {
			return nil
		}
		var iface string
		var changed map[string]dbus.Variant
		var invalidated []string
		if dbus.Store(sig.Body, &iface, &changed, &invalidated) != nil || iface != ifacePlayer {
			return nil
		}
		v, ok := changed["PlaybackStatus"]
		if !ok {
			return nil
		}
		status, _ := v.Value().(string)
		p := t.players[name]
		if status == p.Status {
			return nil
		}
		p.Status = status
		t.players[name] = p
		t.b.log.Debug("playback status changed", logattr.KeyPlayerBusName, name, logattr.KeyPlayerStatus, status)
		return []mixer.Event{mixer.PlayerStatusChanged{BusName: name, Status: status}}
	}
	return nil
}
