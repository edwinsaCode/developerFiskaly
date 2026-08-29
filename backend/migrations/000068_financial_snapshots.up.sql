-- W-6 — Snapshot Keuangan Historis.
--
-- Kondisi laporan keuangan tahun-tahun SEBELUM sistem ini dipakai. Data ini
-- MURNI PELAPORAN: tidak ada jurnal, tidak menyentuh journal_entries /
-- journal_lines, dan tidak dibaca oleh perhitungan mana pun (dashboard, trial
-- balance, AR, HPP, tutup buku). Satu tahun = satu snapshot yang berdiri
-- sendiri — snapshot 2025 BUKAN akumulasi 2021–2025.

CREATE TABLE financial_snapshots (
  id           BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  tenant_id    BIGINT UNSIGNED NOT NULL,
  fiscal_year  SMALLINT NOT NULL,
  status       VARCHAR(10) NOT NULL DEFAULT 'draft',
  -- revision naik setiap kali snapshot difinalkan (0 = belum pernah final).
  revision     INT NOT NULL DEFAULT 0,
  notes        VARCHAR(500) NULL,
  finalized_at DATETIME(3) NULL,
  finalized_by BIGINT UNSIGNED NULL,
  created_by   BIGINT UNSIGNED NULL,
  updated_by   BIGINT UNSIGNED NULL,
  created_at   DATETIME(3) NULL,
  updated_at   DATETIME(3) NULL,
  UNIQUE KEY uk_snapshot_year (tenant_id, fiscal_year),
  KEY idx_snapshot_tenant (tenant_id),
  CONSTRAINT chk_snapshot_status CHECK (status IN ('draft','final'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Baris snapshot. account_code/name/type adalah SNAPSHOT nilai master saat
-- baris dibuat (pola yang sama dengan charge_items.deposit_account_code):
-- laporan tahun lalu tidak boleh berubah isinya karena COA diedit setelahnya.
CREATE TABLE financial_snapshot_lines (
  id           BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  tenant_id    BIGINT UNSIGNED NOT NULL,
  snapshot_id  BIGINT UNSIGNED NOT NULL,
  statement    VARCHAR(20) NOT NULL,
  account_id   BIGINT UNSIGNED NOT NULL,
  account_code VARCHAR(20) NOT NULL,
  account_name VARCHAR(200) NOT NULL,
  account_type VARCHAR(20) NOT NULL,
  -- Nominal dalam ARAH NORMAL akun (Kas 150jt = 150jt, bukan debit/kredit).
  -- Boleh negatif: defisit akumulasi dan rugi tahun berjalan itu nyata.
  amount       DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
  sort_order   INT NOT NULL DEFAULT 0,
  created_at   DATETIME(3) NULL,
  updated_at   DATETIME(3) NULL,
  UNIQUE KEY uk_snapshot_account (snapshot_id, account_id),
  KEY idx_snapshot_lines_tenant (tenant_id),
  KEY idx_snapshot_lines_snapshot (snapshot_id),
  CONSTRAINT chk_snapshot_statement CHECK (statement IN ('balance_sheet','income_statement'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Jejak audit APPEND-ONLY. Tidak ada uang yang bergerak di sini, jadi yang
-- harus bisa dipertanggungjawabkan adalah "siapa mengubah laporan tahun berapa,
-- kapan, menjadi berapa" — bukan diff per akun.
CREATE TABLE financial_snapshot_audits (
  id                BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  tenant_id         BIGINT UNSIGNED NOT NULL,
  snapshot_id       BIGINT UNSIGNED NOT NULL,
  event             VARCHAR(20) NOT NULL,
  revision          INT NOT NULL DEFAULT 0,
  reason            VARCHAR(500) NULL,
  total_assets      DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
  total_liab_equity DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
  net_income        DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
  line_count        INT NOT NULL DEFAULT 0,
  actor_id          BIGINT UNSIGNED NULL,
  created_at        DATETIME(3) NULL,
  updated_at        DATETIME(3) NULL,
  KEY idx_snapshot_audit (tenant_id, snapshot_id),
  CONSTRAINT chk_snapshot_audit_event CHECK (event IN ('created','saved','finalized','reopened'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
