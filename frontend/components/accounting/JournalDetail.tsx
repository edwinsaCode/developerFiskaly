"use client";

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { fetchJournal, postJournal, reverseJournal, deleteJournal } from "@/lib/api/ledger";
import { ApiError } from "@/lib/api/client";
import type { JournalEntry, JournalSource } from "@/lib/types/api";
import { Can } from "@/components/ui/Can";
import { JournalDocumentPanel, type DocumentSelection } from "./JournalDocumentPanel";
import { LinkifiedText } from "@/components/documents/LinkifiedText";
import { todayLocalStr } from "@/lib/date";

interface Props {
  token: string;
  id: number;
}

const SOURCE_LABELS: Record<JournalSource, string> = {
  manual: "Manual",
  system: "Sistem",
  reversal: "Pembalik",
  cost: "Biaya",
  sale: "Penjualan",
  tax: "Pajak",
  termin: "Termin",
  opening_balance: "Saldo Awal",
};

function formatRupiah(value: string): string {
  const num = parseFloat(value);
  if (isNaN(num)) return "Rp 0";
  return "Rp " + num.toLocaleString("id-ID", { minimumFractionDigits: 0, maximumFractionDigits: 0 });
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString("id-ID", { day: "2-digit", month: "long", year: "numeric" });
}

function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString("id-ID", { day: "2-digit", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" });
}

export function JournalDetail({ token, id }: Props) {
  const router = useRouter();
  const [journal, setJournal] = useState<JournalEntry | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [acting, setActing] = useState(false);
  const [reverseDate, setReverseDate] = useState(() => todayLocalStr());
  const [showReverseModal, setShowReverseModal] = useState(false);
  // W-3.6 (INV-DOC-1): jenis bukti yang akan terbit saat posting. `docReady`
  // false = panel belum tahu jawabannya, jadi posting ditahan — mengizinkannya
  // berarti memposting kas sebelum sistem tahu bukti apa yang diperlukan.
  const [doc, setDoc] = useState<DocumentSelection>({ ready: false });
  const [docChoices, setDocChoices] = useState<string[] | null>(null);

  // Stabil supaya panel bukti tidak memuat ulang tiap render.
  const handleDocChange = useCallback((sel: DocumentSelection) => setDoc(sel), []);

  useEffect(() => {
    fetchJournal(token, id)
      .then(setJournal)
      .catch(() => setError("Jurnal tidak ditemukan."))
      .finally(() => setLoading(false));
  }, [token, id]);

  async function handlePost() {
    if (!journal || acting || !doc.ready) return;
    setActing(true);
    setError(null);
    try {
      await postJournal(token, journal.id, doc.docType);
      // Muat ulang detail, bukan pakai respons posting: nomor bukti ikut di
      // endpoint detail, dan itulah yang harus terbaca setelah posting.
      setJournal(await fetchJournal(token, journal.id));
      setDocChoices(null);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Gagal memposting jurnal.");
      // Server menolak sambil menyebutkan jenis bukti yang sah — pakai
      // daftarnya, jangan biarkan pengguna menebak ulang.
      if (e instanceof ApiError && Array.isArray(e.payload?.choices)) {
        setDocChoices(e.payload.choices as string[]);
      }
    } finally {
      setActing(false);
    }
  }

  async function handleDelete() {
    if (!journal || acting) return;
    if (!confirm("Hapus draft jurnal ini?")) return;
    setActing(true);
    try {
      await deleteJournal(token, journal.id);
      router.push("/accounting/jurnal");
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Gagal menghapus jurnal.");
      setActing(false);
    }
  }

  async function handleReverse() {
    if (!journal || acting) return;
    setActing(true);
    setError(null);
    try {
      const rev = await reverseJournal(token, journal.id, reverseDate);
      setShowReverseModal(false);
      router.push(`/accounting/jurnal/${rev.id}`);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Gagal membuat jurnal pembalik.");
    } finally {
      setActing(false);
    }
  }

  if (loading) {
    return (
      <div className="space-y-4">
        {[...Array(4)].map((_, i) => <div key={i} className="h-16 bg-border-subtle rounded-xl animate-pulse" />)}
      </div>
    );
  }

  if (error && !journal) {
    return (
      <div className="text-center py-16">
        <p className="text-danger font-medium">{error}</p>
        <button onClick={() => router.back()} className="mt-4 text-sm text-accent hover:underline">Kembali</button>
      </div>
    );
  }

  if (!journal) return null;

  const isPosted = !!journal.posted_at;
  // S9/R-9: total dari backend (decimal) — FE nol aritmetika bisnis.
  const totalDebit = journal.total_debit ?? "0";
  const totalCredit = journal.total_credit ?? "0";

  return (
    // Dokumen jurnal: lebar dibatasi agar baris tetap terbaca, tapi DIPUSATKAN
    // — sebelumnya menempel kiri dan menyisakan ruang kosong di kanan.
    <div className="space-y-6 mx-auto w-full max-w-4xl">
      {/* Back */}
      <button onClick={() => router.push("/accounting/jurnal")} className="text-sm text-text-secondary hover:text-accent flex items-center gap-1">
        ← Kembali ke daftar jurnal
      </button>

      {/* Header card */}
      <div className="bg-surface rounded-xl border border-border p-5 space-y-3">
        <div className="flex items-start justify-between gap-4">
          <div>
            <div className="flex items-center gap-2 mb-1">
              <span className="text-text-secondary text-sm">#{journal.id}</span>
              {isPosted ? (
                <span className="inline-flex px-2 py-0.5 rounded-full text-xs font-medium bg-success-bg text-success">Diposting</span>
              ) : (
                <span className="inline-flex px-2 py-0.5 rounded-full text-xs font-medium bg-warning-bg text-warning">Draft</span>
              )}
              <span className="inline-flex px-2 py-0.5 rounded-full text-xs font-medium bg-border-subtle text-text-secondary">
                {SOURCE_LABELS[journal.source] ?? journal.source}
              </span>
              {journal.is_reversing && (
                <span className="inline-flex px-2 py-0.5 rounded-full text-xs font-medium bg-warning-bg text-warning">Pembalik</span>
              )}
            </div>
            <h2 className="text-lg font-semibold text-text-primary">
              <LinkifiedText token={token} text={journal.description} />
            </h2>
          </div>
          <div className="text-right text-sm text-text-secondary shrink-0">
            <div className="font-medium text-text-primary">{formatDate(journal.date)}</div>
            {journal.reference && <div className="text-xs mt-0.5">Ref: {journal.reference}</div>}
          </div>
        </div>
        {journal.reverses_id && (
          <div className="text-xs text-text-secondary">
            Membalik jurnal{" "}
            <button
              onClick={() => router.push(`/accounting/jurnal/${journal.reverses_id}`)}
              className="text-accent hover:underline"
            >
              #{journal.reverses_id}
            </button>
          </div>
        )}
        <div className="text-xs text-text-secondary flex gap-4">
          <span>Dibuat: {formatDateTime(journal.created_at)}</span>
          {isPosted && journal.posted_at && <span>Diposting: {formatDateTime(journal.posted_at)}</span>}
        </div>
      </div>

      {/* Bukti kas (W-3.6) — hanya muncul untuk jurnal yang menyentuh kas */}
      <JournalDocumentPanel
        token={token}
        journalId={journal.id}
        posted={isPosted}
        documentNumber={journal.document_number}
        documentTypeName={journal.document_type_name}
        documentIssuedAt={journal.document_issued_at}
        override={docChoices}
        onChange={handleDocChange}
      />

      {/* Lines table */}
      {/* Di layar sempit tabel 4 kolom digulir horizontal di dalam kartunya
          sendiri — halaman tidak ikut melebar. */}
      <div className="bg-surface rounded-xl border border-border overflow-x-auto scrollbar-thin">
        <table className="w-full text-sm min-w-[560px]">
          <thead>
            <tr className="bg-bg border-b border-border text-text-secondary">
              <th className="px-4 py-3 text-left font-medium">Akun</th>
              <th className="px-4 py-3 text-left font-medium">Keterangan</th>
              <th className="px-4 py-3 text-right font-medium w-40">Debit</th>
              <th className="px-4 py-3 text-right font-medium w-40">Kredit</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {journal.lines?.map(line => (
              <tr key={line.id} className="hover:bg-bg transition-colors">
                <td className="px-4 py-3 text-text-primary">
                  {line.account ? (
                    <span>
                      <span className="text-text-secondary text-xs mr-1">{line.account.code}</span>
                      {line.account.name}
                    </span>
                  ) : (
                    <span className="text-text-secondary">Akun #{line.account_id}</span>
                  )}
                </td>
                <td className="px-4 py-3 text-text-secondary">
                  {line.description ? <LinkifiedText token={token} text={line.description} /> : "—"}
                </td>
                <td className="px-4 py-3 text-right font-mono text-text-primary">
                  {parseFloat(line.debit) > 0 ? formatRupiah(line.debit) : "—"}
                </td>
                <td className="px-4 py-3 text-right font-mono text-text-primary">
                  {parseFloat(line.credit) > 0 ? formatRupiah(line.credit) : "—"}
                </td>
              </tr>
            ))}
          </tbody>
          <tfoot className="border-t-2 border-border bg-bg">
            <tr>
              <td colSpan={2} className="px-4 py-3 text-right text-sm font-semibold text-text-secondary">Total</td>
              <td className="px-4 py-3 text-right font-mono font-semibold text-text-primary">{formatRupiah(totalDebit)}</td>
              <td className="px-4 py-3 text-right font-mono font-semibold text-text-primary">{formatRupiah(totalCredit)}</td>
            </tr>
          </tfoot>
        </table>
      </div>

      {/* Error */}
      {error && (
        <div className="rounded-lg bg-danger-bg border border-danger/30 px-4 py-3 text-sm text-danger">{error}</div>
      )}

      {/* Actions */}
      <Can roles={["owner", "accountant"]}>
        <div className="flex items-center gap-3 flex-wrap">
          {!isPosted && (
            <>
              <button
                onClick={handlePost}
                disabled={acting || !doc.ready}
                className="px-5 py-2 rounded-lg bg-accent text-white text-sm font-medium hover:bg-accent/90 disabled:opacity-50 transition-colors"
              >
                {acting ? "Memproses..." : "Posting Jurnal"}
              </button>
              <button
                onClick={handleDelete}
                disabled={acting}
                className="px-5 py-2 rounded-lg border border-danger/30 text-danger text-sm font-medium hover:bg-danger-bg disabled:opacity-50 transition-colors"
              >
                Hapus Draft
              </button>
            </>
          )}
          {isPosted && !journal.is_reversing && (
            <button
              onClick={() => setShowReverseModal(true)}
              disabled={acting}
              className="px-5 py-2 rounded-lg border border-border text-text-primary text-sm font-medium hover:bg-bg disabled:opacity-50 transition-colors"
            >
              Buat Jurnal Pembalik
            </button>
          )}
        </div>
      </Can>

      {/* Reverse modal */}
      {showReverseModal && (
        <div className="fixed inset-0 bg-text-primary/30 backdrop-blur-sm flex items-center justify-center z-overlay" onClick={() => setShowReverseModal(false)}>
          <div className="bg-surface rounded-2xl p-6 w-80 shadow-xl space-y-4" onClick={e => e.stopPropagation()}>
            <h3 className="text-base font-semibold text-text-primary">Tanggal Jurnal Pembalik</h3>
            <p className="text-sm text-text-secondary">Jurnal pembalik akan langsung diposting dengan tanggal yang dipilih.</p>
            <input
              type="date"
              value={reverseDate}
              onChange={e => setReverseDate(e.target.value)}
              className="w-full border border-border rounded-lg px-3 py-2 text-sm bg-surface text-text-primary focus:outline-none focus:ring-2 focus:ring-accent"
            />
            <div className="flex gap-2">
              <button
                onClick={handleReverse}
                disabled={acting || !reverseDate}
                className="flex-1 px-4 py-2 rounded-lg bg-accent text-white text-sm font-medium hover:bg-accent/90 disabled:opacity-50 transition-colors"
              >
                {acting ? "Memproses..." : "Buat Pembalik"}
              </button>
              <button
                onClick={() => setShowReverseModal(false)}
                className="flex-1 px-4 py-2 rounded-lg border border-border text-text-primary text-sm hover:bg-bg transition-colors"
              >
                Batal
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
