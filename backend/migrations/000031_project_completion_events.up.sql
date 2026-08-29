-- P0-4 (Req #3 / D3) — Project completion sebagai business event.
--
-- Completion adalah EVENT AKUNTANSI (bukan sekadar tanggal): menandai konstruksi
-- selesai lalu mengunci biaya aktual final (A_c) yang menjadi basis true-up.
--
-- State machine (IMPL-3): draft → completed → finalized.
--   draft      : proses completion dimulai
--   completed  : konstruksi selesai (completed_at diisi)
--   finalized  : biaya aktual dikunci (actual_cost_finalized_at) → true-up boleh calculate
-- Guard transisi ditegakkan di service layer (P2); `finalized` terminal-maju.
--
-- D3: phase_id NULLABLE → scope = whole project (NULL) atau fase tertentu.
--     Implementasi pertama project-level (phase_id selalu NULL).
-- Satu completion per scope: UNIQUE (tenant, project, phase_key) dengan phase_key
-- generated (COALESCE(phase_id,0)) — MySQL memperlakukan NULL sebagai distinct
-- sehingga UNIQUE biasa tak menjangkau project-level; generated key menutupnya.

CREATE TABLE project_completion_events (
    id                       BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id                BIGINT UNSIGNED NOT NULL,
    project_id               BIGINT UNSIGNED NOT NULL,
    phase_id                 BIGINT UNSIGNED NULL,
    status                   VARCHAR(20)     NOT NULL DEFAULT 'draft'
                                 COMMENT 'draft | completed | finalized',
    completed_at             DATETIME(3)     NULL COMMENT 'tanggal konstruksi selesai (tanggal akuntansi)',
    completed_by             BIGINT UNSIGNED NULL,
    actual_cost_finalized_at DATETIME(3)     NULL COMMENT 'saat biaya aktual dikunci (A_c final)',
    created_by               BIGINT UNSIGNED NULL,
    created_at               DATETIME(3)     NULL,
    updated_at               DATETIME(3)     NULL,

    phase_key                BIGINT UNSIGNED AS (COALESCE(phase_id, 0)) STORED,

    PRIMARY KEY (id),
    INDEX idx_pce_tenant  (tenant_id),
    INDEX idx_pce_project (tenant_id, project_id),
    UNIQUE KEY uq_pce_scope (tenant_id, project_id, phase_key),

    CONSTRAINT chk_pce_status CHECK (status IN ('draft','completed','finalized'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='P0-4 D3: event completion proyek/fase; gate true-up';
