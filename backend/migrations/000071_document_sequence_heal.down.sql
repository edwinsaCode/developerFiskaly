-- Sengaja tidak melakukan apa-apa.
--
-- Migrasi naiknya hanya MENAIKKAN seri ke nomor tertinggi yang sudah terbit.
-- Menurunkannya kembali berarti mengembalikan keadaan di mana engine menerbitkan
-- nomor yang sudah dipakai — persis kerusakan yang diperbaiki. Seri penomoran
-- memang forward-only.
SELECT 1;
