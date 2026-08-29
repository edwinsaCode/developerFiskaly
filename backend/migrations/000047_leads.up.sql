-- PS-3 — Lead / Prospect (blueprint §2: SEAM/CRM ringan, kini diaktifkan).
-- Jembatan funnel: Lead → Customer → Booking → PPJB → BAST (CFO G4/Q14, Q42).
-- Master murni: TANPA ledger, TANPA jurnal. bookings.lead_id (Increment 7)
-- kini punya tabel rujukan (tetap logical FK — konsisten keputusan awal).

CREATE TABLE IF NOT EXISTS leads (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id       BIGINT UNSIGNED NOT NULL,
    name            VARCHAR(200)    NOT NULL,
    phone           VARCHAR(30)     NOT NULL DEFAULT '',
    email           VARCHAR(120)    NOT NULL DEFAULT '',
    source          VARCHAR(30)     NOT NULL DEFAULT 'other'
                        COMMENT 'walk_in|referral|online|ads|expo|other',
    status          VARCHAR(20)     NOT NULL DEFAULT 'new',
    sales_person_id BIGINT UNSIGNED NULL COMMENT 'penanggung jawab (atribusi funnel)',
    project_id      BIGINT UNSIGNED NULL COMMENT 'proyek yang diminati',
    customer_id     BIGINT UNSIGNED NULL COMMENT 'terisi saat converted',
    notes           VARCHAR(1000)   NOT NULL DEFAULT '',
    lost_reason     VARCHAR(500)    NOT NULL DEFAULT '',
    created_by      BIGINT UNSIGNED NULL,
    converted_at    DATETIME(3)     NULL,
    created_at      DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at      DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    KEY idx_leads_tenant (tenant_id),
    KEY idx_leads_status (tenant_id, status),
    KEY idx_leads_sales  (tenant_id, sales_person_id),
    CONSTRAINT chk_leads_status CHECK (
        status IN ('new','contacted','qualified','converted','lost')
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
