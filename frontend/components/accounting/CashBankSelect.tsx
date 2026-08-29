"use client";

import { useEffect, useState } from "react";
import { Select } from "@/components/ui/Input";
import { fetchCashBankAccounts } from "@/lib/api/ledger";
import type { Account } from "@/lib/types/api";

interface Props {
  token: string;
  value: string;
  onChange: (code: string) => void;
  label?: string;
  required?: boolean;
}

// CashBankSelect: dropdown rekening tujuan pembayaran yang COA-driven —
// memuat akun kas/bank aktif dari /ledger/accounts/cash-bank. Tidak ada lagi
// daftar bank yang di-hardcode. Memilih default ke akun pertama saat termuat.
export function CashBankSelect({ token, value, onChange, label = "Rekening / Kas Tujuan", required }: Props) {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    fetchCashBankAccounts(token)
      .then((accs) => {
        if (cancelled) return;
        setAccounts(accs);
        if (accs.length > 0 && !value) onChange(accs[0].code);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token]);

  const banks = accounts.filter((a) => a.account_category === "bank");
  const cash = accounts.filter((a) => a.account_category === "cash");

  // Placeholder memakai tinggi & label yang sama dengan Select-nya supaya form
  // tidak "melompat" saat daftar rekening selesai dimuat.
  if (loading || accounts.length === 0) {
    return (
      <div className="flex flex-col gap-1.5">
        <span className="text-sm font-medium text-text-primary">
          {label}
          {required && <span className="text-danger ml-0.5">*</span>}
        </span>
        <div className="w-full rounded-lg border border-border bg-border-subtle px-3.5 py-2.5 text-sm">
          {loading ? (
            <span className="text-text-tertiary">Memuat rekening…</span>
          ) : (
            <span className="text-danger">Belum ada akun kas/bank aktif — tambahkan di Daftar Akun.</span>
          )}
        </div>
      </div>
    );
  }

  return (
    <Select label={label} value={value} onChange={(e) => onChange(e.target.value)} required={required}>
      {banks.length > 0 && (
        <optgroup label="Bank">
          {banks.map((a) => (
            <option key={a.code} value={a.code}>{a.name}</option>
          ))}
        </optgroup>
      )}
      {cash.length > 0 && (
        <optgroup label="Kas">
          {cash.map((a) => (
            <option key={a.code} value={a.code}>{a.name}</option>
          ))}
        </optgroup>
      )}
    </Select>
  );
}
