# Output test cases

Output devices: speakers, headphones, each output of an audio interface
([ADR 0027](../../adr/0027-output-volume.md)). Every fixture has one test output: the
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
knob 7 and slider 6. Slider 6 wins, up to 80 %. It plays nothing.

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
  an earlier control owns the device; S7 is not built yet. Mute stays unchanged.
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
- [ ] Run `pactl get-sink-volume apptrol-test-extra-sink` → slider 6's position: a
  device that appears for a control with a known position gets it at once
- [ ] Run `pactl unload-module <module>` → the journal has `output matched` for slider 6
  with `apptrol.device.name=apptrol-test-mic-sink`

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
`TestOUT07_SOnAnOutputColumnDoesNothingYet`, `TestOUT07_AppSoloDoesNotMuteOutputs`,
`TestOUT11_TalkOverDoesNotChangeOutputs`, `TestOUT04_FirstMatchingControlOwnsDevice`,
`TestOUT13_OutputMatchedIsLogged`, `TestOUT13_NoOutputDeviceMatchesAtConnectAndReload`,
`TestOUT13_OutputDeviceGoneIsLogged`, `TestOUT13_OutputReleasedIsLogged`,
`TestOUT01_OutputNoControlHoldsIsReleased`,
`TestOUT05_OutputThatStillMatchesAfterARenameIsKept`,
`TestCTRL06_OutputOwnershipEchoesReportedGeneration`, and `FuzzMixer` with outputs on
slider 7 and knob 7;
`TestOUT01_OutputNeverControlsAPlaybackStream` and
`TestCFG20_SetupPassesFixedOutputsButNotDefault` in `internal/config/setup_test.go`;
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
Config: output-fixed
Type: Manual + Automated

```text
M6 -> muted -> restart -> still muted -> outside unmute -> saved unmute
M7 -> ignored: slider6 owns the same matched device
```

**Steps**
- [ ] Apply `output-fixed`; move slider 6 to the middle.
- [ ] Press M6; `pactl get-sink-mute apptrol-test-mic-sink` reports yes. Output LEDs
  remain off until PR 2c. M7 has no effect.
- [ ] Restart Apptrol; the output is still muted. Press M6; it becomes unmuted.
- [ ] Run `pactl set-sink-mute apptrol-test-mic-sink 1`; the journal records
  `muted outside Apptrol` on slider6. Move slider6; mute stays on.
- [ ] Press M6; the output becomes unmuted. Restart; it stays unmuted.
- [ ] Remove slider6 and slider7 assignments from the fixture's copied config and
  reload. Knob7 now owns the output. Set its mute from outside; move knob7; mute stays.
  M7 must not change the knob's mute. Restore the fixture afterwards.

Automated: `TestOUT06_MuteSavedPerControlAndRestored`,
`TestOUT05_OutsideMuteTakenOverOnSliderAndKnob`, `TestOUT05_ReplugRestoresPositionAndMute`,
`TestOUT04_ChangingOwnerAppliesNewOwnersState`,
`TestOUT04_SameLabelsOnDifferentDevicesDoNotConflict`, `TestOUT05_OutputMuteAttribution`,
and `TestIntegration_OUT06_OutputMuteAndOutsideChange`. Re-plug is exercised with a
silent server output; guarding against a competing late server mute restore is a
separate follow-up and is not claimed by this case.

**Result:** Pass / Fail / Blocked
**Notes:**
