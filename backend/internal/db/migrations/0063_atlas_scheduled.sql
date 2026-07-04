-- Scheduled Atlas auto-regen is a paid capability: it runs the heaviest AI
-- pipeline (fetch + embed + LLM per source) on a cadence whether or not the
-- owner is online, so on a single-box instance it's the main sustained-load
-- lever. Free plans get Atlas on manual refresh only; the paid tiers — and the
-- registration trial, which grants personal_plus — keep scheduled regen.
--
-- Gate: plans.features.atlas_scheduled, checked managed-cloud-only in the
-- scheduler (atlas_scheduler.go scheduledAtlasAllowed). Free plans simply omit
-- the flag (featureEnabled fails closed → skipped).
UPDATE plans
   SET features = features || '{"atlas_scheduled": true}'::jsonb,
       updated_at = tela_now()
 WHERE key IN ('personal_plus', 'personal_unlimited', 'org_team', 'org_enterprise');
