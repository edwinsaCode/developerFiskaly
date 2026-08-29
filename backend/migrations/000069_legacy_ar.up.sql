-- W-7 — Piutang Proyek Lama (Legacy AR).
--
-- Saldo piutang dari proyek SEBELUM sistem ini dipakai. Tabel-tabel di sini
-- adalah BUKU PEMBANTU (sub-ledger) atas akun kontrol piutang yang saldonya
-- SUDAH ada di buku besar lewat Saldo Awal — persis peran payment_allocations
-- terhadap kas.
--
-- INV-LAR-4: import TIDAK PERNAH menulis journal_entries. Satu-satunya jurnal
-- yang lahir dari domain ini adalah jurnal PELUNASAN (Dr Kas / Cr akun kontrol)
-- beserta pembaliknya. Mengimpor rincian bukan kejadian ekonomi baru; menjurnal
-- ulang apa yang sudah ada di Saldo Awal berarti melipatgandakan aset.

-- ── Batch import ────────────────────────────────────────────────────────────
--
-- Satu berkas Excel = satu batch. Batch adalah jejak audit unggahan, BUKAN
-- wadah transaksi: yang atomik adalah commit-nya.
CREATE TABLE legacy_ar_batches (
  id                  BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  tenant_id           BIGINT UNSIGNED NOT NULL,
  file_name           VARCHAR(255) NOT NULL,
  file_size           BIGINT UNSIGNED NOT NULL DEFAULT 0,
  -- file_hash selalu terisi (SHA-256 isi berkas) untuk ditampilkan di riwayat.
  file_hash           CHAR(64) NOT NULL,
  -- committed_hash HANYA terisi saat commit berhasil. Unique key dipasang di
  -- kolom ini, bukan di file_hash: draft yang dibatalkan tidak boleh membuat
  -- berkas yang sama mustahil diunggah lagi selamanya.
  committed_hash      CHAR(64) NULL,
  -- as_of_date: tanggal cutoff posisi saldo. SATU per batch — membiarkannya
  -- per baris membuat satu berkas berisi lima tanggal dan tie-out ke buku besar
  -- kehilangan arti.
  as_of_date          DATE NOT NULL,
  control_account_code VARCHAR(20) NOT NULL,
  status              VARCHAR(12) NOT NULL DEFAULT 'draft',
  row_count           INT NOT NULL DEFAULT 0,
  valid_count         INT NOT NULL DEFAULT 0,
  error_count         INT NOT NULL DEFAULT 0,
  warning_count       INT NOT NULL DEFAULT 0,
  total_amount        DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
  -- opening_journal_id: jurnal saldo awal yang dibuat BERSAMA commit untuk
  -- menutup selisih terhadap buku besar. NULL = tidak ada (kasus normal:
  -- Saldo Awal sudah memuat piutangnya). Logical FK.
  opening_journal_id  BIGINT UNSIGNED NULL,
  -- skip_reason: alasan tertulis bila user memilih lanjut walau selisih ≠ 0.
  skip_reason         VARCHAR(500) NULL,
  notes               VARCHAR(500) NULL,
  created_by          BIGINT UNSIGNED NULL,
  committed_by        BIGINT UNSIGNED NULL,
  committed_at        DATETIME(3) NULL,
  created_at          DATETIME(3) NULL,
  updated_at          DATETIME(3) NULL,
  UNIQUE KEY uk_lab_committed_hash (tenant_id, committed_hash),
  KEY idx_lab_tenant (tenant_id),
  KEY idx_lab_status (tenant_id, status),
  CONSTRAINT chk_lab_status CHECK (status IN ('draft','committed','discarded'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ── Baris staging ───────────────────────────────────────────────────────────
--
-- Nilai disimpan MENTAH sebagai string, apa adanya dari berkas. Ketika klien
-- protes "angkanya bukan segitu", yang harus bisa dibuka adalah apa yang mereka
-- kirim — bukan hasil parsing kita.
CREATE TABLE legacy_ar_batch_rows (
  id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  tenant_id     BIGINT UNSIGNED NOT NULL,
  batch_id      BIGINT UNSIGNED NOT NULL,
  line_no       INT NOT NULL,
  raw_customer_name VARCHAR(255) NULL,
  raw_source_label  VARCHAR(255) NULL,
  raw_external_ref  VARCHAR(100) NULL,
  raw_outstanding   VARCHAR(64)  NULL,
  raw_due_date      VARCHAR(40)  NULL,
  raw_phone         VARCHAR(64)  NULL,
  raw_email         VARCHAR(190) NULL,
  raw_notes         VARCHAR(500) NULL,
  -- Hasil parse (hanya bermakna saat parse_status <> 'error').
  amount        DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
  due_date      DATE NULL,
  parse_status  VARCHAR(10) NOT NULL DEFAULT 'ok',
  -- errors: array JSON {column, message}. Warning ikut di sini dengan
  -- parse_status='warning' — duplikat alami tidak boleh memblokir import.
  errors        JSON NULL,
  legacy_receivable_id BIGINT UNSIGNED NULL,
  created_at    DATETIME(3) NULL,
  updated_at    DATETIME(3) NULL,
  UNIQUE KEY uk_labr_row (batch_id, line_no),
  KEY idx_labr_tenant (tenant_id),
  KEY idx_labr_batch (tenant_id, batch_id),
  CONSTRAINT chk_labr_status CHECK (parse_status IN ('ok','warning','error'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ── Piutang lama (aggregate root) ───────────────────────────────────────────
CREATE TABLE legacy_receivables (
  id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  tenant_id     BIGINT UNSIGNED NOT NULL,
  batch_id      BIGINT UNSIGNED NOT NULL,
  -- customer_name adalah identitas OTORITATIF baris ini (snapshot). Customer
  -- legacy tidak dipaksa masuk master `customers`: proyek lamanya memang tidak
  -- ada di sistem, dan memaksakan FK hanya demi keterisian akan mencemari
  -- master dengan orang yang mungkin tidak pernah bertransaksi lagi.
  customer_name VARCHAR(200) NOT NULL,
  -- customer_id: tautan OPSIONAL ke master, diisi belakangan bila ternyata
  -- orang yang sama membeli unit baru. Logical FK.
  customer_id   BIGINT UNSIGNED NULL,
  source_label  VARCHAR(200) NOT NULL,
  external_ref  VARCHAR(100) NULL,
  phone         VARCHAR(30)  NULL,
  email         VARCHAR(120) NULL,
  -- control_account_code di-SNAPSHOT per baris (pola tax_rules rule_id+revision
  -- dan charge_items.deposit_account_code): pelunasan tahun depan harus tetap
  -- mengkredit akun tempat piutang ini DIBENTUK, bukan konfigurasi terbaru.
  control_account_code VARCHAR(20) NOT NULL,
  as_of_date    DATE NOT NULL,
  -- due_date NULL → mesin aging memakai as_of_date. Piutang proyek lama yang
  -- tak bertanggal jatuh tempo memang sudah lewat tempo per tanggal cutoff.
  due_date      DATE NULL,
  -- original_amount IMMUTABLE. Koreksi = batalkan baris + impor ulang; begitu
  -- satu pelunasan menempel, mengubah nominalnya mengubah arti pelunasan itu.
  original_amount DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
  -- paid_amount adalah CACHE. SSOT-nya legacy_receivable_payments (INV-LAR-3):
  -- tidak pernah berubah tanpa baris pelunasan di transaksi yang sama.
  paid_amount   DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
  status        VARCHAR(12) NOT NULL DEFAULT 'open',
  notes         VARCHAR(500) NULL,
  created_by    BIGINT UNSIGNED NULL,
  created_at    DATETIME(3) NULL,
  updated_at    DATETIME(3) NULL,
  UNIQUE KEY uk_lr_extref (tenant_id, external_ref),
  KEY idx_lr_tenant (tenant_id),
  KEY idx_lr_status (tenant_id, status),
  KEY idx_lr_batch (tenant_id, batch_id),
  KEY idx_lr_customer (tenant_id, customer_id),
  CONSTRAINT chk_lr_amount CHECK (original_amount > 0),
  CONSTRAINT chk_lr_paid CHECK (paid_amount >= 0),
  CONSTRAINT chk_lr_status CHECK (status IN ('open','paid','written_off'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ── Pelunasan (append-only) ─────────────────────────────────────────────────
--
-- Satu baris = satu penerimaan kas atas SATU piutang lama. Pembayaran yang
-- mencakup beberapa piutang menghasilkan beberapa baris di sini, semuanya
-- menunjuk jurnal kas yang sama — alokasinya eksplisit, tidak pernah "kurangi
-- total customer secara buta".
CREATE TABLE legacy_receivable_payments (
  id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  tenant_id     BIGINT UNSIGNED NOT NULL,
  legacy_receivable_id BIGINT UNSIGNED NOT NULL,
  -- amount NEGATIF hanya untuk baris void (mirror). Pembaca outstanding
  -- menjumlahkan keduanya — histori utuh, Invariant #5. Pola charge_item_void.
  amount        DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
  payment_date  DATE NOT NULL,
  cash_account_code VARCHAR(20) NOT NULL,
  control_account_code VARCHAR(20) NOT NULL,
  journal_entry_id BIGINT UNSIGNED NOT NULL,
  document_id   BIGINT UNSIGNED NULL,
  -- document_number di-snapshot agar riwayat tetap terbaca tanpa join.
  document_number VARCHAR(40) NULL,
  voids_payment_id BIGINT UNSIGNED NULL,
  notes         VARCHAR(500) NULL,
  idempotency_key VARCHAR(64) NULL,
  created_by    BIGINT UNSIGNED NULL,
  created_at    DATETIME(3) NULL,
  updated_at    DATETIME(3) NULL,
  UNIQUE KEY uk_lrp_idem (tenant_id, idempotency_key),
  UNIQUE KEY uk_lrp_void (tenant_id, voids_payment_id),
  KEY idx_lrp_tenant (tenant_id),
  KEY idx_lrp_receivable (tenant_id, legacy_receivable_id),
  KEY idx_lrp_journal (tenant_id, journal_entry_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ── Jejak audit per piutang (append-only) ───────────────────────────────────
--
-- Mencatat KEADAAN, bukan klik: operasi yang ditolak tidak meninggalkan baris
-- (pelajaran W-6 — riwayat yang penuh percobaan gagal tidak bisa dibaca).
CREATE TABLE legacy_receivable_audits (
  id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  tenant_id     BIGINT UNSIGNED NOT NULL,
  legacy_receivable_id BIGINT UNSIGNED NOT NULL,
  event         VARCHAR(24) NOT NULL,
  amount        DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
  outstanding_after DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
  detail        VARCHAR(500) NULL,
  actor_id      BIGINT UNSIGNED NULL,
  created_at    DATETIME(3) NULL,
  updated_at    DATETIME(3) NULL,
  KEY idx_lra_tenant (tenant_id),
  KEY idx_lra_receivable (tenant_id, legacy_receivable_id, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
