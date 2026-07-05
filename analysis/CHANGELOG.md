# Strategy Changelog

## 2025-07-07 — Remove session filter

**Change:** Removed the London/NY session filter. Bot now trades all hours including Tokyo (23:00–06:59 UTC). Only the dead session (22:00–22:59 UTC) is still blocked via the existing EOD window.

**Why:** v1 baseline analysis (2025-07-05) showed that removing the session filter raised win rate from 44.6% to 47.2% (+2.6%) and added 37 more entries over 14 days. Tokyo-session setups within the combined strategy were winning more often than London/NY ones, which contradicted the original assumption about Tokyo being noisier.

**Data that justified this:** analysis/v1_baseline_2025-07-05/report.html — Test 2, "Remove session filter" row.

**What to watch:** Re-run Test 1 and Test 2 after 2–3 weeks of live data under this change. Create analysis/v2_no_session_YYYY-MM-DD/ with updated results.

**Files changed:** internal/bot/strategy.go, internal/bot/bot.go
