-- W-12 — Identitas user: nama lengkap.
--
-- Email tetap identifier login dan tetap unik; `name` murni identitas tampilan.
-- DEFAULT '' dipilih supaya 24 user yang sudah ada tidak rusak dan tidak ada
-- satu pun nama yang dikarang: nama kosong berarti "belum diisi", dan layar
-- menampilkan email sebagai fallback sampai pemiliknya mengisinya sendiri.
ALTER TABLE users
    ADD COLUMN name VARCHAR(200) NOT NULL DEFAULT '' AFTER email;

-- Role marketing (W-12) tidak menambah kolom — varchar(20) sudah cukup dan
-- tidak ada CHECK constraint. Yang perlu diperbarui hanya komentarnya, supaya
-- schema tidak lagi mengklaim daftar role yang sudah tidak lengkap.
ALTER TABLE users
    MODIFY COLUMN role VARCHAR(20) NOT NULL DEFAULT 'viewer'
    COMMENT 'owner|accountant|marketing|viewer';
