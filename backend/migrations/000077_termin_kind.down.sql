-- Turun W-13 kind. Aman kapan pun: kolom murni deskriptif, tidak pernah
-- ditaut dari jurnal/akun — tidak ada pembalikan yang kehilangan alamat.
ALTER TABLE termin_payments
    DROP COLUMN installment_no,
    DROP COLUMN kind;
