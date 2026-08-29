-- Hardening Receipt/Invoice: snapshot harga unit saat kontrak dibuat.
-- Diskon TIDAK disimpan (no duplicate SoT) — selalu derived:
--   diskon = unit_price_snapshot − dpp_amount (kontrak lama: snapshot NULL → diskon 0).
ALTER TABLE sale_contracts
  ADD COLUMN unit_price_snapshot DECIMAL(20,4) NULL AFTER dpp_amount;
