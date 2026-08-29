"use client";

// W-3.6 — Buku Dokumen: register seluruh nomor bukti yang pernah terbit,
// beserta jurnal yang dibuktikannya.
//
// Sengaja terpisah dari /pengaturan. Layar setelan menjawab "bagaimana nomor
// dibentuk" (master, jarang disentuh); layar ini menjawab "nomor apa saja yang
// sudah terbit" (dibaca tiap hari). Menggabungkannya memaksa akuntan masuk ke
// layar setelan untuk pekerjaan harian.

import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import {
  fetchDocuments,
  fetchDocumentTypes,
  fetchCashDocumentAudit,
  type DocumentRow,
  type DocumentType,
  type CashAuditReport,
} from "@/lib/api/document";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";

function formatRupiah(value: string): string {
  const num = parseFloat(value);
  if (isNaN(num) || num === 0) return "—";
  return "Rp " + num.toLocaleString("id-ID", { maximumFractionDigits: 0 });
}

function formatDate(iso: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  return d.toLocaleDateString("id-ID", { day: "2-digit", month: "short", year: "numeric" });
}

export function DocumentRegisterView({ token }: { token: string }) {
  const router = useRouter();
  const [types, setTypes] = useState<DocumentType[]>([]);
  const [rows, setRows] = useState<DocumentRow[]>([]);
  const [audit, setAudit] = useState<CashAuditReport | null>(null);
  const [typeFilter, setTypeFilter] = useState("");
  const [yearFilter, setYearFilter] = useState<number>(new Date().getFullYear());
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const typeName = useMemo(() => {
    const map: Record<string, string> = {};
    types.forEach((t) => (map[t.code] = t.name));
    return map;
  }, [types]);

  const loadRows = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setRows(await fetchDocuments(token, { type: typeFilter || undefined, year: yearFilter, limit: 200 }));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Gagal memuat dokumen.");
    } finally {
      setLoading(false);
    }
  }, [token, typeFilter, yearFilter]);

  useEffect(() => {
    fetchDocumentTypes(token).then(setTypes).catch(() => setTypes([]));
    // Audit SELALU seluruh tenant — tidak mengikuti filter jenis/tahun. Audit
    // yang ikut terfilter akan terbaca "bersih" hanya karena pengguna sedang
    // menyaring, dan itu justru menyembunyikan pelanggaran.
    fetchCashDocumentAudit(token, 20).then(setAudit).catch(() => setAudit(null));
  }, [token]);

  useEffect(() => {
    loadRows();
  }, [loadRows]);

  const years = useMemo(() => {
    const now = new Date().getFullYear();
    return [now, now - 1, now - 2, now - 3];
  }, []);

  return (
    <div className="space-y-5">
      <AuditCard report={audit} />

      <div className="flex items-center gap-3 flex-wrap">
        <select
          value={typeFilter}
          onChange={(e) => setTypeFilter(e.target.value)}
          className="rounded-lg border border-border bg-surface px-3 py-1.5 text-sm text-text-primary"
        >
          <option value="">Semua jenis</option>
          {types.map((t) => (
            <option key={t.code} value={t.code}>
              {t.prefix} — {t.name}
            </option>
          ))}
        </select>
        <select
          value={yearFilter}
          onChange={(e) => setYearFilter(Number(e.target.value))}
          className="rounded-lg border border-border bg-surface px-3 py-1.5 text-sm text-text-primary"
        >
          {years.map((y) => (
            <option key={y} value={y}>
              {y}
            </option>
          ))}
        </select>
        <span className="ml-auto text-sm text-text-secondary">
          {loading ? "Memuat…" : `${rows.length} dokumen`}
        </span>
      </div>

      {error ? (
        <ErrorState description={error} onRetry={loadRows} />
      ) : loading ? (
        <div className="bg-surface rounded-xl border border-border p-5 space-y-3">
          {[0, 1, 2, 3].map((i) => (
            <div key={i} className="h-4 rounded bg-border-subtle animate-pulse" />
          ))}
        </div>
      ) : rows.length === 0 ? (
        <div className="bg-surface rounded-xl border border-border">
          <EmptyState
            title={typeFilter ? "Tidak ada dokumen pada filter ini" : "Belum ada dokumen terbit"}
            description={
              typeFilter
                ? "Tidak ada nomor bukti untuk jenis dan tahun yang dipilih. Ubah filter untuk melihat yang lain."
                : "Nomor bukti terbit sendiri saat transaksi kas diposting — kwitansi termin, booking, kas keluar. Tidak ada yang perlu dibuat manual di sini."
            }
            action={
              <div className="flex items-center gap-2">
                <button
                  onClick={() => router.push("/accounting/jurnal")}
                  className="px-3 py-1.5 bg-accent text-white text-sm rounded-lg hover:bg-accent/90 transition-colors"
                >
                  Buka Jurnal
                </button>
                <button
                  onClick={() => router.push("/pengaturan")}
                  className="px-3 py-1.5 rounded-lg border border-border text-text-secondary text-sm hover:text-accent hover:border-accent/40 transition-colors"
                >
                  Atur format penomoran
                </button>
              </div>
            }
          />
        </div>
      ) : (
        <div className="bg-surface rounded-xl border border-border overflow-x-auto scrollbar-thin">
          <table className="w-full text-sm min-w-[680px]">
            <thead>
              <tr className="bg-bg border-b border-border text-text-secondary">
                <th className="px-4 py-3 text-left font-medium">Nomor</th>
                <th className="px-4 py-3 text-left font-medium">Jenis</th>
                <th className="px-4 py-3 text-left font-medium">Terbit</th>
                <th className="px-4 py-3 text-right font-medium">Nilai</th>
                <th className="px-4 py-3 text-left font-medium">Sumber</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((d) => (
                <tr key={d.id} className="border-b border-border-subtle last:border-0">
                  <td className="px-4 py-3 font-mono text-text-primary whitespace-nowrap">{d.number}</td>
                  <td className="px-4 py-3 text-text-secondary">
                    {typeName[d.document_type_code] ?? d.document_type_code}
                  </td>
                  <td className="px-4 py-3 text-text-secondary whitespace-nowrap">{formatDate(d.issued_at)}</td>
                  <td className="px-4 py-3 text-right font-mono text-text-primary whitespace-nowrap">
                    {formatRupiah(d.amount)}
                  </td>
                  <td className="px-4 py-3">
                    <SourceCell row={d} onOpenJournal={(id) => router.push(`/accounting/jurnal/${id}`)} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// SourceCell — dokumen selalu lahir DARI sesuatu. Kalau asalnya jurnal, beri
// jalan ke jurnalnya: penelusuran Document → Journal adalah separuh dari
// INV-DOC-1, dan separuh yang tidak bisa diklik sama saja tidak ada.
function SourceCell({ row, onOpenJournal }: { row: DocumentRow; onOpenJournal: (id: number) => void }) {
  if (row.source_table === "journal_entries" && row.source_id) {
    return (
      <button onClick={() => onOpenJournal(row.source_id)} className="text-accent hover:underline">
        Jurnal #{row.source_id} →
      </button>
    );
  }
  if (row.source_table) {
    return (
      <span className="text-text-secondary">
        {row.source_table}
        {row.source_id ? ` #${row.source_id}` : ""}
      </span>
    );
  }
  return <span className="text-text-secondary">—</span>;
}

// AuditCard — kesehatan INV-DOC-1. Tiga keadaan, dan keadaan ketiga penting:
// audit "bersih" pada buku yang belum punya pergerakan kas bukan kabar baik,
// jadi tidak boleh ditampilkan hijau.
function AuditCard({ report }: { report: CashAuditReport | null }) {
  if (!report) return null;

  if (report.cash_journals_posted === 0) {
    return (
      <div className="rounded-xl border border-border bg-surface px-5 py-4">
        <div className="text-sm font-semibold text-text-primary">Bukti kas (INV-DOC-1)</div>
        <p className="text-sm text-text-secondary mt-0.5">
          Belum ada pergerakan kas yang diposting — belum ada yang bisa diperiksa.
        </p>
      </div>
    );
  }

  if (report.clean) {
    return (
      <div className="rounded-xl border border-success/30 bg-success-bg px-5 py-4">
        <div className="text-sm font-semibold text-success">✓ Bukti kas utuh</div>
        <p className="text-sm text-text-secondary mt-0.5">
          {report.cash_journals_posted} jurnal kas terposting ·{" "}
          {report.cash_journals_documented} berdokumen · {report.cash_journals_exempt} saldo awal
          (dikecualikan). Setiap pergerakan kas punya tepat satu bukti bernomor.
        </p>
      </div>
    );
  }

  return (
    <div className="rounded-xl border border-danger/30 bg-danger-bg px-5 py-4 space-y-2">
      <div className="text-sm font-semibold text-danger">
        ✗ {report.violations} pelanggaran INV-DOC-1
      </div>
      <p className="text-sm text-text-secondary">
        {report.cash_journals_posted} jurnal kas terposting · {report.cash_journals_documented}{" "}
        berdokumen · {report.cash_journals_exempt} saldo awal.
      </p>
      <ul className="space-y-2">
        {report.checks
          .filter((c) => c.count > 0)
          .map((c) => (
            <li key={c.code}>
              <div className="text-sm font-medium text-text-primary">
                {c.title} · {c.count}
              </div>
              <ul className="mt-0.5 space-y-0.5">
                {(c.findings ?? []).map((f, i) => (
                  <li key={i} className="text-xs text-text-secondary">
                    {f.journal_id ? `Jurnal #${f.journal_id}` : f.number || "—"} — {f.detail}
                  </li>
                ))}
                {c.truncated && (
                  <li className="text-xs text-text-secondary italic">
                    … {c.count - (c.findings?.length ?? 0)} temuan lagi tidak ditampilkan
                  </li>
                )}
              </ul>
            </li>
          ))}
      </ul>
    </div>
  );
}
