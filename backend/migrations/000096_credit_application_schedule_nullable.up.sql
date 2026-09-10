-- Buyer Credit lifecycle: konsumsi kredit tidak selalu terikat satu cicilan
-- tertentu (mis. dikonsumsi otomatis saat Akad melalui netting Uang Muka
-- Penjualan, atau saat unit dibatalkan/disposisi terminal). Kolom ini jadi
-- nullable agar credit_applications tetap SATU sumber kebenaran untuk SEMUA
-- konsumsi saldo kredit buyer, bukan hanya pemakaian eksplisit ke cicilan.
ALTER TABLE credit_applications
    MODIFY COLUMN payment_schedule_id BIGINT UNSIGNED NULL;
