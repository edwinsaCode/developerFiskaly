-- Increment 5.1 — Approval Workflow Hardening (pasca-review Increment 5).
-- ADD-only; tanpa breaking change; approval tetap governance (tanpa jurnal).
--
-- H3: approval_workflows.revision — revisi konfigurasi per (tenant, target_type).
--     Baris workflow immutable-by-construction (tidak ada jalur edit step/nama);
--     "mengubah workflow" = nonaktifkan lama + baris baru (revision naik).
-- H1: approval_requests.workflow_snapshot — SNAPSHOT beku (nama, revision,
--     steps: role/quorum/threshold/escalation) saat request dibuat. Evaluasi
--     keputusan MEMBACA SNAPSHOT, bukan workflow hidup (pola Terms Snapshot
--     scheme / Tax Rule revision / Allocation Snapshot).
-- H2: approval_requests.target_snapshot — konteks minimum dokumen target saat
--     submit (amount, currency, target_version seam). Business-rule invalidation
--     BELUM diimplementasikan — hanya snapshot + seam.
-- H4: approval_steps.escalation_after_hours / escalation_role — seam eskalasi
--     (tanpa scheduler); CHECK decision menampung 'delegate' & 'cancel'.
-- Blind spot audit: cancel kini meninggalkan baris action (decision='cancel').

ALTER TABLE approval_workflows
    ADD COLUMN revision INT NOT NULL DEFAULT 1
        COMMENT 'revisi konfigurasi per (tenant, target_type); workflow pengganti = revision berikutnya';

ALTER TABLE approval_steps
    ADD COLUMN escalation_after_hours INT NOT NULL DEFAULT 0
        COMMENT 'seam eskalasi: jam sebelum eskalasi (0 = tanpa eskalasi; scheduler menyusul)',
    ADD COLUMN escalation_role VARCHAR(30) NOT NULL DEFAULT ''
        COMMENT 'seam eskalasi: role tujuan eskalasi';

ALTER TABLE approval_requests
    ADD COLUMN workflow_revision INT NULL
        COMMENT 'H1/H3: revisi workflow yang dibekukan saat submit (NULL = request pra-hardening)',
    ADD COLUMN workflow_snapshot JSON NULL
        COMMENT 'H1: snapshot beku workflow (name, revision, steps: seq/role/quorum/min_amount/escalation)',
    ADD COLUMN target_snapshot JSON NULL
        COMMENT 'H2: konteks minimum target saat submit (amount, currency, target_version seam)';

-- Vocabulary decision diperluas (delegate = seam H4; cancel = jejak audit
-- pembatalan). MySQL: ganti CHECK lama.
ALTER TABLE approval_actions
    DROP CHECK chk_aa_decision;
ALTER TABLE approval_actions
    ADD CONSTRAINT chk_aa_decision CHECK (
        decision IN ('approve', 'reject', 'delegate', 'cancel')
    );

-- Backfill revision utk workflow existing: urutan id per (tenant, target_type).
UPDATE approval_workflows w
JOIN (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY tenant_id, target_type ORDER BY id) AS rn
    FROM approval_workflows
) x ON x.id = w.id
SET w.revision = x.rn;
