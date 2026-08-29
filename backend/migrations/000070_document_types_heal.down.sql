-- Sengaja tidak melakukan apa-apa.
--
-- Migrasi naiknya hanya MENAMBAL baris master yang hilang; baris hasil tambalan
-- tidak bisa dibedakan dari baris yang sudah ada sejak 000065/000066. Menghapus
-- "kesebelas jenis untuk semua tenant" di sini berarti ikut membuang master milik
-- tenant yang tidak pernah bolong — dan begitu jenisnya hilang, seluruh penerbitan
-- kwitansi, invoice, dan bukti kas tenant itu berhenti (resolver fail-closed).
--
-- Turun tanpa efek lebih jujur daripada turun yang merusak.
SELECT 1;
