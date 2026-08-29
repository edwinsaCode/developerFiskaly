-- Rollback Increment 6 — Unit Lifecycle.
-- Aman: menghapus guard + tabel audit. Nilai status baru (booked/ppjb/dst.) yang
-- terlanjur tersimpan di units.status TIDAK dipaksa balik (append-only spirit);
-- namun bila down dijalankan, CHECK dilepas sehingga nilai apa pun kembali sah.

ALTER TABLE units DROP CHECK chk_units_status;

DROP TABLE IF EXISTS unit_status_transitions;
