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
  `media buttons allowed: this session is in front` with
  `apptrol.screen.state=unlocked`; the music stays paused
- [ ] Press ▶ → the music plays again; the log has `media key` with
  `apptrol.player.command=Play`
- [ ] Run `journalctl --user -u apptrol --since <time> | grep -E 'media button|screen is locked'`
  → one `blocked`, three media warnings (▶, ■, R1), two
  `button pressed while the screen is locked` (both M1), one `allowed`: one line per
  press

**Result:** Pass / Fail / Blocked
**Notes:**
