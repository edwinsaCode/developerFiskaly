-- Rollback Increment 5.1 — Approval Workflow Hardening.
-- Baris action 'delegate'/'cancel' dihapus sebelum CHECK lama dikembalikan
-- (dev-only rollback; audit trail produksi tidak pernah di-rollback).

DELETE FROM approval_actions WHERE decision IN ('delegate', 'cancel');

ALTER TABLE approval_actions
    DROP CHECK chk_aa_decision;
ALTER TABLE approval_actions
    ADD CONSTRAINT chk_aa_decision CHECK (decision IN ('approve', 'reject'));

ALTER TABLE approval_requests
    DROP COLUMN workflow_revision,
    DROP COLUMN workflow_snapshot,
    DROP COLUMN target_snapshot;

ALTER TABLE approval_steps
    DROP COLUMN escalation_after_hours,
    DROP COLUMN escalation_role;

ALTER TABLE approval_workflows
    DROP COLUMN revision;
