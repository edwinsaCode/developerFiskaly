-- UAT rule klien #4: akun 1-2200 diganti nama dari "Piutang Bank (KPR)" menjadi
-- "Dana Jaminan Bank (KPR)". Semantik akun (Asset/Debit-normal, RoleReceivable,
-- piutang kepada bank dalam jendela akad→pencairan, T-3) SUDAH BENAR — hanya
-- label tampilan yang berubah, code/type/normal_balance TIDAK disentuh.
UPDATE accounts
SET name = 'Dana Jaminan Bank (KPR)'
WHERE code = '1-2200' AND name = 'Piutang Bank (KPR)';
