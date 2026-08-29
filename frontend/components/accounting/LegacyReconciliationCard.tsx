"use client";

import { useState } from "react";
import { Rupiah } from "@/components/format/Rupiah";
import type { LegacyReconciliation } from "@/lib/api/legacyar";

// Kartu Kecocokan dengan Buku Besar — kontrol akuntansi W-7, bukan hiasan.
//
// Yang dibandingkan BUKAN saldo hidup akun kontrol, melainkan PORSI SALDO
// AWAL-nya. Bedanya menentukan apakah kontrol ini dipakai orang: kalau
// pembandingnya saldo hidup, angkanya meleset setiap kali satu customer
// membayar, dan kontrol yang merah setiap hari berhenti dibaca dalam sepekan.
//
// Tiga keadaan, tiga tampilan — termasuk keadaan "saldo tidak terbaca", yang
// sengaja TIDAK menampilkan angka selisih sama sekali. Menampilkan "selisih
// Rp 1 miliar" ketika akun kontrolnya belum ada membuat admin mengejar selisih
// yang tidak pernah ada.

interface Props {
  rec: LegacyReconciliation;
  /** Judul kartu; wizard memakai kalimat lain daripada halaman daftar. */
  title?: string;
  /** Buka rincian sejak awal (dipakai di langkah wizard). */
  defaultOpen?: boolean;
}

function isZero(v: string): boolean {
  const n = parseFloat(v);
  return !isNaN(n) && n === 0;
}

export function LegacyReconciliationCard({
  rec,
  title = "Kecocokan dengan Buku Besar",
  defaultOpen = false,
}: Props) {
  const [open, setOpen] = useState(defaultOpen);

  const unavailable = !rec.ledger_available;
  const matched = rec.matched;
  const tone = unavailable
    ? "border-border bg-surface"
    : matched
      ? "border-success/30 bg-success-bg/40"
      : "border-warning/40 bg-warning-bg/40";

  // Selisih negatif = rinciannya LEBIH BESAR daripada yang tercatat di buku
  // besar. Dua arah ini punya tindak lanjut yang berbeda, jadi kalimatnya pun
  // berbeda — "selisih Rp 200jt" saja tidak memberi tahu ke arah mana.
  const diffNum = parseFloat(rec.difference);
  const ledgerShort = diffNum < 0;

  return (
    <div className={`rounded-lg border ${tone} px-5 py-4`}>
      <div className="flex items-baseline justify-between gap-3">
        <h3 className="text-sm font-semibold text-text-primary">{title}</h3>
        <span className="text-xs text-text-tertiary">per {rec.as_of_date}</span>
      </div>

      {unavailable ? (
        <p className="mt-2 text-sm text-text-secondary">
          Saldo buku besar untuk akun{" "}
          <span className="font-mono">{rec.control_account_code}</span> belum bisa dibaca
          {rec.note ? ` — ${rec.note}` : "."} Selisihnya karena itu tidak bisa ditafsirkan.
        </p>
      ) : (
        <>
          <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <p className="text-xs uppercase tracking-wide text-text-secondary">
                Saldo awal {rec.control_account_code} di buku besar
              </p>
              <p className="mt-0.5 text-lg font-bold tabular-nums text-text-primary">
                <Rupiah value={rec.opening_portion} colorSign={false} />
              </p>
            </div>
            <div>
              <p className="text-xs uppercase tracking-wide text-text-secondary">
                Rincian piutang proyek lama
              </p>
              <p className="mt-0.5 text-lg font-bold tabular-nums text-text-primary">
                <Rupiah value={rec.subledger_total} colorSign={false} />
              </p>
            </div>
          </div>

          <div className="mt-3 border-t border-border pt-3">
            {matched ? (
              <p className="text-sm font-medium text-success">✓ Cocok — selisih Rp 0</p>
            ) : (
              <>
                <p className="text-sm font-medium text-warning">
                  Selisih <Rupiah value={rec.difference} colorSign={false} />
                </p>
                <p className="mt-1 text-sm text-text-secondary">
                  {ledgerShort
                    ? "Rinciannya lebih besar daripada saldo awal yang tercatat di buku besar. Sistem tidak akan membuat jurnal untuk menutupnya sendiri."
                    : "Buku besar memuat saldo awal piutang yang belum ada rinciannya. Bisa jadi memang belum semuanya diimpor."}
                </p>
              </>
            )}

            {/* Turunnya saldo akun kontrol setelah customer membayar adalah
                hal yang benar, bukan selisih. Dikatakan terang-terangan supaya
                tidak ada yang mengejarnya sebagai masalah. */}
            {!isZero(rec.legacy_payment_effect) && (
              <p className="mt-1 text-xs text-text-tertiary">
                Saldo {rec.control_account_code} saat ini{" "}
                <Rupiah value={rec.ledger_balance} colorSign={false} /> — sudah dikurangi{" "}
                <Rupiah value={rec.legacy_payment_effect} colorSign={false} /> oleh pelunasan
                piutang lama. Itu bukan selisih.
              </p>
            )}
          </div>

          <button
            type="button"
            onClick={() => setOpen((v) => !v)}
            className="mt-3 text-xs text-accent hover:underline"
            aria-expanded={open}
          >
            {open ? "Sembunyikan rincian" : "Rincian perhitungan"} {open ? "▴" : "▾"}
          </button>

          {open && (
            <dl className="mt-2 space-y-1 border-t border-border pt-2 text-xs">
              <Line
                label={`Saldo ${rec.control_account_code} per ${rec.as_of_date}`}
                value={rec.ledger_balance}
                strong
              />
              <Line label="dari Saldo Awal (dibandingkan)" value={rec.opening_portion} indent />
              <Line label="dari operasi berjalan" value={rec.operational_movement} indent />
              <Line label="pelunasan piutang lama" value={rec.legacy_payment_effect} indent />
              <div className="border-t border-border-subtle pt-1" />
              <Line label="Piutang lama sudah diimpor (nilai asli)" value={rec.legacy_existing} />
              {!isZero(rec.incoming) && <Line label="Yang akan diimpor" value={rec.incoming} />}
              <Line label="Total rincian" value={rec.subledger_total} strong />
              <Line label="Selisih" value={rec.difference} strong />
            </dl>
          )}
        </>
      )}
    </div>
  );
}

function Line({
  label,
  value,
  indent,
  strong,
}: {
  label: string;
  value: string;
  indent?: boolean;
  strong?: boolean;
}) {
  return (
    <div className="flex items-baseline justify-between gap-4">
      <dt className={`${indent ? "pl-4 text-text-tertiary" : "text-text-secondary"}`}>{label}</dt>
      <dd
        className={`tabular-nums ${strong ? "font-semibold text-text-primary" : "text-text-secondary"}`}
      >
        <Rupiah value={value} colorSign={false} />
      </dd>
    </div>
  );
}
