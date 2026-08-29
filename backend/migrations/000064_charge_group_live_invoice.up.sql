-- 000064 — INV-CG-1, definisi "live" dipertajam.
--
-- 000063 memasang UNIQUE (tenant_id, charge_group_live) dengan live = REALISASI
-- dan status <> 'cancelled'. Itu lebih ketat daripada aturan aplikasi
-- (billing.GenerateChargeGroupInvoice), yang mengecualikan 'paid' juga.
--
-- Yang benar adalah versi aplikasi. Invoice berstatus paid adalah DOKUMEN YANG
-- SUDAH SELESAI, bukan tagihan hidup. Bila K-5 menaikkan nominal item setelah
-- invoice lama lunas, grup punya outstanding baru dan HARUS bisa ditagihkan
-- dengan dokumen baru — index 000063 akan menolaknya dengan 1062 padahal
-- aplikasi mengizinkan.
--
-- Invariant yang ditegakkan: SATU grup tagihan punya PALING BANYAK SATU invoice
-- realisasi yang hidup (belum lunas, belum batal). Sebuah invoice tetap tidak
-- pernah mencakup lebih dari satu grup — itu dijaga oleh charge_group_id.

ALTER TABLE invoices
    DROP INDEX uk_invoices_charge_group_live,
    DROP COLUMN charge_group_live;

ALTER TABLE invoices
    ADD COLUMN charge_group_live BIGINT UNSIGNED
        GENERATED ALWAYS AS (
            CASE WHEN invoice_type = 'REALISASI'
                  AND status NOT IN ('cancelled', 'paid')
                 THEN charge_group_id END
        ) STORED,
    ADD UNIQUE KEY uk_invoices_charge_group_live (tenant_id, charge_group_live);
