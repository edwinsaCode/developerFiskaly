"use client";

// W-3.6 — Panel bukti kas pada detail jurnal (INV-DOC-1).
//
// Sejak W-3.5 backend MENOLAK memposting jurnal yang menggerakkan kas tanpa
// bukti bernomor. Tanpa panel ini penolakan itu sampai ke akuntan sebagai error
// 400 di tengah alur: tombol ditekan, gagal, dan tidak ada petunjuk apa yang
// diminta. Aturan yang benar dengan penyampaian yang buruk tetap terasa bug.
//
// Panel hanya muncul untuk jurnal yang MENYENTUH kas. Menempelkan "jurnal ini
// tidak butuh bukti" pada setiap jurnal akrual cuma menambah kebisingan di
// layar yang paling sering dibuka.

import { useCallback, useEffect, useState } from "react";
import { fetchJournalDocumentChoices } from "@/lib/api/ledger";
import { fetchDocumentTypes } from "@/lib/api/document";

// Penjelasan satu baris per jenis: KEPEMILIKAN EKONOMIS dana — itulah yang
// membedakan BKK/BTP/RFC, dan itulah yang perlu diketahui operator saat memilih
// (D-W3-6). Nama resminya tetap datang dari master di server; yang lokal di
// sini hanya kalimat penjelasnya.
const OWNERSHIP: Record<string, string> = {
  cash_out: "Uang perusahaan yang keluar — beban operasional, pembelian, gaji.",
  third_party_payout:
    "Meneruskan titipan customer ke pihak ketiga — notaris, PDAM, listrik. Bukan uang perusahaan.",
  customer_refund: "Mengembalikan uang customer — pembatalan, kelebihan bayar.",
  cash_in: "Penerimaan perusahaan yang bukan pembayaran customer — setoran modal, pinjaman, bunga bank.",
  house_payment: "Pembayaran rumah dari customer (termin/cicilan).",
  booking: "Booking fee dari customer.",
  realization: "Titipan biaya realisasi dari customer.",
  kpr_disbursement: "Pencairan KPR — dana datang dari bank, bukan dari customer.",
  internal_transfer: "Pindah antar kas/bank — posisi kas perusahaan tidak berubah.",
  journal_reversal: "Bukti pembatalan pergerakan kas sebelumnya.",
};

// Tanggal terbit datang sebagai YYYY-MM-DD dari server. Di layar ia dibaca
// manusia, jadi ditulis seperti tanggal — bukan seperti kolom database.
function issuedLabel(iso: string): string {
  const d = new Date(`${iso}T00:00:00`);
  if (isNaN(d.getTime())) return iso;
  return d.toLocaleDateString("id-ID", { day: "numeric", month: "short", year: "numeric" });
}

export interface DocumentSelection {
  /** Panel selesai memutuskan. Selama false, posting HARUS ditahan. */
  ready: boolean;
  /** Jenis bukti terpilih; undefined = jurnal tidak menyentuh kas. */
  docType?: string;
}

interface Props {
  token: string;
  journalId: number;
  posted: boolean;
  /** Nomor bukti yang sudah terbit (jurnal terposting). */
  documentNumber?: string;
  documentTypeName?: string;
  documentIssuedAt?: string;
  /**
   * Daftar pilihan dari server saat posting ditolak 400 — menggantikan hasil
   * pemuatan awal. Server yang paling tahu apa yang sah, bukan layar.
   */
  override?: string[] | null;
  onChange: (sel: DocumentSelection) => void;
}

export function JournalDocumentPanel({
  token,
  journalId,
  posted,
  documentNumber,
  documentTypeName,
  documentIssuedAt,
  override,
  onChange,
}: Props) {
  const [loading, setLoading] = useState(!posted);
  const [touchesCash, setTouchesCash] = useState(false);
  const [required, setRequired] = useState(false);
  const [choices, setChoices] = useState<string[]>([]);
  const [selected, setSelected] = useState<string>("");
  const [names, setNames] = useState<Record<string, string>>({});

  const label = useCallback(
    (code: string) => names[code] ?? code,
    [names],
  );

  // Nama resmi jenis dokumen datang dari master (bisa diubah admin di
  // /pengaturan), jadi tidak boleh dihardcode di layar ini.
  useEffect(() => {
    let alive = true;
    fetchDocumentTypes(token)
      .then((types) => {
        if (!alive) return;
        setNames(Object.fromEntries(types.map((t) => [t.code, t.name])));
      })
      .catch(() => {
        /* nama jenis hanya kosmetik — kode tetap terbaca */
      });
    return () => {
      alive = false;
    };
  }, [token]);

  useEffect(() => {
    if (posted) {
      onChange({ ready: true });
      setLoading(false);
      return;
    }
    let alive = true;
    setLoading(true);
    fetchJournalDocumentChoices(token, journalId)
      .then((c) => {
        if (!alive) return;
        setTouchesCash(c.touches_cash);
        setRequired(c.required);
        setChoices(c.choices ?? []);
        setSelected(c.default ?? "");
        onChange({ ready: true, docType: c.touches_cash ? c.default : undefined });
      })
      .catch(() => {
        // Gagal tahu = tidak boleh menebak. Posting ditahan; pesan server yang
        // sesungguhnya akan muncul kalau pengguna tetap mencoba lewat retry.
        if (alive) onChange({ ready: false });
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [token, journalId, posted, onChange]);

  // Pilihan dari respons 400 menimpa apa pun yang dimuat di awal.
  useEffect(() => {
    if (!override || override.length === 0) return;
    setTouchesCash(true);
    setRequired(override.length > 1);
    setChoices(override);
    setSelected((prev) => (override.includes(prev) ? prev : override[0]));
    onChange({ ready: true, docType: override.includes(selected) ? selected : override[0] });
    // `selected` sengaja tidak jadi dependency: efek ini hanya bereaksi pada
    // datangnya daftar baru dari server, bukan pada tiap klik radio.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [override, onChange]);

  function pick(code: string) {
    setSelected(code);
    onChange({ ready: true, docType: code });
  }

  // ── Sudah diposting: tampilkan buktinya ──────────────────────────────────
  if (posted) {
    if (!documentNumber) return null;
    return (
      <div className="bg-surface rounded-xl border border-border px-5 py-4">
        <div className="flex items-baseline gap-3 flex-wrap">
          <span className="text-xs uppercase tracking-wide text-text-secondary">Bukti</span>
          <span className="font-mono text-base font-semibold text-text-primary">{documentNumber}</span>
          <span className="text-sm text-text-secondary">
            {documentTypeName}
            {documentIssuedAt ? ` · terbit ${issuedLabel(documentIssuedAt)}` : ""}
          </span>
        </div>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="bg-surface rounded-xl border border-border px-5 py-4">
        <div className="h-4 w-56 rounded bg-border-subtle animate-pulse" />
      </div>
    );
  }

  if (!touchesCash) return null;

  // ── Draft yang menyentuh kas ─────────────────────────────────────────────
  return (
    <div className="bg-surface rounded-xl border border-accent/30 px-5 py-4 space-y-3">
      <div>
        <h3 className="text-sm font-semibold text-text-primary">Jurnal ini menggerakkan kas</h3>
        <p className="text-sm text-text-secondary mt-0.5">
          Setiap pergerakan kas wajib punya bukti bernomor. Nomornya terbit saat posting —
          bukan sekarang, supaya tidak ada nomor terpakai untuk transaksi yang tidak jadi.
        </p>
      </div>

      {!required ? (
        <div className="rounded-lg bg-bg px-3 py-2.5">
          <div className="text-sm font-medium text-text-primary">{label(selected)}</div>
          {OWNERSHIP[selected] && (
            <div className="text-xs text-text-secondary mt-0.5">{OWNERSHIP[selected]}</div>
          )}
        </div>
      ) : (
        <fieldset className="space-y-2">
          <legend className="text-xs uppercase tracking-wide text-text-secondary mb-1">
            Jenis bukti
          </legend>
          {choices.map((code) => (
            <label
              key={code}
              className={`flex gap-3 rounded-lg border px-3 py-2.5 cursor-pointer transition-colors
                ${selected === code ? "border-accent bg-accent/5" : "border-border hover:border-accent/40"}`}
            >
              <input
                type="radio"
                name={`doc-type-${journalId}`}
                value={code}
                checked={selected === code}
                onChange={() => pick(code)}
                className="mt-1 accent-accent"
              />
              <span>
                <span className="block text-sm font-medium text-text-primary">{label(code)}</span>
                {OWNERSHIP[code] && (
                  <span className="block text-xs text-text-secondary mt-0.5">{OWNERSHIP[code]}</span>
                )}
              </span>
            </label>
          ))}
        </fieldset>
      )}
    </div>
  );
}
