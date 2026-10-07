// Package session is the adapter for logind, the login manager, on the D-Bus
// system bus. It reports to the mixer when the computer wakes up from sleep
// or hibernation, so every LED is sent again (LED-09, ADR 0023), and whether
// the user's screen is unlocked, so launchers start apps only then
// (LAUNCH-13, ADR 0024), and whose session is in front at the seat, so the
// controller is let go while another user is there (SVC-08, ADR 0029).
package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/Dzobash/apptrol/internal/logattr"
	"github.com/Dzobash/apptrol/internal/mixer"
)

// logind names (https://www.freedesktop.org/software/systemd/man/latest/org.freedesktop.login1.html).
const (
	login1Name   = "org.freedesktop.login1"
	login1Path   = dbus.ObjectPath("/org/freedesktop/login1")
	login1Iface  = "org.freedesktop.login1.Manager"
	userIface    = "org.freedesktop.login1.User"
	sessionIface = "org.freedesktop.login1.Session"
	seatIface    = "org.freedesktop.login1.Seat"
	propsIface   = "org.freedesktop.DBus.Properties"
	sleepSignal  = "PrepareForSleep"
	noSuchUser   = "org.freedesktop.login1.NoSuchUser"
	noSuchSeat   = "org.freedesktop.login1.NoSuchSeat"
	systemSocket = "unix:path=/run/dbus/system_bus_socket"
)

// seatName is the seat whose session in front is followed. Computers with
// several seats are out of scope (ADR 0029).
const seatName = "seat0"

// greeterClass is the session class of the login screen (SVC-10).
const greeterClass = "greeter"

// callTimeout bounds every call to logind.
const callTimeout = 2 * time.Second

// unreadableRetry is how long after a failed read of the screen state it is
// read again; launchers stay blocked meanwhile (ADR 0024).
const unreadableRetry = 5 * time.Second

// Why the screen state is unknown (apptrol.screen.reason, ADR 0024).
const (
	reasonNoBus      = "no_system_bus"
	reasonBusLost    = "system_bus_lost"
	reasonNoSession  = "no_graphical_session"
	reasonUnreadable = "unreadable"
)

// defaultResend: after a wake-up, mixer.SystemResumed is sent at once and
// again after each of these delays. The controller starts again some seconds
// after the wake-up and ignores LED messages while it starts (LED-09, ADR 0023).
var defaultResend = []time.Duration{500 * time.Millisecond, 2 * time.Second, 5 * time.Second}

// Watcher keeps a connection to the system bus and reports wake-ups and the
// screen state.
type Watcher struct {
	log     *slog.Logger
	address string // "" = from the environment (SystemBusAddress)
	uid     uint32 // the user whose graphical session is followed
	// Wait before reconnecting: starts at retryMin, doubles up to retryMax.
	retryMin, retryMax time.Duration
	resend             []time.Duration // see defaultResend
}

// New returns a Watcher for the user running Apptrol. address is the system
// bus address; "" finds it as SystemBusAddress does.
func New(log *slog.Logger, address string) *Watcher {
	return &Watcher{log: log.With(logattr.Component(logattr.Session)), address: address,
		uid: uint32(os.Getuid()), retryMin: 500 * time.Millisecond, retryMax: 30 * time.Second,
		resend: defaultResend}
}

// SystemBusAddress returns DBUS_SYSTEM_BUS_ADDRESS, or the standard socket.
func SystemBusAddress() string {
	if a := os.Getenv("DBUS_SYSTEM_BUS_ADDRESS"); a != "" {
		return a
	}
	return systemSocket
}

func (w *Watcher) busAddress() string {
	if w.address != "" {
		return w.address
	}
	return SystemBusAddress()
}

// Run connects and keeps the connection until ctx is canceled. It sends
// mixer.SystemResumed after every wake-up, mixer.ScreenChanged whenever the
// screen state changes, and mixer.SeatChanged whenever another session comes
// to the front; without a connection both are unknown, so launchers are
// blocked (LAUNCH-13, ADR 0024) and the controller is held as before (SVC-12,
// ADR 0029). Without a bus, or when the connection breaks, it logs a warning
// and reconnects. It returns ctx's error.
func (w *Watcher) Run(ctx context.Context, out chan<- mixer.Event) error {
	scr := &screen{log: w.log, out: out}
	st := &seat{log: w.log, out: out}
	wait := w.retryMin
	failed := false // the last attempt failed; don't repeat the warning
	for {
		addr := w.busAddress()
		conn, err := dbus.Connect(addr, dbus.WithContext(ctx))
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !failed {
				w.log.Warn("cannot connect to the system bus; launchers are blocked and LEDs are not sent again after sleep, retrying",
					logattr.Error(logattr.ErrSystemBusUnreachable, err))
				failed = true
			} else {
				w.log.Debug("system bus still unreachable", logattr.Error(logattr.ErrSystemBusUnreachable, err),
					logattr.KeyRetryDelay, wait.Seconds())
			}
			if !scr.set(ctx, mixer.ScreenUnknown, reasonNoBus, "") || !st.set(ctx, mixer.FrontUnknown, "", nil) {
				return ctx.Err()
			}
		} else {
			wait = w.retryMin
			err = w.session(ctx, conn, addr, scr, st)
			_ = conn.Close()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			w.log.Warn("lost the connection to the system bus; reconnecting", logattr.Error(logattr.ErrSystemBusLost, err))
			failed = true // already reported; retries are logged at debug level
			if !scr.set(ctx, mixer.ScreenUnknown, reasonBusLost, "") || !st.set(ctx, mixer.FrontUnknown, "", nil) {
				return ctx.Err()
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		wait = min(wait*2, w.retryMax)
	}
}

// session follows logind on one connection until it breaks or ctx ends: the
// sleep signal, the user's graphical session, and the session in front at
// the seat.
func (w *Watcher) session(ctx context.Context, conn *dbus.Conn, addr string, scr *screen, st *seat) error {
	signals := make(chan *dbus.Signal, 16)
	conn.Signal(signals)
	defer conn.RemoveSignal(signals)
	if err := conn.AddMatchSignalContext(ctx, dbus.WithMatchSender(login1Name),
		dbus.WithMatchObjectPath(login1Path), dbus.WithMatchInterface(login1Iface),
		dbus.WithMatchMember(sleepSignal)); err != nil {
		return fmt.Errorf("subscribe to %s: %w", sleepSignal, err)
	}
	// Changes of the user's Display, of the session's LockedHint and Active,
	// and of the seat's ActiveSession (all emit changes). Subscribed before
	// reading, so none is missed.
	if err := conn.AddMatchSignalContext(ctx, dbus.WithMatchSender(login1Name),
		dbus.WithMatchPathNamespace(login1Path), dbus.WithMatchInterface(propsIface),
		dbus.WithMatchMember("PropertiesChanged")); err != nil {
		return fmt.Errorf("subscribe to session changes: %w", err)
	}
	// logind starting (again): its objects are new, so everything is read again
	// (ADR 0024).
	if err := conn.AddMatchSignalContext(ctx, dbus.WithMatchSender("org.freedesktop.DBus"),
		dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, login1Name)); err != nil {
		return fmt.Errorf("subscribe to logind starting: %w", err)
	}
	// Logged once subscribed: from here on, no wake-up is missed.
	w.log.Info("connected to the system bus", logattr.KeySessionBusAddress, addr)

	g := &graphical{w: w, conn: conn, ctx: ctx}
	var retry <-chan time.Time // the next read while a state is unreadable
	reread := func() bool {
		retry = nil
		if !g.update(scr) || !g.updateSeat(st) {
			return false
		}
		if scr.reason == reasonUnreadable || st.unreadable {
			retry = time.After(unreadableRetry)
		}
		return true
	}
	if !reread() {
		return ctx.Err()
	}

	var resend <-chan time.Time // the next repeat of SystemResumed
	var next int                // index into w.resend of the repeat after that
	var resumedAt time.Time
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-conn.Context().Done():
			return errors.New("the system bus closed the connection")
		case sig, ok := <-signals:
			if !ok {
				return errors.New("the system bus closed the connection")
			}
			if sig.Name == "org.freedesktop.DBus.NameOwnerChanged" {
				g.user, g.seat = "", "" // logind's objects may have changed
				if !reread() {
					return ctx.Err()
				}
				continue
			}
			if g.concerns(sig) {
				if !reread() {
					return ctx.Err()
				}
				continue
			}
			sleeping, ok := sleepState(sig)
			if !ok {
				continue
			}
			if sleeping {
				w.log.Debug("system going to sleep")
				resend = nil // a wake-up that was not over yet does not matter now
				continue
			}
			w.log.Info("system resumed; sending LEDs again")
			if !send(ctx, scr.out, mixer.SystemResumed{}) {
				return ctx.Err()
			}
			resumedAt, next, resend = time.Now(), 0, nil
			if len(w.resend) > 0 {
				resend = time.After(w.resend[0])
			}
		case <-retry:
			if !reread() {
				return ctx.Err()
			}
		case <-resend:
			// Each repeat is logged, so a log shows whether they ran when LEDs
			// come back late or not at all (LED-09, ADR 0023).
			w.log.Debug("LEDs sent again after waking up", logattr.KeySessionResend, next+1)
			if !send(ctx, scr.out, mixer.SystemResumed{}) {
				return ctx.Err()
			}
			resend = nil
			if next++; next < len(w.resend) {
				resend = time.After(time.Until(resumedAt.Add(w.resend[next])))
			}
		}
	}
}

// graphical finds the user's graphical session and reads its state.
type graphical struct {
	w    *Watcher
	conn *dbus.Conn
	ctx  context.Context

	user    dbus.ObjectPath // the user's object, "" until found
	session dbus.ObjectPath // the graphical session's object, "" if none
	id      string          // its id
	seat    dbus.ObjectPath // seat0's object, "" until found
}

// concerns reports whether a signal is a property change of the user, of
// their graphical session, or of the seat.
func (g *graphical) concerns(sig *dbus.Signal) bool {
	return sig.Name == propsIface+".PropertiesChanged" && sig.Path != "" &&
		(sig.Path == g.user || sig.Path == g.session || sig.Path == g.seat)
}

// update reads the screen state again and reports it. It returns false when
// ctx ends.
func (g *graphical) update(scr *screen) bool {
	state, reason, err := g.read()
	if err != nil {
		g.w.log.Warn("cannot read the screen lock state; launchers are blocked",
			logattr.Error(logattr.ErrScreenUnreadable, err))
	}
	return scr.set(g.ctx, state, reason, g.id)
}

// read finds the user's graphical session (the user's Display) and reads its
// LockedHint and Active.
func (g *graphical) read() (mixer.ScreenState, string, error) {
	ctx, cancel := context.WithTimeout(g.ctx, callTimeout)
	defer cancel()
	if g.user == "" {
		var user dbus.ObjectPath
		err := g.conn.Object(login1Name, login1Path).CallWithContext(ctx, login1Iface+".GetUser", 0, g.w.uid).Store(&user)
		if errorName(err) == noSuchUser {
			g.forget()
			return mixer.ScreenUnknown, reasonNoSession, nil // no login session at all
		}
		if err != nil {
			return mixer.ScreenUnknown, reasonUnreadable, fmt.Errorf("find the user: %w", err)
		}
		g.user = user
	}
	var display struct {
		ID   string
		Path dbus.ObjectPath
	}
	if err := g.get(ctx, g.user, userIface, "Display", &display); err != nil {
		return mixer.ScreenUnknown, reasonUnreadable, fmt.Errorf("read the user's graphical session: %w", err)
	}
	if display.ID == "" || display.Path == "" || display.Path == "/" {
		g.forget()
		return mixer.ScreenUnknown, reasonNoSession, nil
	}
	if display.ID != g.id {
		g.w.log.Debug("graphical session found", logattr.KeySessionID, display.ID)
	}
	g.session, g.id = display.Path, display.ID
	var locked, active bool
	if err := g.get(ctx, g.session, sessionIface, "LockedHint", &locked); err != nil {
		return mixer.ScreenUnknown, reasonUnreadable, fmt.Errorf("read LockedHint: %w", err)
	}
	if err := g.get(ctx, g.session, sessionIface, "Active", &active); err != nil {
		return mixer.ScreenUnknown, reasonUnreadable, fmt.Errorf("read Active: %w", err)
	}
	switch {
	case !active:
		return mixer.ScreenInactive, "", nil
	case locked:
		return mixer.ScreenLocked, "", nil
	}
	return mixer.ScreenUnlocked, "", nil
}

// forget drops the graphical session, keeping the user's object.
func (g *graphical) forget() { g.session, g.id = "", "" }

// updateSeat reads whose session is in front and reports it. It returns false
// when ctx ends.
func (g *graphical) updateSeat(st *seat) bool {
	front, id, err := g.readSeat()
	return st.set(g.ctx, front, id, err)
}

// readSeat finds seat0 and reads its ActiveSession, then that session's User
// and Class (SVC-08, SVC-10).
func (g *graphical) readSeat() (front mixer.SeatFront, id string, err error) {
	ctx, cancel := context.WithTimeout(g.ctx, callTimeout)
	defer cancel()
	if g.seat == "" {
		var seat dbus.ObjectPath
		err := g.conn.Object(login1Name, login1Path).CallWithContext(ctx, login1Iface+".GetSeat", 0, seatName).Store(&seat)
		if errorName(err) == noSuchSeat {
			return mixer.FrontNobody, "", nil // no seat, e.g. a server: nobody sits in front
		}
		if err != nil {
			return mixer.FrontUnknown, "", fmt.Errorf("find %s: %w", seatName, err)
		}
		g.seat = seat
	}
	var active struct {
		ID   string
		Path dbus.ObjectPath
	}
	if err := g.get(ctx, g.seat, seatIface, "ActiveSession", &active); err != nil {
		return mixer.FrontUnknown, "", fmt.Errorf("read the session in front: %w", err)
	}
	if active.ID == "" || active.Path == "" || active.Path == "/" {
		return mixer.FrontNobody, "", nil
	}
	var user struct {
		UID  uint32
		Path dbus.ObjectPath
	}
	if err := g.get(ctx, active.Path, sessionIface, "User", &user); err != nil {
		return mixer.FrontUnknown, active.ID, fmt.Errorf("read the user of the session in front: %w", err)
	}
	var class string
	if err := g.get(ctx, active.Path, sessionIface, "Class", &class); err != nil {
		return mixer.FrontUnknown, active.ID, fmt.Errorf("read the class of the session in front: %w", err)
	}
	switch {
	case user.UID == g.w.uid:
		// Any session of this user, graphical or not (e.g. startx), is theirs.
		return mixer.FrontThisUser, active.ID, nil
	case class == greeterClass:
		return mixer.FrontLoginScreen, active.ID, nil
	}
	return mixer.FrontOtherUser, active.ID, nil
}

// errorName returns the D-Bus error name of err, "" if it is none.
func errorName(err error) string {
	var v dbus.Error
	if errors.As(err, &v) {
		return v.Name
	}
	var p *dbus.Error
	if errors.As(err, &p) {
		return p.Name
	}
	return ""
}

func (g *graphical) get(ctx context.Context, path dbus.ObjectPath, iface, prop string, v any) error {
	var variant dbus.Variant
	if err := g.conn.Object(login1Name, path).CallWithContext(ctx, propsIface+".Get", 0, iface, prop).Store(&variant); err != nil {
		return err
	}
	return variant.Store(v)
}

// screen remembers the last screen state reported, so that only changes are
// logged and sent.
type screen struct {
	log    *slog.Logger
	out    chan<- mixer.Event
	state  mixer.ScreenState // "" until the first report
	reason string
}

// set reports a new state. It returns false when ctx ends.
func (s *screen) set(ctx context.Context, state mixer.ScreenState, reason, id string) bool {
	if state == s.state && reason == s.reason {
		return true
	}
	s.state, s.reason = state, reason
	switch {
	case state == mixer.ScreenUnlocked:
		s.log.Info("launchers allowed: the screen is unlocked", logattr.KeySessionID, id)
	case reason != "":
		s.log.Info("launchers blocked: the screen is not known to be unlocked",
			logattr.KeyScreenState, string(state), logattr.KeyScreenReason, reason)
	default:
		s.log.Info("launchers blocked: the screen is not known to be unlocked", logattr.KeyScreenState, string(state))
	}
	return send(ctx, s.out, mixer.ScreenChanged{State: state})
}

// seat remembers whose session was last reported in front, so that only
// changes are logged and sent.
type seat struct {
	log        *slog.Logger
	out        chan<- mixer.Event
	front      mixer.SeatFront // "" until the first report
	id         string
	unreadable bool // the last read failed; warned once until it succeeds
}

// set reports who is in front; err is why it could not be read. It returns
// false when ctx ends.
func (s *seat) set(ctx context.Context, front mixer.SeatFront, id string, err error) bool {
	if err != nil && !s.unreadable {
		s.log.Warn("cannot read who is in front; holding the controller as before",
			logattr.Error(logattr.ErrSeatUnreadable, err))
	}
	s.unreadable = err != nil
	if front == s.front && id == s.id {
		return true
	}
	s.front, s.id = front, id
	s.log.Debug("session in front changed", logattr.KeySeatFront, string(front), logattr.KeySessionID, id)
	return send(ctx, s.out, mixer.SeatChanged{Front: front, SessionID: id})
}

// sleepState reads a PrepareForSleep signal: true before sleep, false after
// waking up. ok is false for any other signal.
func sleepState(sig *dbus.Signal) (sleeping, ok bool) {
	if sig.Name != login1Iface+"."+sleepSignal || sig.Path != login1Path || len(sig.Body) != 1 {
		return false, false
	}
	sleeping, ok = sig.Body[0].(bool)
	return sleeping, ok
}

func send(ctx context.Context, out chan<- mixer.Event, ev mixer.Event) bool {
	select {
	case out <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}
