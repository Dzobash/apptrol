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

Test: `TestOUT12_ListShowsOutputDevices` in `cmd/apptrol/list_test.go`;
`TestOUT12_SinkFromReply`, `TestOUT12_OutputChanges` in
`internal/audio/pulse/convert_test.go`; `TestIntegration_OUT12_OutputDevices` and
`TestIntegration_List` in `internal/audio/pulse/integration_test.go` (a silent test
output that appears and goes, against a real PipeWire);
`TestOUT12_MixerKeepsOutputDevices`, `TestOUT13_OutputDevicesOnNoControlAreLoggedAtDebug`
in `internal/mixer/output_test.go`.

```text
Output devices (* = the system's default). Put part of DESCRIPTION or NAME into an output's match list:
    DESCRIPTION        NAME                     VOLUME
  * Built-in Speakers  alsa_output.speakers     45%
    GoXLR Music        alsa_output.goxlr-music  100% muted
```

**Result:** Pass / Fail / Blocked
**Notes:**
