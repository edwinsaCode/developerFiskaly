-- Membalik W-12 identitas user. Aman dijalankan: `name` hanya identitas
-- tampilan, tidak ada baris lain yang mereferensinya.
--
-- CATATAN: user dengan role 'marketing' akan tetap ada dengan role itu setelah
-- down migration ini — komentar kolom kembali menyebut tiga role, tetapi datanya
-- tidak diubah. Menurunkan role mereka secara diam-diam akan mengubah hak akses
-- orang tanpa jejak; itu keputusan pemilik, bukan keputusan migrasi.
ALTER TABLE users
    MODIFY COLUMN role VARCHAR(20) NOT NULL DEFAULT 'viewer'
    COMMENT 'owner|accountant|viewer';

ALTER TABLE users DROP COLUMN name;
