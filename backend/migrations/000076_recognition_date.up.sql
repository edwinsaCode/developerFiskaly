-- Temuan #7: pengakuan pendapatan+HPP pindah dari titik fisik "BAST" ke titik
-- legal "Akad". `bast_date` sekarang bermakna tanggal Akad; `handed_over_at`
-- baru menandai serah terima fisik (bisa NULL — belum diserahkan, atau kapan
-- saja setelah Akad).
ALTER TABLE sale_records
  CHANGE COLUMN bast_date recognition_date DATETIME(3) NOT NULL,
  ADD COLUMN handed_over_at DATETIME(3) NULL AFTER recognition_date;
