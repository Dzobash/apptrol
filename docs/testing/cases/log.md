# Logging test cases

### TC-LOG-001 — The log file keeps going after it was deleted or a rotation failed

Covers: LOG-06, LOG-07 · Phase 1.7
Config: minimal
Type: Automated

Test: in `internal/logging/rotate_test.go`: `TestLOG07_RotatesAfterLogFileWasRemoved`,
`TestLOG07_FailedRotationKeepsLoggingAndRetriesLater`, `TestLOG07_ReopensAfterFailedReopen`,
`TestLOG07_ReopenedFileAfterMoveRotatesAtMaxSize`, `TestLOG07_FailedRetriesKeepTheArchives`,
`TestLOG07_FailedShiftKeepsTheOpenFile`.

A deleted `apptrol.log`, or its deleted folder, is started again at the next rotation,
and the archives stay. After any other failure the record is still written to
`apptrol.log` (the open file, if the files cannot be renamed), and the rotation is tried
again once the file has grown by another `max_size`; failed tries do not remove the
remaining archives. Only an explicit close stops writing ([ADR 0008](../../adr/0008-logging.md), #250).

```text
apptrol.log full ─► rotation fails ─► record kept in apptrol.log
                                      │
                 grown by another max_size ─► rotate again
```

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-LOG-002 — A reload keeps file logging and every record

Covers: LOG-08 · Phase 1.7
Config: minimal
Type: Automated

Test: in `internal/logging/logging_test.go`:
`TestLOG08_ReloadKeepsUnavailableFileForRecovery`,
`TestLOG08_ReloadRevivesOrReplacesTheSameFile`,
`TestLOG08_ReconfigureKeepsRecordsBeingWritten`,
`TestLOG08_ReloadFromAFormattedValueDoesNotBlock`,
`TestLOG08_ReloadBackToAPathTakesItsFileAgain`,
`TestLOG08_ConcurrentReloadsKeepAWorkingFile`,
`TestLOG08_StuckWriteDoesNotBlockReload`.

A reload with the same path reopens a file that could not be reopened. If it still
cannot be opened, the reload reports it (`could not apply the log settings`), logs to the
journal meanwhile, and the file comes back with the next record once it can be opened.
A record being written while a reload moves the file still reaches the old file, which
is closed after it; the reload does not wait for it, nor for a write that is stuck. A
reload back to that path takes the same file again, and reloads at the same time never
leave a closed file in use ([ADR 0008](../../adr/0008-logging.md), #250).

**Result:** Pass / Fail / Blocked
**Notes:**
