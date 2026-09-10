-- UAT 2026-09-07 (URGENT): RAB Konstruksi/Hard Cost tidak membedakan Produksi
-- Subsidi vs Produksi Komersial — keduanya jatuh ke satu pool alokasi yang
-- diratakan ke SEMUA unit HPP-eligible, padahal nilai biaya produksinya
-- berbeda per klasifikasi. Business rule final klien: Produksi Subsidi dan
-- Produksi Komersial WAJIB terpisah; Sarana & Prasarana dan Perizinan tetap
-- sesuai rule allocation existing (semua unit HPP-eligible).
--
-- budget_items.subcategory sudah free-text (VARCHAR(100)) sejak awal — TIDAK
-- perlu migrasi, cukup validasi Go-level baru di budget.Service.AddItem
-- (domain.ConstructionSubcategory) saat category='construction'.
--
-- cost_entries BELUM punya kolom untuk membawa klasifikasi ini ke transaksi
-- aktual — kolom baru NULLABLE, TANPA CHECK constraint (pola 000099: baris
-- historis shared+hard yang diposting sebelum fitur ini tidak boleh menjadi
-- tidak-sah gara-gara migrasi skema; penegakan aturan baru ada di Go-level
-- cost.Service.validate, bukan di constraint schema).
ALTER TABLE cost_entries
    ADD COLUMN hard_subcategory VARCHAR(30) NULL
    AFTER category;
