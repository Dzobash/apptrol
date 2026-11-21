# Logging test cases

### TC-LOG-001 — The log file keeps going after it was deleted or a rotation failed

Covers: LOG-06, LOG-07 · Phase 1.7
Config: none (Go test)
Type: Automated

Test: in `internal/logging/rotate_test.go`: `TestLOG07_RotatesAfterLogFileWasRemoved`,
`TestLOG07_FailedRotationKeepsLoggingAndRetriesLater`, `TestLOG07_ReopensAfterFailedReopen`,
`TestLOG07_ReopenedFileAfterMoveRotatesAtMaxSize`, `TestLOG07_FailedRetriesKeepTheArchives`,
`TestLOG07_FailedShiftKeepsTheOpenFile`, `TestLOG07_ReplacedLogFileKeepsTheArchive`,
`TestLOG07_FileGoneDuringShiftKeepsTheArchive`.

A deleted `apptrol.log`, or its deleted folder, is started again at the next rotation,
and the archives stay; so does a file created in its place. When the old file cannot be
moved, the record is still written to it, and the rotation is tried again once the
file has grown by another `max_size`; failed tries do not remove the remaining
archives. When the old file was moved but the new one cannot be created, that record is
lost and each next record tries to create the file again. Only an explicit close stops writing ([ADR 0008](../../adr/0008-logging.md), #250).

```text
apptrol.log full ─► rotation fails ─► record kept in apptrol.log
                                      │
                 grown by another max_size ─► rotate again
```

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-LOG-002 — A reload keeps file logging and every record

Covers: LOG-08 · Phase 1.7
Config: none (Go test)
Type: Automated

Test: in `internal/logging/logging_test.go`:
`TestLOG08_ReloadKeepsUnavailableFileForRecovery`,
`TestLOG08_ReloadRevivesOrReplacesTheSameFile`,
`TestLOG08_ReconfigureKeepsRecordsBeingWritten`,
`TestLOG08_ReloadFromAFormattedValueDoesNotBlock`,
`TestLOG08_ReloadBackToAPathTakesItsFileAgain`,
`TestLOG08_ConcurrentReloadsKeepAWorkingFile`,
`TestLOG08_StuckWriteDoesNotBlockReload`,
`TestLOG08_ReloadAfterCloseTakesTheDrainingFile`,
`TestLOG08_StuckRotationBlocksNoReload`,
`TestLOG08_ReloadHandsLimitsToAWriteInProgress`,
`TestLOG08_ReloadDuringAFailedReopenFallsBack`,
`TestLOG08_ReloadDuringARotationReportsNoError`.

A reload with the same path reopens a file that could not be reopened. If it still
cannot be opened, or a record is just retrying to open it, the reload reports it
(`could not apply the log settings`) and also logs to the journal until the next reload;
the file comes back with the next record once it can be opened.
A record being written while a reload moves the file still reaches the old file, which
is closed after it; the reload does not wait for it, nor for a write that is stuck,
also when it returns to that file. A reload back to that path takes the same file
again, and reloads at the same time never leave a closed file in use ([ADR 0008](../../adr/0008-logging.md), #250).

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-LOG-003 — A deleted log file comes back without a restart

Covers: LOG-06, LOG-07 · Phase 1.7
Config: log-file
Type: Manual

**Setup**
- [ ] Test tone A plays; a terminal is open in the fixture's log folder: `cd ~/.local/state/apptrol`
  (the fixture's path is fixed; `$XDG_STATE_HOME` does not move it)

**Steps**
- [ ] Run `date +%T` → note the time
- [ ] Move slider 1 slowly from the bottom to the top and back, again and again, for up to a minute, until `ls -l --time-style=+%T apptrol-test.log.1` shows a time after the noted one → the log has rotated; `apptrol-test.log` has `volume changed` lines with `apptrol.control=slider1`
- [ ] Run `systemctl --user show -p InvocationID --value apptrol` → note the ID of the running service
- [ ] Run `rm apptrol-test.log` → `ls apptrol-test.log` says it does not exist
- [ ] Move slider 1 slowly up and down again, for up to a minute, until `ls apptrol-test.log` finds the file → it is back
- [ ] Move slider 1 a little, then run `grep "volume changed" apptrol-test.log | tail -n 1` → a line with the time of that move
- [ ] Run `systemctl --user show -p InvocationID --value apptrol` → the same ID as noted: Apptrol was not restarted
- [ ] Run `journalctl --user -u apptrol --since "<noted time>" | grep -E "Apptrol starting|configuration reloaded"` → no output: no start and no reload since the case began
- [ ] Run `journalctl --user -u apptrol --since "<noted time>" -p warning` → no warning or error about the log file

**Result:** Pass / Fail / Blocked
**Notes:**
