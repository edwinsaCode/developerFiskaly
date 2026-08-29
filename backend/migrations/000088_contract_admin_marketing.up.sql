-- P1 (kelebihan-tanah execution mandate, priority 2026-08-26) — Sales ≠ Admin
-- Marketing. Sales (Booking.sales_person_id / Contract.sales_person_id) adalah
-- tenaga penjual yang berhak komisi (internal/commission mengunci ke
-- sales_person_id, TIDAK PERNAH ke kolom ini). Admin Marketing menangani
-- administrasi kontrak/dokumen/KPR/follow-up pasca-Booking — peran independen,
-- bisa orang yang sama atau berbeda dari Sales pada kontrak yang sama.
--
-- Reuse sales_persons sebagai master personel (bukan tabel person baru) — pola
-- sama dengan sales_person_id di tabel lain di codebase ini: logical ref, TANPA
-- FK constraint (divalidasi di service layer via SalesPersonExists), tenant
-- isolation ditegakkan lewat GORM global scope seperti biasa.
ALTER TABLE sale_contracts
    ADD COLUMN admin_marketing_person_id BIGINT UNSIGNED NULL
        COMMENT 'logical ref sales_persons.id — administrasi kontrak, TIDAK berhak komisi',
    ADD INDEX idx_sc_admin_marketing (tenant_id, admin_marketing_person_id);
