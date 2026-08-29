-- R1 KPR Realization: kebijakan tenant untuk auto-invoice kekurangan pasca
-- pencairan bank. DEFAULT 0 (manual — CTA satu-klik) sesuai design review.
ALTER TABLE tenants
  ADD COLUMN auto_shortfall_invoice TINYINT(1) NOT NULL DEFAULT 0;
