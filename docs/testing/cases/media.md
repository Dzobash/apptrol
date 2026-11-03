# Media test cases

### TC-MEDIA-001 — At the login screen after Switch user, the media buttons send nothing

Covers: MEDIA-11, LAUNCH-15, SVC-10 · Phase 1.6
Config: minimal
Type: Manual
Was: H-49 (the media buttons part)

**Setup**
- [ ] Test tone A plays (slider 1); slider 1 in the middle
- [ ] Music plays in a media player that is not muted by Apptrol (e.g. a browser tab):
  ▶ is lit
- [ ] Note the time, for reading the log afterwards

**Steps**
- [ ] Choose *Switch user* → the login screen appears; the music pauses (the system
  takes the speakers, outside Apptrol); the log has
  `media buttons blocked: this session is not in front` with
  `apptrol.screen.state=inactive`
- [ ] Press ▶ → nothing starts playing; the log has one line,
  `media button pressed while this session is not in front; nothing sent` with
  `apptrol.button=▶` and `apptrol.screen.state=inactive`
- [ ] Press ■ → nothing changes; one warning with `apptrol.button=■`
- [ ] Press R1 → nothing changes; one warning with `apptrol.button=R1`, also though test
  tone A has no media player
- [ ] Move slider 1 → the log has `volume changed` for slider 1
- [ ] Press M1 → M1 lights up; the log has `button pressed while the screen is locked`
  with `apptrol.button=M1`, not the media warning
- [ ] Press M1 again → M1 goes out
- [ ] Log in as yourself again → the log has
  `media buttons allowed: this session is in front`, once; its `apptrol.screen.state`
  is the state the session comes back in, `locked` when the desktop shows its lock
  screen first, or `unlocked`; the music stays paused
- [ ] Press ▶ → the music plays again; the log has `media key` with
  `apptrol.player.command=Play`
- [ ] Run `journalctl --user -u apptrol --since <time> | grep -E 'media button|screen is locked'`
  → one `blocked`, three media warnings (▶, ■, R1), two
  `button pressed while the screen is locked` (both M1), one `allowed`: one line per
  press

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-MEDIA-002 — Seeking a browser video with the mouse does not blink R and ▶

Covers: MEDIA-09, LED-03, LED-06 · Phase 1.6
Config: browser-player
Type: Manual

**Setup**
- [ ] A long video plays in a browser tab whose player pauses while you scrub (YouTube,
  or the browser's own controls on a plain video file); slider 1 in the middle
- [ ] R1 and ▶ are lit
- [ ] Note the time, for reading the log afterwards

**Steps**
- [ ] Click a point in the loaded part of the progress bar → R1 and ▶ stay lit, with
  no flicker
- [ ] Drag the progress bar back and forth a few times, each drag shorter than half a
  second → R1 and ▶ stay lit. A drag held longer is a pause longer than 500 ms, and
  R1 and ▶ go dark until the video plays again (MEDIA-09)
- [ ] Press the keyboard's right arrow key to seek → R1 and ▶ stay lit
- [ ] Click a point far beyond the loaded part, so the video buffers for a second or more
  → R1 and ▶ go dark until the video plays again, then light up again at once
- [ ] Press R1 → the video pauses and R1 and ▶ go dark at once, without waiting
- [ ] Press R1 again → the video plays; R1 and ▶ light up at once
- [ ] Pause the video with the browser's own button → R1 and ▶ go dark about half a
  second later
- [ ] Run `journalctl --user -u apptrol --since <time> | grep -E 'playback status|on the LEDs'`
  → each skipped seek pause shows `playback status changed` (`Paused`, then `Playing`)
  and `short pause not shown on the LEDs`; the buffering and the browser's own pause
  show `pause shown on the LEDs`; the presses of R1 show none of the two messages

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-MEDIA-003 — R on an output with a player list plays and pauses that player

Covers: MEDIA-12, OUT-09, OUT-10, MEDIA-06, CFG-15 · Phase 1.7
Config: output-player
Type: Manual

**Setup**
- [ ] A video plays in a browser (Firefox, Chrome or Vivaldi); nothing else plays
- [ ] Note the time, for reading the log afterwards

**Steps**
- [ ] Run `apptrol check` → slider7 shows
  `output: apptrol-test-mic-sink; player: firefox, chrome, vivaldi`; one warning says
  `test-player-out` and `test-out` can match the same output devices and those go to
  `test-player-out` on slider7
- [ ] Look at column 7 → R7 is lit (the video plays); S7 and M7 are off
- [ ] Look at column 8 → S8, M8 and R8 are off (an output without player)
- [ ] Press R7 → the video pauses; R7 goes dark at once
- [ ] Press R7 again → the video plays; R7 lights up at once
- [ ] Pause the video with the browser's own button → R7 goes dark about half a second
  later
- [ ] Press ▶ → the video plays (the media keys are pinned to `test-player-out`)
- [ ] Press M7 → M7 lights up; the desktop's volume settings show
  `apptrol-test-mic-sink` muted; the video keeps playing and R7 stays lit
- [ ] Press M7 again → M7 goes out; the sink is unmuted
- [ ] Press R8 → nothing changes; R8 stays dark
- [ ] Run `journalctl --user -u apptrol --since <time> | grep -E 'media player matched|paused|playing|media key|no function'`
  → `media player matched` with `apptrol.app.id=test-player-out`,
  `apptrol.app.type=output`, `apptrol.control=slider7` and `apptrol.player.matched_by`;
  `paused` and `playing` with `apptrol.button=R7`; `media key` with
  `apptrol.player.selection=pinned`; `button has no function: its mode is off` with
  `apptrol.button=R8`

**Result:** Pass / Fail / Blocked
**Notes:**
