-- ╔═══════════════════════════════════════════════════════════════════════════╗
-- ║  W-11 · MIGRASI 000073 — SUDAH DISETUJUI & TERPASANG (dev, 2026-08-14)    ║
-- ║                                                                           ║
-- ║  Berkas ini adalah SALINAN REVIEW — arsip dari apa yang direview, bukan   ║
-- ║  yang dijalankan. Yang dijalankan golang-migrate:                         ║
-- ║      backend/migrations/000073_ap.up.sql                                  ║
-- ║      backend/migrations/000073_ap.down.sql                                ║
-- ║  Isinya identik (diverifikasi dengan diff saat pemisahan).                ║
-- ║                                                                           ║
-- ║  JANGAN sunting berkas ini untuk mengubah schema — ia tidak dibaca oleh   ║
-- ║  golang-migrate. Perubahan schema berikutnya = migrasi 000074 yang baru.  ║
-- ╚═══════════════════════════════════════════════════════════════════════════╝
--
-- W-11 — Hutang Usaha (Accounts Payable).
--
-- YANG DILAKUKAN MIGRASI INI
--   1. Menambah 2 akun COA ke SETIAP tenant yang sudah ada (idempoten).
--   2. Membuat 4 tabel baru.
--   3. Menambah 2 kolom NULLABLE pada cost_entries.
--
-- YANG TIDAK DILAKUKAN — dan ini bagian yang penting:
--   · Nol UPDATE/DELETE atas baris bisnis mana pun.
--   · Nol backfill. Seluruh baris cost_entries historis tetap NULL pada dua
--     kolom baru, dan artinya tidak berubah.
--   · Nol perubahan pada journal_entries, journal_lines, accounts existing.
--   · Nol jenis dokumen baru (BKK yang sudah ada dipakai apa adanya).
--
-- KENAPA AKUNNYA DIMASUKKAN LEWAT MIGRASI, BUKAN CUKUP SeedCOA
--   SeedCOA hanya berjalan saat tenant DIBUAT. Tenant yang sudah ada tidak akan
--   pernah mendapat 2-1100/1-5300 dari sana. Kalau akunnya tidak ada, jurnal
--   pengakuan hutang akan gagal di ResolveAccount — bukan diam-diam salah,
--   tetapi tetap membuat modulnya tak bisa dipakai oleh tenant lama.

-- ═══════════════════════════════════════════════════════════════════════════
-- BAGIAN UP
-- ═══════════════════════════════════════════════════════════════════════════

-- ── 1. AKUN COA ─────────────────────────────────────────────────────────────
--
-- Pola INSERT…SELECT ini menyalin `type`, `normal_balance`, dan `category` dari
-- akun SEJENIS yang sudah ada di tenant yang sama (pola migrasi 000056), bukan
-- menuliskannya sebagai literal. Alasannya: kalau suatu tenant pernah mengubah
-- klasifikasi akunnya, akun baru ikut konsisten dengan tenant itu — bukan
-- dengan asumsi yang ditulis di sini setahun yang lalu.
--
-- IDEMPOTEN lewat NOT EXISTS: dijalankan ulang tidak menghasilkan duplikat.
-- Tenant yang (karena alasan apa pun) tidak punya akun rujukan akan terlewat
-- tanpa error — dan modul AP akan menolak bekerja untuk tenant itu dengan pesan
-- "akun COA tidak ditemukan", yang benar: lebih baik gagal terang-terangan
-- daripada menjurnal ke akun yang ditebak.

-- 2-1100 Hutang Retensi Kontraktor (rujukan: 2-1000 Hutang Usaha)
--
-- SENGAJA akun tersendiri, bukan sub-saldo 2-1000: jatuh temponya terikat masa
-- pemeliharaan, bukan termin. Menyatukannya membuat "hutang jatuh tempo bulan
-- ini" memuat uang yang baru boleh keluar setahun lagi.
INSERT INTO accounts (tenant_id, code, name, type, normal_balance, is_system, description, is_active, category)
SELECT a.tenant_id,
       '2-1100',
       'Hutang Retensi Kontraktor',
       a.type,
       a.normal_balance,
       1,
       'Bagian termin kontraktor yang ditahan sampai masa pemeliharaan selesai (W-11 D-6). Kewajiban tersendiri: jatuh tempo & syarat pelepasannya berbeda dari 2-1000.',
       1,
       a.category
FROM accounts a
WHERE a.code = '2-1000'
  AND NOT EXISTS (
      SELECT 1 FROM accounts b WHERE b.tenant_id = a.tenant_id AND b.code = '2-1100'
  );

-- 1-5300 Uang Muka Vendor (rujukan: 1-5000 Biaya Dibayar di Muka)
--
-- Rujukannya 1-5000 dan bukan akun kas mana pun — DISENGAJA. `category` ikut
-- tersalin sebagai other_asset, sehingga 1-5300 TIDAK akan pernah muncul di
-- daftar akun kas/bank, tidak bisa dipilih sebagai akun pembayaran, dan tidak
-- ikut terhitung dalam KPI Kas. Kalau ia terklasifikasi cash/bank, uang muka
-- vendor akan terbaca sebagai kas yang masih dipegang perusahaan.
INSERT INTO accounts (tenant_id, code, name, type, normal_balance, is_system, description, is_active, category)
SELECT a.tenant_id,
       '1-5300',
       'Uang Muka Vendor',
       a.type,
       a.normal_balance,
       1,
       'Pembayaran ke vendor sebelum pekerjaannya diakui (W-11 D-8). BUKAN biaya dan BUKAN realisasi RAB sampai dikompensasi ke termin.',
       1,
       a.category
FROM accounts a
WHERE a.code = '1-5000'
  AND NOT EXISTS (
      SELECT 1 FROM accounts b WHERE b.tenant_id = a.tenant_id AND b.code = '1-5300'
  );

-- ── 2. vendors ──────────────────────────────────────────────────────────────
--
-- Master MINIMAL (D-7): identitas + status PKP. Bukan modul procurement.
-- is_pkp menentukan APAKAH sebuah tagihan boleh membawa PPN Masukan; nilainya
-- tetap diketik dari faktur, tidak dihitung dari tarif (tidak ada asumsi pajak
-- yang ditanam di schema).
CREATE TABLE vendors (
    id           BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id    BIGINT UNSIGNED NOT NULL,
    name         VARCHAR(200)    NOT NULL,
    npwp         VARCHAR(25)     NULL,
    is_pkp       TINYINT(1)      NOT NULL DEFAULT 0,
    address      VARCHAR(500)    NULL,
    phone        VARCHAR(50)     NULL,
    email        VARCHAR(200)    NULL,
    bank_name    VARCHAR(100)    NULL,
    bank_account VARCHAR(50)     NULL,
    -- Vendor DINONAKTIFKAN, tidak pernah dihapus: menghapusnya memutus tautan
    -- pada tagihan dan baris biaya yang sudah terbit.
    is_active    TINYINT(1)      NOT NULL DEFAULT 1,
    note         VARCHAR(500)    NULL,
    created_at   DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at   DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    -- Nama unik PER TENANT. Bukan global: dua tenant boleh punya vendor bernama
    -- sama, dan keduanya tidak boleh saling tahu.
    UNIQUE KEY uq_vendor_tenant_name (tenant_id, name),
    INDEX idx_vendor_tenant (tenant_id),
    INDEX idx_vendor_tenant_active (tenant_id, is_active)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='W-11 master vendor (minimal: identitas + status PKP)';

-- ── 3. ap_invoices ──────────────────────────────────────────────────────────
--
-- ATURAN NOMINAL — bagian yang paling mudah salah dibaca:
--   dpp_amount       nilai pekerjaan; INI yang menjadi biaya & realisasi RAB
--   ppn_amount       PPN Masukan → aset 1-5100, BUKAN biaya, tanpa tag proyek
--   retention_amount bagian DPP yang ditahan → pindah ke kewajiban 2-1100
--   advance_applied  kompensasi uang muka → mengurangi kas yang harus keluar
--   payable_amount   (dpp + ppn) − retensi − advance_applied
--
-- Σ cost_entries.amount milik tagihan ini == dpp_amount, SELALU (INV-AP-8).
-- Retensi, PPN, dan uang muka hanya memecah sisi KREDIT jurnal; tidak satu pun
-- boleh mengubah nilai biaya yang diakui.
--
-- TIDAK ADA kolom "sisa tagihan" dan tidak ada kolom "status pembayaran"
-- (D-12). Keduanya diturunkan dari ap_payment_allocations. Kolom cache adalah
-- cara termudah melahirkan dua angka yang tidak sepakat — satu di daftar, satu
-- di detail — dan yang salah tidak akan pernah memunculkan error.
CREATE TABLE ap_invoices (
    id                  BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id           BIGINT UNSIGNED NOT NULL,
    vendor_id           BIGINT UNSIGNED NOT NULL,
    -- NULL = tagihan overhead yang tidak bermuara ke proyek mana pun.
    project_id          BIGINT UNSIGNED NULL,
    -- Nomor dari VENDOR, bukan nomor terbitan kita. Sengaja TIDAK unique:
    -- dua vendor berbeda boleh memakai nomor yang sama.
    invoice_number      VARCHAR(100)    NOT NULL,
    invoice_date        DATE            NOT NULL,
    due_date            DATE            NOT NULL,

    dpp_amount          DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    ppn_amount          DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    retention_amount    DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    advance_applied     DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    payable_amount      DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',

    -- PPN tanpa faktur tidak bisa dikreditkan; kewajiban pengisiannya
    -- ditegakkan di service supaya pesan errornya bisa menjelaskan sebabnya.
    faktur_pajak_number VARCHAR(100)    NULL,
    retention_due_date  DATE            NULL,

    status              VARCHAR(12)     NOT NULL DEFAULT 'draft',
    -- Terisi sejak DRAFT: cost_entries.journal_entry_id NOT NULL, jadi jurnal
    -- (draft) harus lahir lebih dulu. Jurnal draft tidak dibaca satu pun
    -- laporan — semuanya posted-only.
    journal_entry_id    BIGINT UNSIGNED NOT NULL,
    -- Terisi saat dibalik, supaya status 'reversed' bisa dibuktikan tanpa
    -- menelusuri balik ke journal_entries.
    reversal_journal_entry_id BIGINT UNSIGNED NULL,

    description         VARCHAR(500)    NULL,
    created_by          BIGINT UNSIGNED NULL,
    posted_at           DATETIME(3)     NULL,
    created_at          DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at          DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    -- Nominal negatif tidak punya makna akuntansi di sini: koreksi dilakukan
    -- lewat jurnal pembalik, bukan lewat angka minus.
    CONSTRAINT chk_api_amounts_nonneg CHECK (
        dpp_amount >= 0 AND ppn_amount >= 0 AND retention_amount >= 0
        AND advance_applied >= 0 AND payable_amount >= 0
    ),
    -- Retensi adalah bagian DARI nilai pekerjaan. Menahan lebih dari nilainya
    -- membuat kewajiban ke vendor menjadi negatif.
    CONSTRAINT chk_api_retention_le_dpp CHECK (retention_amount <= dpp_amount),
    CONSTRAINT chk_api_status CHECK (status IN ('draft','posted','reversed')),

    INDEX idx_api_tenant (tenant_id),
    INDEX idx_api_tenant_vendor (tenant_id, vendor_id),
    INDEX idx_api_tenant_project (tenant_id, project_id),
    INDEX idx_api_tenant_status_due (tenant_id, status, due_date),
    INDEX idx_api_journal (journal_entry_id),
    CONSTRAINT fk_api_vendor  FOREIGN KEY (vendor_id)        REFERENCES vendors(id),
    CONSTRAINT fk_api_journal FOREIGN KEY (journal_entry_id) REFERENCES journal_entries(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='W-11 kewajiban kepada vendor; sisa tagihan & status bayar DITURUNKAN dari ap_payment_allocations';

-- ── 4. ap_payments ──────────────────────────────────────────────────────────
--
-- Satu baris = satu PERGERAKAN KAS = satu dokumen BKK bernomor (INV-DOC-1).
-- payment_kind memisahkan tiga jurnal yang tidak boleh saling menggantikan:
--   invoice   Dr 2-1000 / Cr Kas
--   advance   Dr 1-5300 / Cr Kas   ← membentuk ASET, bukan biaya
--   retention Dr 2-1100 / Cr Kas
-- Tidak satu pun menyentuh akun taksonomi biaya, sehingga pembayaran mustahil
-- terhitung ulang sebagai realisasi RAB (INV-AP-3/4).
CREATE TABLE ap_payments (
    id                BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id         BIGINT UNSIGNED NOT NULL,
    vendor_id         BIGINT UNSIGNED NOT NULL,
    payment_kind      VARCHAR(12)     NOT NULL,
    payment_date      DATE            NOT NULL,
    amount            DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    cash_account_code VARCHAR(20)     NOT NULL,

    journal_entry_id  BIGINT UNSIGNED NOT NULL,
    document_number   VARCHAR(50)     NULL,
    -- Pembalikan MENANDAI, tidak menghapus (Invariant #5 append-only).
    reversed_at       DATETIME(3)     NULL,

    -- Perlindungan terhadap klik dobel / retry jaringan. NULLABLE: pemanggil
    -- yang tidak mengirim kunci tetap dilayani, tetapi tanpa perlindungan itu.
    -- MySQL mengizinkan banyak NULL dalam UNIQUE index, jadi kolom nullable +
    -- unique key adalah bentuk yang benar di sini.
    idempotency_key   VARCHAR(100)    NULL,

    description       VARCHAR(500)    NULL,
    created_by        BIGINT UNSIGNED NULL,
    created_at        DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at        DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    CONSTRAINT chk_app_amount_pos CHECK (amount > 0),
    CONSTRAINT chk_app_kind CHECK (payment_kind IN ('invoice','advance','retention')),

    UNIQUE KEY uq_app_tenant_idem (tenant_id, idempotency_key),
    INDEX idx_app_tenant (tenant_id),
    INDEX idx_app_tenant_vendor (tenant_id, vendor_id),
    INDEX idx_app_tenant_kind (tenant_id, payment_kind),
    INDEX idx_app_journal (journal_entry_id),
    CONSTRAINT fk_app_vendor  FOREIGN KEY (vendor_id)        REFERENCES vendors(id),
    CONSTRAINT fk_app_journal FOREIGN KEY (journal_entry_id) REFERENCES journal_entries(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='W-11 pengeluaran kas ke vendor; satu baris = satu BKK';

-- ── 5. ap_payment_allocations ───────────────────────────────────────────────
--
-- SUB-LEDGER yang menentukan sisa tagihan. Inilah sumber kanonik — bukan kolom
-- pada ap_invoices.
--
-- payment_id NULLABLE karena kompensasi uang muka ke sebuah tagihan BUKAN
-- pengeluaran kas baru: uangnya sudah keluar saat uang muka dibayar. Barisnya
-- menunjuk ke pembayaran uang muka terdahulu lewat source_payment_id.
--
-- KONKURENSI (R-12): saldo uang muka yang tersedia = nilai pembayaran uang muka
-- − Σ alokasi bertipe 'advance' yang belum dibalik atas source_payment_id itu.
-- Karena saldonya DIHITUNG dari tabel ini (tidak disimpan), dua kompensasi yang
-- berjalan bersamaan bisa sama-sama membaca saldo lama. Penjagaannya di
-- service: baris ap_payments sumber dikunci SELECT … FOR UPDATE sebelum saldo
-- dihitung, sehingga transaksi kedua menunggu dan melihat hasil yang pertama.
-- Indeks idx_apa_source ada supaya penjumlahan itu tidak memindai tabel.
CREATE TABLE ap_payment_allocations (
    id                BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id         BIGINT UNSIGNED NOT NULL,
    payment_id        BIGINT UNSIGNED NULL,
    invoice_id        BIGINT UNSIGNED NOT NULL,
    allocation_type   VARCHAR(12)     NOT NULL,
    amount            DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    source_payment_id BIGINT UNSIGNED NULL,
    reversed_at       DATETIME(3)     NULL,
    created_at        DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at        DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    CONSTRAINT chk_apa_amount_pos CHECK (amount > 0),
    CONSTRAINT chk_apa_type CHECK (allocation_type IN ('invoice','retention','advance')),
    -- Alokasi harus punya asal-usul yang SESUAI JENISNYA. Bentuk longgar
    -- "payment_id IS NOT NULL OR source_payment_id IS NOT NULL" tidak cukup:
    -- ia meloloskan baris bertipe 'advance' yang tidak menunjuk uang muka mana
    -- pun. Baris seperti itu mengurangi tagihan, tetapi TIDAK ikut terhitung
    -- saat saldo uang muka dijumlahkan (penjumlahannya per source_payment_id) —
    -- sehingga saldo yang sama bisa dipakai lagi. Itu persis kelas kegagalan
    -- yang R-12 tutup, dan menutupnya di DB berarti ia tertutup walau kode
    -- pemanggilnya kelak berubah.
    --
    --   advance      → WAJIB menunjuk pembayaran uang muka yang dikonsumsi.
    --   invoice/     → WAJIB punya pembayaran kas sendiri; keduanya memang
    --   retention      lahir dari uang yang baru keluar.
    CONSTRAINT chk_apa_origin CHECK (
        (allocation_type =  'advance' AND source_payment_id IS NOT NULL) OR
        (allocation_type <> 'advance' AND payment_id        IS NOT NULL)
    ),

    INDEX idx_apa_tenant (tenant_id),
    INDEX idx_apa_tenant_invoice (tenant_id, invoice_id),
    INDEX idx_apa_payment (payment_id),
    INDEX idx_apa_source (tenant_id, source_payment_id),
    CONSTRAINT fk_apa_invoice FOREIGN KEY (invoice_id)        REFERENCES ap_invoices(id),
    CONSTRAINT fk_apa_payment FOREIGN KEY (payment_id)        REFERENCES ap_payments(id),
    CONSTRAINT fk_apa_source  FOREIGN KEY (source_payment_id) REFERENCES ap_payments(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='W-11 sub-ledger alokasi pembayaran → tagihan; sumber kanonik sisa tagihan & saldo uang muka';

-- ── 6. cost_entries: 2 kolom NULLABLE ───────────────────────────────────────
--
-- NULL = baris lahir dari jalur biaya langsung (kas/bank). Itu berlaku untuk
-- SELURUH baris historis: tidak ada backfill, dan makna kolom lama tidak
-- berubah sedikit pun.
--
-- Kolom teks `vendor` yang sudah ada TIDAK dihapus dan TIDAK di-backfill dari
-- master. Ia adalah nama yang tertulis di dokumen saat itu; vendor_id adalah
-- tautan ke master. Kalau master kelak berganti nama, bukti historis tidak
-- ikut berubah.
--
-- ON DELETE SET NULL pada kedua FK adalah jaring pengaman, bukan izin: service
-- tidak pernah menghapus vendor (dinonaktifkan) maupun tagihan (dibalik).
ALTER TABLE cost_entries
    ADD COLUMN vendor_id     BIGINT UNSIGNED NULL,
    ADD COLUMN ap_invoice_id BIGINT UNSIGNED NULL,
    ADD INDEX idx_ce_vendor (tenant_id, vendor_id),
    ADD INDEX idx_ce_ap_invoice (tenant_id, ap_invoice_id),
    ADD CONSTRAINT fk_ce_vendor     FOREIGN KEY (vendor_id)     REFERENCES vendors(id)     ON DELETE SET NULL,
    ADD CONSTRAINT fk_ce_ap_invoice FOREIGN KEY (ap_invoice_id) REFERENCES ap_invoices(id) ON DELETE SET NULL;


-- ═══════════════════════════════════════════════════════════════════════════
-- BAGIAN DOWN  →  000073_ap.down.sql
-- ═══════════════════════════════════════════════════════════════════════════
--
-- Urutan dibalik dari UP dan mengikuti arah foreign key: alokasi → pembayaran
-- → tagihan → vendor. Membalik urutannya akan gagal karena FK.

ALTER TABLE cost_entries
    DROP FOREIGN KEY fk_ce_ap_invoice,
    DROP FOREIGN KEY fk_ce_vendor;

ALTER TABLE cost_entries
    DROP INDEX idx_ce_ap_invoice,
    DROP INDEX idx_ce_vendor,
    DROP COLUMN ap_invoice_id,
    DROP COLUMN vendor_id;

DROP TABLE IF EXISTS ap_payment_allocations;
DROP TABLE IF EXISTS ap_payments;
DROP TABLE IF EXISTS ap_invoices;
DROP TABLE IF EXISTS vendors;

-- Akun COA dihapus HANYA bila belum pernah dipakai menjurnal.
--
-- Ini disengaja dan penting: down-migration yang menghapus akun yang punya
-- baris jurnal akan meninggalkan journal_lines menunjuk akun yang tidak ada —
-- neraca menjadi tidak dapat disusun, dan tidak ada cara memulihkannya selain
-- dari backup. Kalau akunnya sudah dipakai, biarkan ia tinggal: akun tak
-- terpakai tidak merusak apa pun, sedangkan jurnal yatim merusak segalanya.
DELETE a FROM accounts a
WHERE a.code IN ('2-1100','1-5300')
  AND NOT EXISTS (SELECT 1 FROM journal_lines jl WHERE jl.account_id = a.id);
