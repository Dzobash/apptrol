# 0016. Log records follow the OpenTelemetry semantic conventions

- **Status:** Accepted
- **Date:** 2026-09-30

## Context

ADR 0008 chose the log library and outputs, but not what a log record contains. Testing
v0.1.0-rc2 showed the result: every part of Apptrol named its attributes its own way
(`err`, `file`, `device`, `app`, `percent`), nothing said which part of Apptrol a line came
from, and an invalid configuration was logged as one record spanning several lines,
which the terminal output draws as a box and the journal splits into unrelated lines.

The [OpenTelemetry semantic conventions](https://opentelemetry.io/docs/concepts/semantic-conventions/)
are a widely used standard for naming what telemetry records contain. Following them now,
before the first release, costs little and keeps the names stable for anyone who later
filters or ships Apptrol's logs with standard tools.

## Decision

Every log record Apptrol writes follows these rules (requirements LOG-10 to LOG-13):

1. **Message**: a short, fixed text that says what happened, in lower case (names such as
   "Apptrol" keep their capitals). Values never go into the message; they are attributes.
   A hint on what to do may follow in the message after a semicolon or in parentheses.
2. **One line per record.** Line breaks in messages and values are replaced by `; `. Where
   there are several problems (an invalid configuration), each is its own record.
3. **Component**: every record carries `apptrol.component`, one of `service`, `config`,
   `state`, `audio`, `controller`, `mixer`.
4. **Attribute names** follow the
   [OpenTelemetry naming rules](https://opentelemetry.io/docs/specs/semconv/general/naming/):
   lower case, dot-separated namespaces, words within a part joined by underscores.
   - Where the conventions define an attribute, it is used with its name and meaning:
     `error.type`, `exception.message`, `file.path`, `url.full`, `service.name`,
     `service.version`, `process.executable.name`.
   - Everything else lives in the `apptrol.` namespace, e.g. `apptrol.control`,
     `apptrol.app.name`, `apptrol.volume_percent`, `apptrol.controller.device`.
   - All names are constants in `internal/logattr`; no log call writes a name itself.
5. **Errors** ([recording errors](https://opentelemetry.io/docs/specs/semconv/general/recording-errors/),
   [exceptions in logs](https://opentelemetry.io/docs/specs/semconv/exceptions/exceptions-logs/)):
   a record about an error carries `error.type`, a fixed snake_case word for the kind of
   error (`config_invalid`, `controller_busy`, `audio_connection_lost`, …), and the error's
   text as `exception.message`. Every `error` record has an `error.type`.
6. **Context**: a record about a control says everything needed to understand it on its
   own: `apptrol.layout`, `apptrol.control` and, if the control is assigned,
   `apptrol.app.id`, `apptrol.app.name` and `apptrol.app.type`. A record caused by a
   button names it (`apptrol.button`, e.g. `M2`). Every button press is logged: mute and
   solo at info, buttons without a function (R, transport, M or S on a column without a
   suitable app) at debug.
7. **Levels** stay as LOG-09 defines them; they correspond to the OpenTelemetry severities
   DEBUG (5), INFO (9), WARN (13) and ERROR (17).

Considered and left out for now:

- `event.name` on every record: the fixed message already identifies the event, and the
  extra attribute would make every line longer without a consumer that needs it.
- `service.name` and `service.version` on every record: in OpenTelemetry they describe the
  *resource*, not each record. They appear once, on the start record; the journal already
  names the unit on every line.
- Exporting logs over OTLP: Apptrol writes to the journal and a file; nothing collects
  OTLP on a desktop. The names chosen here map directly if that is ever added.

## Consequences

- Lines are longer, mostly because of `apptrol.component=…`; in return, each line says
  where it comes from and can be filtered, e.g.
  `journalctl --user -u apptrol | grep apptrol.component=controller` or, in a JSON log file,
  by `error.type`.
- Tests enforce the rules: `internal/logattr` checks that every log call uses its
  constants and a one-line message, and the service tests fail on any record without a
  component, with a badly formed name, or an error without `error.type`.
- The logging layer flattens and joins lines itself, so the rules hold in every output
  format (text, logfmt, JSON) regardless of how the formatter treats groups or line breaks.
- New attributes and error types need a constant in `internal/logattr`, a row in the user
  guide [`docs/logging.md`](../logging.md) (a test checks this), and, if they come from
  the OpenTelemetry conventions, an entry in the test's list of OpenTelemetry names.
- The logs can be collected by log stores (Loki, Elasticsearch) without mapping: timestamps
  have milliseconds, each attribute keeps one type, and no name is the start of another
  (Elasticsearch turns dotted names into nested objects). Tests check both rules.
- Once layouts can be switched (Phase 2), `apptrol.layout` already tells which layout a
  record is about; nothing in the format changes.
