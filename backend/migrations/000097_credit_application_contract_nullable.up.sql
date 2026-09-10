-- Buyer Credit lifecycle (lanjutan 000096): konsumsi otomatis (netting Uang
-- Muka saat Akad, atau disposisi terminal saat pembatalan unit) bisa terjadi
-- pada unit yang TIDAK PERNAH punya sale_contract formal (mis. termin/booking
-- pra-kontrak yang dibatalkan sebelum Konversi Kontrak — lihat
-- TestIntegration_Cancellation_PreBAST). credit_applications.sale_contract_id
-- jadi nullable agar kejadian ini tidak gagal menulis audit trail konsumsi
-- (unit_id + tenant_id + reason tetap cukup untuk audit tanpa kontrak).
ALTER TABLE credit_applications
    MODIFY COLUMN sale_contract_id BIGINT UNSIGNED NULL;
