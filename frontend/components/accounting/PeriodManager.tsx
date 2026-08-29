"use client";

import { useEffect, useState, useCallback } from "react";
import { fetchPeriods, closePeriod, reopenPeriod } from "@/lib/api/ledger";
import type { AccountingPeriod } from "@/lib/types/api";
import { Can } from "@/components/ui/Can";

interface Props {
  token: string;
}

const MONTH_NAMES = [
  "", "Januari", "Februari", "Maret", "April", "Mei", "Juni",
  "Juli", "Agustus", "September", "Oktober", "November", "Desember",
];

function formatPeriod(year: number, month: number): string {
  return `${MONTH_NAMES[month] ?? month} ${year}`;
}

function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString("id-ID", {
    day: "2-digit", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit",
  });
}

export function PeriodManager({ token }: Props) {
  const [periods, setPeriods] = useState<AccountingPeriod[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [acting, setActing] = useState<string | null>(null); // "year-month" while processing
  const [confirmClose, setConfirmClose] = useState<AccountingPeriod | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await fetchPeriods(token);
      setPeriods(data);
    } catch {
      setError("Gagal memuat periode akuntansi.");
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => { load(); }, [load]);

  async function handleClose(period: AccountingPeriod) {
    const key = `${period.year}-${period.month}`;
    setActing(key);
    setError(null);
    try {
      const updated = await closePeriod(token, period.year, period.month);
      setPeriods(prev => prev.map(p =>
        p.year === updated.year && p.month === updated.month ? updated : p
      ));
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Gagal menutup periode.");
    } finally {
      setActing(null);
      setConfirmClose(null);
    }
  }

  async function handleReopen(period: AccountingPeriod) {
    const key = `${period.year}-${period.month}`;
    setActing(key);
    setError(null);
    try {
      const updated = await reopenPeriod(token, period.year, period.month);
      setPeriods(prev => prev.map(p =>
        p.year === updated.year && p.month === updated.month ? updated : p
      ));
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Gagal membuka kembali periode.");
    } finally {
      setActing(null);
    }
  }

  return (
    <div className="space-y-4">
      {error && (
        <div className="rounded-lg bg-danger-bg border border-danger/30 px-4 py-3 text-sm text-danger">{error}</div>
      )}

      {loading ? (
        <div className="space-y-2">
          {[...Array(4)].map((_, i) => (
            <div key={i} className="h-14 bg-border-subtle rounded-xl animate-pulse" />
          ))}
        </div>
      ) : periods.length === 0 ? (
        <div className="text-center py-16 text-text-secondary">
          <p className="text-lg font-medium">Belum ada periode</p>
          <p className="text-sm mt-1">Periode akan muncul otomatis saat ada jurnal yang dicatat</p>
        </div>
      ) : (
        <div className="bg-surface rounded-xl border border-border overflow-x-auto scrollbar-thin">
          <table className="w-full text-sm min-w-[560px]">
            <thead>
              <tr className="bg-bg border-b border-border text-text-secondary">
                <th className="px-5 py-3 text-left font-medium">Periode</th>
                <th className="px-5 py-3 text-left font-medium">Status</th>
                <th className="px-5 py-3 text-left font-medium">Ditutup Pada</th>
                <th className="px-5 py-3 text-right font-medium">Aksi</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {periods.map(p => {
                const key = `${p.year}-${p.month}`;
                const isActing = acting === key;
                const isClosed = p.status === "closed";
                return (
                  <tr key={key} className="hover:bg-bg transition-colors">
                    <td className="px-5 py-3 font-medium text-text-primary">
                      {formatPeriod(p.year, p.month)}
                    </td>
                    <td className="px-5 py-3">
                      {isClosed ? (
                        <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-danger-bg text-danger">
                          <span className="w-1.5 h-1.5 rounded-full bg-danger" />
                          Ditutup
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-success-bg text-success">
                          <span className="w-1.5 h-1.5 rounded-full bg-success" />
                          Terbuka
                        </span>
                      )}
                    </td>
                    <td className="px-5 py-3 text-text-secondary text-xs">
                      {p.closed_at ? formatDateTime(p.closed_at) : "—"}
                    </td>
                    <td className="px-5 py-3 text-right">
                      {!isClosed ? (
                        <Can roles={["owner", "accountant"]}>
                          <button
                            onClick={() => setConfirmClose(p)}
                            disabled={isActing}
                            className="px-3 py-1.5 text-xs rounded-lg border border-danger/30 text-danger hover:bg-danger-bg disabled:opacity-50 transition-colors"
                          >
                            Tutup Buku
                          </button>
                        </Can>
                      ) : (
                        <Can roles={["owner"]}>
                          <button
                            onClick={() => handleReopen(p)}
                            disabled={isActing}
                            className="px-3 py-1.5 text-xs rounded-lg border border-border text-text-secondary hover:bg-bg disabled:opacity-50 transition-colors"
                          >
                            {isActing ? "Memproses..." : "Buka Kembali"}
                          </button>
                        </Can>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {/* Confirmation modal for close */}
      {confirmClose && (
        <div
          className="fixed inset-0 bg-text-primary/30 backdrop-blur-sm flex items-center justify-center z-overlay"
          onClick={() => setConfirmClose(null)}
        >
          <div
            className="bg-surface rounded-2xl p-6 w-96 shadow-xl space-y-4"
            onClick={e => e.stopPropagation()}
          >
            <div className="flex items-start gap-3">
              <div className="w-10 h-10 rounded-full bg-danger-bg flex items-center justify-center shrink-0">
                <svg className="w-5 h-5 text-danger" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.998L13.732 4c-.77-1.331-2.694-1.331-3.464 0L3.34 16.002C2.57 17.333 3.53 19 5.07 19z" />
                </svg>
              </div>
              <div>
                <h3 className="font-semibold text-text-primary">Tutup Periode?</h3>
                <p className="text-sm text-text-secondary mt-1">
                  Periode <strong>{formatPeriod(confirmClose.year, confirmClose.month)}</strong> akan ditutup.
                  Semua posting jurnal dengan tanggal di bulan ini akan ditolak.
                </p>
              </div>
            </div>
            <p className="text-xs text-text-secondary bg-bg rounded-lg p-3">
              Hanya owner yang dapat membuka kembali periode yang sudah ditutup.
            </p>
            <div className="flex gap-2">
              <button
                onClick={() => handleClose(confirmClose)}
                disabled={acting !== null}
                className="flex-1 px-4 py-2 rounded-lg bg-danger text-white text-sm font-medium hover:opacity-90 disabled:opacity-50 transition-colors"
              >
                {acting ? "Menutup..." : "Ya, Tutup Periode"}
              </button>
              <button
                onClick={() => setConfirmClose(null)}
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
