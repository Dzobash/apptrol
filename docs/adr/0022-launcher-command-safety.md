# 0022. Launcher commands: the user's responsibility, and a denylist of catastrophic commands

- **Status:** Accepted
- **Date:** 2026-10-02

## Context

A launcher can run any `command` the user writes in `config.toml` (ADR 0019). It runs
with the user's rights, through systemd, without a shell unless the user asks for one
(`["sh", "-c", "…"]`). A command copied from a forum or a joke ("`rm -rf /` removes the
French language pack") can destroy the user's files or the system with one button press.

What already limits the damage, by design:

- Commands run as the user, never as root, and only from the user's own file. Anyone who
  can edit that file can already run anything as the user (e.g. through `~/.bashrc`), so
  there is no attacker to keep out: the risk is the user's own mistake.
- `sudo`, `su` and `doas` ask for the password in a terminal; a launcher has none, so
  they cannot work anyway. `pkexec`, `run0`, `kdesu` and `lxqt-sudo` ask in a dialog, so
  every use of root needs the user's consent. Installed apps that need root (Synaptic,
  KSystemLog and software settings on the reference system) start this way through their
  desktop ID.

Researched on 2026-10-02:

- Linux security guides and the guardrails that AI coding agents run their commands
  through agree on a short list of irreversible commands: deleting everything (`rm -rf /`,
  `~`), wiping a disk (`mkfs`, `dd` to a device, writing to `/dev/sda`), the fork bomb,
  running a download (`curl … | sh`), changing rights on everything (`chmod -R 777 /`),
  and `sudo`. Their design rule: block only the catastrophic, because over-blocking
  makes a tool useless.
- Denylists are fragile. Research published in June 2026 (GuardFall, ShellSieve) found
  that 69–99 % of real-world denylists could be bypassed, and that shell quoting tricks
  beat the guards of 10 of 11 popular coding agents: the guard reads the text, but the
  shell rewrites it before running it. OWASP recommends allow-lists over deny-lists for
  command input.

Alternatives considered:

- **Only a disclaimer.** Honest, but does nothing against the copy-paste accident.
- **An allow-list** of permitted programs. The most secure, but it would make launchers
  useless for what they are for: any program the user chooses.
- **A short denylist plus a disclaimer.** Catches the known catastrophic commands where
  they are easy to recognise, and says plainly that it is not protection.

## Decision

1. **The user is responsible** for the commands they configure. The README (Disclaimer),
   docs/config.md (Launchers) and the example configuration say so, and that the authors
   accept no liability. This follows the MIT license's "as is, without warranty".
2. **Validation refuses** a launcher `command` from these groups (LAUNCH-12):

   | Group | Examples |
   |---|---|
   | Delete everything | `rm` with `-r` and `-f` (in any form) on `/`, `/*`, `~`, `$HOME` or `/home` |
   | Wipe a disk | `mkfs*`, `wipefs`, `dd … of=/dev/…`, writing to `/dev/sd*`, `nvme*`, … |
   | Fork bomb | `:(){ :\|:& };:` and the same with any name |
   | Download and run | `curl` or `wget` piped into a shell or interpreter |
   | Rights on everything | `chmod -R` or `chown -R` on `/` |
   | Root in a terminal | `sudo`, `su`, `doas` |

   Everything else is allowed. The check reads the arguments and the words of any shell
   text in them, split into commands at `;` `|` `&` `` ` `` and `$(`; it counts a word only
   in program position (first, or after a wrapper such as `env`, `nohup`, `pkexec`, after
   `-c`, or after a variable assignment), so a message that merely contains "su" passes.
   `pkexec rm -rf /` is still refused: the dialog does not make the rest safe.
3. **Only commands are checked.** Desktop IDs start installed apps, whose `Exec` line
   comes from a package, not from the user.
4. **A refused command makes the whole file invalid**, like any other configuration error
   (CFG-07): Apptrol keeps the previous valid configuration, or waits for a valid one at
   start, and logs the line and the reason. A disabled button with only a warning would
   be too easy to miss.
5. **The error names the group and points to the list**, e.g.
   `layouts.default.buttons.r4.command: blocked: deletes all your files ("rm -rf /" or
   "rm -rf ~"); see "Blocked commands" in docs/config.md`. docs/config.md lists every
   group with examples, so a user who tries knows why it does not work.
6. **It is a safety net against accidents, not security**, and the documentation says so:
   plain argument lists are checked reliably, because no shell rewrites them; text for a
   shell can always be disguised and is checked on a best-effort basis.

### Logging

7. A refused command is a configuration error, logged like the others (ADR 0016):

   | Level | Message | Attributes |
   |---|---|---|
   | error | "invalid configuration; waiting for a valid one (run apptrol check)" at start, "the changed configuration is invalid; keeping the current settings" on reload | `file.path`, `error.type=config_invalid`, `exception.message` = the line above |

   `apptrol check` prints the same line. No new attributes or error types.

## Consequences

- The known catastrophic commands cannot be started by a button, however they were
  copied into the file.
- Someone determined can still get past the check with shell text; the documentation
  does not promise otherwise.
- The list may need additions; each is a dated note here and a row in docs/config.md.
- The check is fuzz-tested (QA-08), so no command, however odd, crashes validation.

Sources: [sysxplore, 12 destructive Linux commands](https://sysxplore.substack.com/p/12-destructive-linux-commands-every);
[phoenixNAP, dangerous Linux terminal commands](https://phoenixnap.com/kb/dangerous-linux-terminal-commands);
[global agent guardrails](https://www.skills.sh/davidondrej/skills/global-agent-guardrails);
[GuardFall and ShellSieve on denylist fragility](https://codex.danielvaughan.com/2026/07/26/guardfall-shellsieve-denylist-fragility-coding-agents-codex-cli-os-level-sandbox-defence/);
[Contrast Security, command injection](https://contrastsecurity.dev/docs/learn-devsec/vulnerabilities/command-injection);
[CWE-184, incomplete list of disallowed inputs](https://hub.corgea.com/vulnerabilities/CWE-184).
