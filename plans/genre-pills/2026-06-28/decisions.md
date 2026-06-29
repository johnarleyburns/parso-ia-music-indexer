# Decisions

D1. Pill set granularity -> Coverage-gated dynamic set (~14 pills). (confirmed)
D2. Classical gap -> Prioritize musopen indexing so classical pills can populate. (confirmed)
D3. Pill storage -> DB table `pills`, seeded from embedded JSON, tunable in DB. (confirmed)
D4. `min_library_count` default -> 10 listenable albums. (confirmed)
D5. Implement all 4 phases in one pass. (confirmed)

## Constraints derived
- Keyword source = subjects + track tags (genres field is near-empty).
- db package must not import clap (avoid cycle); CLAP orchestration in cmd/tui.
- Seeding uses INSERT OR IGNORE so in-DB tuning is never overwritten.
