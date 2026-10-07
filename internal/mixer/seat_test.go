package mixer

import (
	"fmt"
	"log/slog"
	"testing"

	"github.com/Dzobash/apptrol/internal/logattr"
)

// Fronts as the session adapter reports them (ADR 0029).
var (
	alice   = SeatChanged{Front: FrontThisUser, SessionID: "c1"}
	bob     = SeatChanged{Front: FrontOtherUser, SessionID: "c2"}
	login   = SeatChanged{Front: FrontLoginScreen, SessionID: "c3"}
	nobody  = SeatChanged{Front: FrontNobody}
	unknown = SeatChanged{Front: FrontUnknown}
)

// wantController checks the ReleaseController and TakeController actions so far.
func (w *world) wantController(want ...Action) {
	w.t.Helper()
	if fmt.Sprint(w.controller) != fmt.Sprint(want) {
		w.t.Errorf("controller actions = %v, want %v", w.controller, want)
	}
}

// noticeAttrs returns the attributes of the first notice with msg.
func (w *world) noticeAttrs(msg string) map[string]any {
	w.t.Helper()
	for _, n := range w.notices {
		if n.Msg == msg {
			attrs := map[string]any{}
			for i := 0; i+1 < len(n.Attrs); i += 2 {
				attrs[fmt.Sprint(n.Attrs[i])] = n.Attrs[i+1]
			}
			return attrs
		}
	}
	w.t.Fatalf("no notice %q in %v", msg, w.notices)
	return nil
}

func TestSVC08_AnotherUserInFrontReleasesTheController(t *testing.T) {
	w := started(t)
	w.do(alice)
	w.wantController() // the controller is held from the start

	w.do(bob) // Switch user to Bob
	w.wantController(ReleaseController{})
	w.wantNotice(slog.LevelInfo, "another user is in front; releasing the controller")
	attrs := w.noticeAttrs("another user is in front; releasing the controller")
	if attrs[logattr.KeySeatFront] != "other_user" || attrs[logattr.KeySessionID] != "c2" {
		t.Errorf("release attributes = %v, want the reason other_user and session c2", attrs)
	}

	w.do(SeatChanged{Front: FrontOtherUser, SessionID: "c4"}) // a third user: still released
	w.wantController(ReleaseController{})

	w.do(alice) // back to Alice
	w.wantController(ReleaseController{}, TakeController{})
	if attrs := w.noticeAttrs("this user is in front again; taking the controller"); attrs[logattr.KeySeatFront] != "this_user" {
		t.Errorf("take attributes = %v, want the reason this_user", attrs)
	}
}

func TestSVC09_ReleasingEndsHeldStates(t *testing.T) {
	w := started(t).do(alice)
	w.press(ButtonS, 8) // cough: the microphone is muted while S8 is held
	if !w.devMute[goxlr.Name] {
		t.Fatal("cough did not mute the microphone")
	}
	w.do(bob) // the release of S8 will never arrive
	if w.devMute[goxlr.Name] {
		t.Error("the microphone stayed muted after the controller was released")
	}
	if attrs := w.noticeAttrs("held state ended"); attrs[logattr.KeyHeldReason] != "controller_released" {
		t.Errorf("held state ended with %v, want reason controller_released", attrs)
	}
	if _, last := w.last[len(w.last)-1].(ReleaseController); !last {
		t.Errorf("ReleaseController is not the last action: %v", w.last)
	}
}

func TestSVC10_LoginScreenKeepsOrReleases(t *testing.T) {
	w := started(t).do(alice, login, SeatChanged{Front: FrontLoginScreen, SessionID: "c5"})
	w.wantController() // at_login_screen = "keep" (the default)
	n := 0
	for _, x := range w.notices {
		if x.Msg == "login screen in front; keeping the controller" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("keeping logged %d times, want once per change", n)
	}

	setup := testSetup()
	setup.ReleaseAtLoginScreen = true
	w = newWorld(t, setup, State{}).do(alice, login)
	w.wantController(ReleaseController{})
	if attrs := w.noticeAttrs("another user is in front; releasing the controller"); attrs[logattr.KeySeatFront] != "login_screen" {
		t.Errorf("release attributes = %v, want the reason login_screen", attrs)
	}
}

func TestCFG24_ReloadAppliesAtLoginScreenAtOnce(t *testing.T) {
	w := started(t).do(alice, login)
	w.wantController()
	setup := testSetup()
	setup.ReleaseAtLoginScreen = true
	w.do(ConfigChanged{Setup: setup})
	w.wantController(ReleaseController{})
	// Back to "keep" while the login screen is still in front: the login
	// screen changes nothing, so it stays released until this user is back.
	w.do(ConfigChanged{Setup: testSetup()})
	w.wantController(ReleaseController{})
	w.do(alice)
	w.wantController(ReleaseController{}, TakeController{})
}

// Found on the v0.2.1-rc2 hardware checklist: switching back from another user
// goes through the login screen, and the controller was taken there already,
// before this user was in front (and opening it failed: the login screen's
// session has the rights to it then).
func TestSVC10_KeepLeavesTheControllerWithWhoeverHadIt(t *testing.T) {
	w := started(t).do(alice, bob) // Switch user to Bob: released
	w.wantController(ReleaseController{})

	w.do(SeatChanged{Front: FrontLoginScreen, SessionID: "c6"}) // Bob switches back: the login screen first
	w.wantController(ReleaseController{})                       // not taken yet: Bob had it last
	w.wantNotice(slog.LevelInfo, "login screen in front; the controller stays with the other user")

	w.do(alice) // Alice logs in again
	w.wantController(ReleaseController{}, TakeController{})
}

func TestSVC12_UnknownOrNobodyHoldsTheController(t *testing.T) {
	w := started(t).do(unknown, nobody)
	w.wantController() // held from the start, also while it cannot be told
	w.do(bob, unknown)
	w.wantController(ReleaseController{}, TakeController{}) // unknown: hold as before
	w.do(bob, nobody)
	w.wantController(ReleaseController{}, TakeController{}, ReleaseController{}, TakeController{})
}
