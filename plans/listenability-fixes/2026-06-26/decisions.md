# Decisions (resolved)

## Decision 1: Should "applause" and "crowd noise" stay in non-music terms?
**Resolved: Keep but demote to penalty.**

Remove from `IsNonMusicMetadata()` hard-exclude list. Keep them as scoring penalties
in `collectTrackReasons()` so they still lower the content score but don't
hard-exclude tracks. Live albums survive, SFX libraries still get low scores.

## Decision 2: How aggressive should the LongformMaxSeconds increase be?
**Resolved: 2700s (45 min).**

- `LongformMaxSeconds` = 2700 (was 1500)
- `PreferredMaxSeconds` = 1800 (was 900)

## Decision 3: Channel dump detection
**Resolved: Title patterns + album shape.**

When an album meets the short-track hard-exclusion criteria AND has
`channel_dump_title_pattern` on its tracks, demote to "longform_candidate" instead
of hard-excluding.

## Decision 4: Auto-restart scope
**Resolved: None — add restart command only.**

Add a `CmdRestartWorker` control command that accepts a worker label pattern
(e.g., "cleaner-1") and spawns a replacement. This is manual but prevents
masking systemic bugs.
