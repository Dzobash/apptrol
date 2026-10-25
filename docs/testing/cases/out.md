# Output test cases

Output devices: speakers, headphones, each output of an audio interface
([ADR 0027](../../adr/0027-output-volume.md)), fixed or the system's default
([ADR 0038](../../adr/0038-default-output-follows-the-server.md)). Every fixture has one test output: the
virtual microphone is made from a silent output called `apptrol-test-mic-sink`.

### TC-OUT-001 — `apptrol list` shows the output devices and marks the default

Covers: OUT-12, OUT-13, CFG-10 · Phase 1.6
Config: minimal
Type: Manual

**Setup**
- [ ] Your desktop's volume settings are open and show the output devices
- [ ] Note which output is the default (the one sound plays on)

**Steps**
- [ ] Run `apptrol list` → after the input devices comes
  `Output devices (* = the system's default)`; its NAME column lists
  `apptrol-test-mic-sink`
- [ ] Look at the lines that start with `*` → exactly one; it is the output noted as the
  default
- [ ] Run `apptrol list | grep -i monitor` → nothing: no monitor is listed as an input
  or an output
- [ ] Choose another output as the default in the desktop's volume settings, then run
  `apptrol list` → the `*` is on that output now
- [ ] Choose the noted output as the default again → the `*` is back on it in
  `apptrol list`
- [ ] Run `date +%T; systemctl --user restart apptrol` → it prints the time (`<time>`
  below) and Apptrol starts again. Apptrol logs the outputs it sees only when it starts,
  and the fixture started it before this case, so the restart gives the next step fresh
  records
- [ ] Run `journalctl --user -u apptrol --since <time> | grep 'output device not on a control'`
  → a line with `apptrol.device.name=apptrol-test-mic-sink` and its
  `apptrol.device.description`, and one for each other output

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-OUT-002 — Output devices are found, followed and listed

Covers: OUT-12, OUT-13 · Phase 1.6
Config: minimal
Type: Automated

Test: `TestOUT12_ListShowsOutputDevices`, `TestOUT12_ListShowsOutputControl` in
`cmd/apptrol/list_test.go`;
`TestOUT12_SinkFromReply`, `TestOUT12_OutputChanges` in
`internal/audio/pulse/convert_test.go`; `TestIntegration_OUT12_OutputDevices` and
`TestIntegration_List` in `internal/audio/pulse/integration_test.go` (a silent test
output that appears and goes, against a real PipeWire);
`TestOUT12_MixerKeepsOutputDevices`, `TestOUT13_OutputDevicesOnNoControlAreLoggedAtDebug`
in `internal/mixer/output_test.go`.

```text
Output devices (* = the system's default). Put part of DESCRIPTION or NAME into an output's match list:
    DESCRIPTION        NAME                     VOLUME      CONTROL
  * Built-in Speakers  alsa_output.speakers     45%         slider7 (Speakers)
    GoXLR Music        alsa_output.goxlr-music  100% muted  -
```

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-OUT-003 — The first matching control owns an output

Covers: OUT-01, OUT-02, OUT-04, OUT-05, OUT-12, OUT-13, PRIO-04 · Phase 1.6
Config: output-fixed
Type: Manual

The fixture matches the silent test output `apptrol-test-mic-sink` from slider 7,
knob 7 and slider 6. With no extra test sink present, slider 6 wins, up to 80 %.
It plays nothing.

```
slider6 ──► "Any test output" (max 80 %) ──► apptrol-test-mic-sink
slider7 ──► "Test output"               ──► ignored: slider6 owns it
knob7   ──► "Test output (knob)"        ──► ignored: slider6 owns it
```

**Setup**
- [ ] Slider 7, knob 7 and slider 6 have not been moved since the fixture was applied

**Steps**
- [ ] Run `pactl get-sink-volume apptrol-test-mic-sink` → note the percentage
  (`<before>`)
- [ ] Run `date +%T; systemctl --user restart apptrol` → it prints the time (`<time>`
  below) and Apptrol starts again
- [ ] Run `journalctl --user -u apptrol --since <time> | grep 'output matched'` → one
  line, for `apptrol.control=slider6`, with
  `apptrol.device.name=apptrol-test-mic-sink` and `apptrol.device.selected_by=match`
- [ ] Run `journalctl --user -u apptrol --since <time> | grep 'output match ignored'`
  → two warnings with `apptrol.device.controls="slider6, slider7"` and `"slider6, knob7"`
- [ ] Run `pactl get-sink-volume apptrol-test-mic-sink` → still `<before>`: no control has
  been moved, so Apptrol leaves the volume alone
- [ ] Move slider 6 to the top, then run `pactl get-sink-volume apptrol-test-mic-sink` →
  80 % on every channel (its `max_volume`)
- [ ] Move slider 6 to the middle, then run the same command → about 40 %
- [ ] Move knob 7 and slider 7 to the top, then run the same command → still about 40 %
- [ ] Run `apptrol list` → the line of `apptrol-test-mic-sink` shows
  only `slider6 (Any test output)` under
  CONTROL
- [ ] Press M7, then S7 → no LED lights up. The journal explains M7 is ignored because
  an earlier control owns the device, and that S has no function on an output column
  (ADR 0036). Mute stays unchanged.
- [ ] Wait five seconds after restart (past the new-output guard), run
  `pactl set-sink-volume apptrol-test-mic-sink 30%`, then run
  `pactl get-sink-volume apptrol-test-mic-sink` → still 30 %: Apptrol does not revert a
  change made outside it
- [ ] Move slider 6 a little, then run the same command → slider 6's position again

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-OUT-004 — Several matching outputs: the first by name, with a warning

Covers: OUT-02, OUT-05, OUT-13 · Phase 1.6
Config: output-fixed
Type: Manual

Slider 6's `match = ["apptrol-test"]` fits every test output. A second one,
`apptrol-test-extra-sink`, comes before `apptrol-test-mic-sink` by name.

**Setup**
- [ ] Slider 6 has been moved once, so its position is known

**Steps**
- [ ] Run `pactl load-module module-null-sink sink_name=apptrol-test-extra-sink` → it
  prints a module number (`<module>` below)
- [ ] Run `journalctl --user -u apptrol --since -1min | grep 'apptrol-test-extra-sink'` →
  `output device not on a control` (debug) and no warning: slider 6 keeps the output it
  has while that still matches
- [ ] Run `date +%T; systemctl --user restart apptrol` → it prints the time (`<time>`
  below)
- [ ] Run `journalctl --user -u apptrol --since <time> | grep 'several output devices match'`
  → one warning with `apptrol.control=slider6`,
  `apptrol.device.name=apptrol-test-extra-sink` and
  `apptrol.device.matches="apptrol-test-extra-sink, apptrol-test-mic-sink"`; the message
  says that `apptrol list` shows each device's unique name
- [ ] Run `apptrol list` → slider6 owns `apptrol-test-extra-sink`; slider7 now owns
  `apptrol-test-mic-sink`. The journal has `output match ignored` with
  `apptrol.device.controls="slider7, knob7"` for the mic sink.
- [ ] Run `pactl get-sink-volume apptrol-test-extra-sink` → slider 6's position: a
  device that appears for a control with a known position gets it at once
- [ ] Run `pactl unload-module <module>` → the journal has `output matched` for slider 6
  with `apptrol.device.name=apptrol-test-mic-sink`
- [ ] Read the new `output match ignored` warnings → `slider6, slider7` and
  `slider6, knob7`. `apptrol list` shows slider6 owning the mic sink again.

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-OUT-005 — Output volume rules hold in every situation

Covers: OUT-01, OUT-02, OUT-04, OUT-05, OUT-07, OUT-11, OUT-13, CFG-20, CTRL-06 · Phase 1.6
Config: output-fixed
Type: Automated

Test: in `internal/mixer/output_test.go`: `TestOUT01_SliderSetsOutputVolume`,
`TestOUT01_KnobSetsOutputVolume`, `TestOUT01_OutputsOnNoControlAreNotTouched`,
`TestOUT01_OutputMovedToAnotherControlIsSetAgain`,
`TestOUT02_MatchIsCaseInsensitiveOnNameOrDescription`,
`TestOUT02_SeveralMatchesUseFirstByNameAndWarn`, `TestOUT02_ChosenDeviceIsKeptWhileItMatches`,
`TestOUT05_UnknownPositionLeavesVolumeAlone`, `TestOUT05_OutsideVolumeIsNotReverted`,
`TestOUT07_SOnAnOutputColumnDoesNothing`, `TestOUT07_AppSoloDoesNotMuteOutputs`,
`TestOUT11_TalkOverDoesNotChangeOutputs`, `TestOUT04_FirstMatchingControlOwnsDevice`,
`TestOUT13_OutputMatchedIsLogged`, `TestOUT13_NoOutputDeviceMatchesAtConnectAndReload`,
`TestOUT13_OutputDeviceGoneIsLogged`, `TestOUT13_OutputReleasedIsLogged`,
`TestOUT01_OutputNoControlHoldsIsReleased`,
`TestOUT05_OutputThatStillMatchesAfterARenameIsKept`,
`TestCTRL06_OutputOwnershipEchoesReportedGeneration`, and `FuzzMixer` with outputs on
slider 7 and knob 7;
`TestOUT01_OutputNeverControlsAPlaybackStream` and
`TestCFG20_SetupPassesFixedAndDefaultOutputs` in `internal/config/setup_test.go`;
`TestOUT01_SliderSetsOutputVolumeThroughTheAudioAdapter` in
`internal/service/service_test.go`; `TestOUT05_GuardKeepsTheVolumeSetOnANewOutput`,
`TestOUT01_LatestSliderPositionWinsOverAGuardWrite`,
`TestCTRL06_ReleasedOutputIsNoLongerDefended`,
`TestCTRL06_RenamedOutputWaitsForReleaseAcrossNotifications` (rename, second notification,
no write, late release; deterministic, no sleeps),
`TestCTRL06_OutputOwnershipRejectsOlderGenerations` (stale answers and reappearance), and
`TestOUT05_RenamedOutputIsDefendedOnceTheMixerKeepsIt` in
`internal/audio/pulse/convert_test.go`; `TestIntegration_OUT01_OutputVolume` and
`TestIntegration_OUT05_GuardSetsTheOutputVolumeAgain` in
`internal/audio/pulse/integration_test.go` (`make test-audio`: a silent test output set
to 30 % reads 30 %; within the guard's 3 seconds a volume set from outside goes back to
30 %, after them it stays).

```text
slider moved, device chosen       -> SetOutputVolume (position × max_volume)
position unknown                  -> the device's volume is left alone
app solo, talk-over, S            -> no output changes
M on owning output slider        -> toggle saved output mute
M on conflicting output slider   -> ignored, logged with reason
new description, still matches    -> KeepOutput: the guard checks the volume read with it
new description, no longer        -> ReleaseOutput, logged once: an outside volume stays
```

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-OUT-006 — Output mute, outside changes and saved state

Covers: OUT-04, OUT-05, OUT-06, MUTE-01, MUTE-07, STATE-01 · Phase 1.6
Config: output-fixed, output-mute
Type: Manual + Automated

```text
M6 -> saved mute -> stop -> outside unmute -> start -> muted again
M7 -> ignored: slider6 owns the same matched device
plug in -> unmute within 3 s -> muted again; unmute after 3 s -> taken over
```

**Steps**
- [ ] Apply `output-fixed` with no extra test sink present; move slider6 to the middle
  → `pactl get-sink-volume apptrol-test-mic-sink` reports about 40 %.
- [ ] Press M6 → `pactl get-sink-mute apptrol-test-mic-sink` reports yes. Press M7
  → still yes; the journal explains the conflicting control was ignored. M6 is lit,
  M7 stays dark (TC-OUT-007 checks the LEDs).
- [ ] Run `systemctl --user stop apptrol`, then
  `pactl set-sink-mute apptrol-test-mic-sink 0`
  → `pactl get-sink-mute apptrol-test-mic-sink` reports no.
- [ ] Run `systemctl --user start apptrol`
  → `pactl get-sink-mute apptrol-test-mic-sink` reports yes: saved state restored it.
- [ ] Press M6 → mute reports no. Run `pactl set-sink-mute apptrol-test-mic-sink 1`
  → the journal records `muted outside Apptrol` on slider6. Move slider6 → mute stays on.
- [ ] Press M6 → mute reports no.
- [ ] Apply `output-mute` → `apptrol list` shows knob7 owning the mic sink and slider7
  assigned to the test app; slider6 warns that its extra sink is not present yet.
- [ ] Run `pactl set-sink-mute apptrol-test-mic-sink 1`, then move knob7
  → mute stays on; `muted outside Apptrol` names knob7.
- [ ] Press M7 → the test app mutes, while `pactl get-sink-mute apptrol-test-mic-sink`
  still reports yes. Press M7 again → the app unmutes, while the output stays muted.
- [ ] Run `pactl load-module module-null-sink sink_name=apptrol-test-extra-sink`
  → note `<module>`; the journal matches it to slider6.
- [ ] Move slider6 to the middle, press M6 → `pactl get-sink-mute apptrol-test-extra-sink`
  reports yes and `pactl get-sink-volume apptrol-test-extra-sink` reports about 40 %.
- [ ] Run `pactl unload-module <module>`, then
  `pactl load-module module-null-sink sink_name=apptrol-test-extra-sink`
  → note `<new-module>`; the new sink reports muted and about 40 % without a control move.
- [ ] Late restore, imitated (#177): run `pactl unload-module <new-module>`, then this
  one line. It plugs the sink in, gives Apptrol half a second to write its mute (it
  takes milliseconds; the sink may even come back muted on its own, so waiting for
  "muted" would not show Apptrol's write), and only then unmutes it, as a late restore
  would, with about 2.5 of the guard's 3 seconds left:
  `pactl load-module module-null-sink sink_name=apptrol-test-extra-sink; sleep 0.5; pactl set-sink-mute apptrol-test-extra-sink 0`
  → note `<new-module>` again. A second later `pactl get-sink-mute apptrol-test-extra-sink`
  reports yes and M6 is still lit. The journal has `output mute change not taken over:
  the new-output guard holds the mute` with `apptrol.audio.guard.state=restore_mute` (or
  `ownership_pending`) and `audio server changed the mute of a new output; setting it
  again`, and no `unmuted outside Apptrol`. Before the unmute, the journal must have
  `output mute request completed` with `apptrol.muted=true` for the sink; without it,
  Apptrol had not written yet, so unload the module and run the line again. If it is
  there and the journal has `unmuted outside Apptrol` instead, the case fails.
- [ ] Wait 5 seconds, then run `pactl set-sink-mute apptrol-test-extra-sink 0`
  → the journal records `unmuted outside Apptrol` on slider6 and M6 goes dark: after
  the window an outside change is taken over.

  ```text
  plug in ─ muted ─ unmute ─► muted again ─┤ 3 s ├─ unmute ─► taken over, M6 dark
  ```
- [ ] Run `pactl unload-module <new-module>` and restore the fixture with
  `docs/testing/apply-fixture.sh --restore` → the extra sink is gone and the original
  configuration is restored.

Automated: `TestOUT06_MuteSavedPerControlAndRestored`,
`TestOUT05_OutsideMuteTakenOverOnSliderAndKnob`, `TestOUT05_ReplugRestoresPositionAndMute`,
`TestOUT04_ChangingOwnerAppliesNewOwnersState`,
`TestOUT04_SameLabelsOnDifferentDevicesDoNotConflict`, `TestOUT05_OutputMuteAttribution`,
`TestOUT05_DescriptionAndMuteChangePreserveOwnersState`,
`TestOUT05_LateServerMuteRestoreIsSetAgain` and the other guard mute tests in
`output_mute_test.go` (docs/testing.md lists them), and
`TestIntegration_OUT06_OutputMuteAndOutsideChange`. Re-plug is exercised with a
silent server output. A real late restore by WirePlumber cannot be forced by hand;
the unmute in the same command line as the plug-in imitates it.

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-OUT-007 — M's LED on an output column; S does nothing

Covers: OUT-07, OUT-10, OUT-04, LED-02, MUTE-07 · Phase 1.6
Config: output-fixed
Type: Manual + Automated

Outputs have no solo ([ADR 0036](../../adr/0036-no-solo-on-outputs.md)). M's LED
shows the owning control's mute; S and R stay off.

```text
column 6 (owns the mic sink)   S ○  M ● while muted  R ○
column 7 (ignored: slider6)    S ○  M ○ always       R ○
```

**Setup**
- [ ] Apply `output-fixed` with no extra test sink present; the mic sink is not muted
  (`pactl get-sink-mute apptrol-test-mic-sink` reports no)

**Steps**
- [ ] Look at columns 6 and 7 → all six LEDs are off
- [ ] Press M6 → M6 lights up; `pactl get-sink-mute apptrol-test-mic-sink` reports yes
- [ ] Press S6 → no LED changes, mute stays yes. Run
  `journalctl --user -u apptrol --since -1min | grep 'button has no function on an output column'`
  → a debug line with `apptrol.button=S6` and `apptrol.app.type=output`
- [ ] Press M7 → M7 stays dark and M6 stays lit: slider6 owns the device
- [ ] Press M6 → M6 goes dark; mute reports no
- [ ] Run `pactl set-sink-mute apptrol-test-mic-sink 1` → M6 lights up: the outside
  mute is taken over. Run `pactl set-sink-mute apptrol-test-mic-sink 0` → M6 goes dark
- [ ] Press M6, then run `systemctl --user restart apptrol` → after the start M6 is lit
  again (saved mute). Press M6 → dark, mute reports no

Automated: `TestOUT10_MLitWhileOutputMuted`, `TestOUT10_AppSoloLeavesOutputLEDsOff`,
`TestOUT10_SavedMuteLitAtStart`, `TestOUT10_ConflictingControlShowsNoMute`,
`TestOUT10_OutputAddedConflictDarkensM`, `TestOUT10_SnapshotWithConflictShowsNoMute` in
`internal/mixer/output_mute_test.go`; `TestOUT07_SOnAnOutputColumnDoesNothing` and
`TestOUT07_AppSoloDoesNotMuteOutputs` in `internal/mixer/output_test.go`.

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-OUT-008 — The default output is followed; an overlap goes to the first control

Covers: OUT-03, OUT-04, OUT-05, OUT-10, OUT-12, OUT-13, MUTE-07 · Phase 1.6
Config: output-default
Type: Manual + Automated

Slider 6 follows the system's default output ([ADR 0038](../../adr/0038-default-output-follows-the-server.md));
slider 7 holds the silent test output. While the test output is the default, both
choose it, and slider 6, the earlier control, owns it. Slider 6 is moved only while
the test output is the default, so your own output's volume is never set.

```text
your output default ─────────┤ test output default ├─────────┤ your output default
slider6 → your output          slider6 → test output           slider6 → your output (volume kept)
slider7 → test output          slider7 does nothing            slider7 → test output (its position)
```

**Setup**
- [ ] Nothing loud is playing; slider 6 and slider 7 have not been moved since the
  fixture was applied
- [ ] Run `pactl get-default-sink` → note the name (`<your default>` below); it is not
  `apptrol-test-mic-sink`
- [ ] Run `pactl get-sink-volume <your default>` → note the percentage (`<your volume>`)

**Steps**
- [ ] Run `date +%T; systemctl --user restart apptrol` → it prints the time (`<time>`
  below)
- [ ] Run `journalctl --user -u apptrol --since <time> | grep -E 'output matched|keeps its volume'`
  → `output matched` for `apptrol.control=slider6` with
  `apptrol.device.selected_by=default` and `apptrol.device.name=<your default>`, one for
  slider 7 with `selected_by=match`, and `default output keeps its volume until the
  control is moved` for slider 6. No line says `not implemented yet`
- [ ] Run `pactl get-sink-volume apptrol-test-mic-sink` → note the percentage
  (`<test volume>`)
- [ ] Run `pactl set-default-sink apptrol-test-mic-sink` (or choose the test output in
  the desktop's sound settings) → `apptrol list` puts `*` on `apptrol-test-mic-sink`
  and shows only `slider6 (Main volume)` under CONTROL
- [ ] Run `journalctl --user -u apptrol --since -1min | grep -E 'default output|output released'`
  → `default output changed` with `apptrol.device.name=apptrol-test-mic-sink` and
  `apptrol.device.previous_name=<your default>`; `output released; no control holds
  the device` for `<your default>`; and at info, `default output is the same device as
  another output; the earlier control owns it` with `apptrol.control=slider7` and
  `apptrol.device.controls="slider6, slider7"`. No `output match ignored` warning
- [ ] Run `pactl get-sink-volume apptrol-test-mic-sink` → still `<test volume>`: the
  new default keeps its volume until slider 6 moves
- [ ] Move slider 6 to the top → the same command shows 50 % (slider 6's `max_volume`)
- [ ] Move slider 7 to the top → still 50 %: slider 7 yields while slider 6 owns it
- [ ] Press M6 → M6 lights up; `pactl get-sink-mute apptrol-test-mic-sink` reports yes.
  Press M7 → M7 stays dark, mute stays yes
- [ ] Run `pactl set-sink-mute apptrol-test-mic-sink 0` (as a mute key would, on the
  default output) → M6 goes dark: slider 6 takes the outside unmute over
- [ ] Press M6 twice → lit, then dark again; mute reports no. Leave it unmuted, so
  switching back does not mute your own output
- [ ] Run `pactl set-default-sink <your default>` → the journal has `default output
  changed` with `apptrol.device.previous_name=apptrol-test-mic-sink` and
  `output matched` for slider 7 on `apptrol-test-mic-sink`
- [ ] Run `pactl get-sink-volume apptrol-test-mic-sink` → 80 %: slider 7 took the test
  output back with its own position (top, `max_volume = 80`)
- [ ] Run `pactl get-sink-volume <your default>` → still `<your volume>`: Apptrol did not
  put slider 6's position on it. Do not move slider 6 now
- [ ] Run `pactl get-default-sink` → `<your default>` again

Automated: in `internal/mixer/output_default_test.go`:
`TestOUT03_DefaultOutputFollowsEveryChange`, `TestOUT03_DefaultOutputVolumeIsSetOnlyByAMove`,
`TestOUT03_NoDefaultOutputDoesNothing`, `TestOUT05_OutsideMuteOnTheDefaultOutputIsTakenOver`,
`TestOUT04_DefaultOverlapFirstControlOwns`; `TestOUT03_ServerChangeReportsANewDefaultOutput`
in `internal/audio/pulse/convert_test.go`; `TestIntegration_OUT03_DefaultOutputFollowsASwitch`
in `internal/audio/pulse/integration_test.go` (the real default switched and restored);
`TestOUT03_DefaultOutputFollowedThroughTheService` in `internal/service/service_test.go`;
`TestOUT12_ListShowsOutputControl` in `cmd/apptrol/list_test.go`.

**Result:** Pass / Fail / Blocked
**Notes:**
