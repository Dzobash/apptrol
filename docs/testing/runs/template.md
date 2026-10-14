# Test run vX.Y.Z

The blank record of a manual test run. Copy it to `runs/vX.Y.Z.md` for each release (see
[Running the release tests](../how-to-run-release-tests.md)) and leave this file as it is.

This record is public: no device names or IDs, process numbers, user names, home paths or
media titles. Use placeholders (`<pid>`, `<device>`, `/home/you`).

| | |
|---|---|
| **Version** | vX.Y.Z-rcN (the `service.version` in the log, not only `apptrol --version`) |
| **Date** | YYYY-MM-DD |
| **Distribution** | e.g. Ubuntu 26.04, KDE Plasma 6.x |
| **PipeWire** | e.g. 1.4.x (`pactl info`, *Server Name*) |
| **Hardware** | e.g. Korg nanoKONTROL2, USB 2 port |
| **Install** | `.deb`, `.rpm` or built from source |

## Results

One row per case, in the order run. Result: `Pass`, `Fail` or `Blocked`; a note for
anything but a plain pass, with the issue for a failure.

| ID | Fixture | Result | Notes |
|---|---|---|---|
| TC-…-001 | two-apps | | |

## Exploratory charter

```text
Explore   <the area or feature>
with      <the setup, the fixture, the tools>
to find   <the kind of problem>
```

**Time:** 30 minutes

**Notes:** what was tried, what was found.

## Summary

- **Passed:** n of m
- **Failed:** the case IDs, each with its issue
- **Blocked:** the case IDs, each with the reason
- **Release:** go / no-go, and why
