-- Sengaja tidak melakukan apa-apa.
--
-- Migrasi naiknya hanya membalik counts_toward_price menjadi FALSE pada
-- termin Kelebihan Tanah pra-Akad; baris hasil perbaikan tidak bisa dibedakan
-- dari termin yang sejak awal dicatat benar oleh kode baru. Mengembalikannya
-- ke TRUE berarti menghidupkan lagi bug "pembayaran tanah mengurangi harga
-- unit" — dan ikut merusak termin yang memang benar.
--
-- Turun tanpa efek lebih jujur daripada turun yang merusak.
SELECT 1;
