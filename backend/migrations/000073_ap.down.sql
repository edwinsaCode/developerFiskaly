-- W-11 — rollback Hutang Usaha (Accounts Payable).
--
-- PERINGATAN OPERASIONAL (N-6): JANGAN jalankan setelah ada transaksi AP yang
-- diposting. Migrasi ini membuang sub-ledger AP, sementara jurnal yang sudah
-- terposting TETAP ada — saldo 2-1000/2-1100/1-5300 lalu tidak bisa dijelaskan
-- dari tabel mana pun. Ledger tidak rusak dan tidak ada journal_lines yatim,
-- tetapi jejak rincinya hilang dan tidak bisa dipulihkan selain dari backup.
-- Aman HANYA sebelum tagihan AP pertama diposting.
--
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
