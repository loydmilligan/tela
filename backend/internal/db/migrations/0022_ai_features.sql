-- 0022_ai_features.sql — add the managed-AI ("ask your docs") feature flag.
--
-- ask_docs is the boolean entitlement for the cloud-backed LLM: the managed chat
-- proxy (/api/cloud/llm/v1/chat/completions, gated by featureEnabled "ask_docs")
-- and, on the provider instance, the /api/rag/ask endpoint. It sits beside
-- managed_rag (migration 0021) in plans.features and is flagged on the same paid
-- tiers. As with managed_rag, on a self-host instance the LLM is BYO and not
-- plan-gated — the flag is advisory until the cloud-connect entitlement path
-- reads it. jsonb_set merges the key without disturbing existing flags.

UPDATE plans
   SET features = jsonb_set(features, '{ask_docs}', 'true', true)
 WHERE key IN ('personal_plus', 'personal_unlimited', 'org_team', 'org_enterprise');
