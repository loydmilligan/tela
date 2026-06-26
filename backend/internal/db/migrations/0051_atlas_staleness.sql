-- 0051_atlas_staleness.sql — decouple drift DETECTION from REGENERATION.
--
-- Before: the freshness poller probed + regenerated in one step on the project
-- cadence. Now a cheap ~15-min `ls-remote`/jira-count probe records per-source
-- staleness (so the UI shows "behind" and Manual projects get a useful nudge),
-- while regeneration runs on the slower project cadence and only for sources
-- actually behind. `ref` stays the last *generated* ref; staleness = the probe
-- saw upstream move past it.
ALTER TABLE atlas_sources ADD COLUMN stale_since         TEXT NOT NULL DEFAULT '';  -- '' = docs match upstream; set when a probe sees upstream move past ref, cleared on regen
ALTER TABLE atlas_sources ADD COLUMN upstream_checked_at TEXT NOT NULL DEFAULT '';  -- last detection-probe time (gates the 15-min cadence)

-- Detection is always-on and free, so default new projects to hourly auto-regen.
ALTER TABLE atlas_projects ALTER COLUMN cadence SET DEFAULT 'hourly';
