"use client";

import { useCallback, useEffect, useState } from "react";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { ConfirmModal } from "@/components/ui/ConfirmModal";
import { Can } from "@/components/ui/Can";
import { Rupiah } from "@/components/format/Rupiah";
import { useToast } from "@/components/ui/Toast";
import { fetchYearClosePreview, closeFiscalYear } from "@/lib/api/ledger";
import { ApiError } from "@/lib/api/client";
import type { ClosingPreview } from "@/lib/types/api";

interface Props {
  token: string;
}

const CURRENT_YEAR = new Date().getFullYear();
const YEARS = [CURRENT_YEAR, CURRENT_YEAR - 1, CURRENT_YEAR - 2];

export function YearEndClosing({ token }: Props) {
  const { toast } = useToast();
  const [year, setYear] = useState(CURRENT_YEAR);
  const [preview, setPreview] = useState<ClosingPreview | null>(null);
  const [loading, setLoading] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [closing, setClosing] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setPreview(await fetchYearClosePreview(token, year));
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal memuat ringkasan tutup buku", "error");
      setPreview(null);
    } finally {
      setLoading(false);
    }
  }, [token, year, toast]);

  useEffect(() => {
    void load();
  }, [load]);

  async function handleClose() {
    setClosing(true);
    try {
      const res = await closeFiscalYear(token, year);
      toast(`Tutup buku ${year} selesai. Laba/rugi bersih dipindahkan ke Laba Ditahan (Jurnal #${res.income_summary_journal_id}).`, "success");
      setConfirming(false);
      await load();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal tutup buku", "error");
    } finally {
      setClosing(false);
    }
  }

  const netIncome = preview?.net_income ?? "0";
  const isLoss = netIncome.startsWith("-");
  const hasData = preview ? preview.revenue_accounts.length > 0 || preview.expense_accounts.length > 0 : false;

  return (
    <Card padding="none">
      <CardHeader className="px-5 pt-5 flex items-center justify-between">
        <div>
          <CardTitle>Tutup Buku Tahunan</CardTitle>
          <p className="text-sm text-text-secondary mt-1">
            Memindahkan saldo Pendapatan & Beban tahun berjalan ke <strong>Laba Ditahan</strong> melalui
            Ikhtisar Laba Rugi. Setelah ini, akun nominal kembali nol untuk tahun berikutnya.
          </p>
        </div>
        <select
          value={year}
          onChange={(e) => setYear(Number(e.target.value))}
          className="text-sm border border-border rounded-md px-2.5 py-1.5 bg-surface text-text-primary"
        >
          {YEARS.map((y) => (
            <option key={y} value={y}>Tahun {y}</option>
          ))}
        </select>
      </CardHeader>

      <div className="px-5 pb-5">
        {loading ? (
          <p className="text-sm text-text-secondary py-6 text-center">Memuat…</p>
        ) : !preview ? (
          <p className="text-sm text-danger py-6 text-center">Tidak dapat memuat data tutup buku.</p>
        ) : preview.already_closed ? (
          <div className="flex items-center gap-3 bg-success-bg border border-success/30 rounded-lg px-4 py-3">
            <span className="text-success text-lg">✓</span>
            <div>
              <p className="text-sm font-medium text-success">Tahun {year} sudah ditutup</p>
              <p className="text-xs text-text-secondary mt-0.5">
                Entri penutup (source <span className="font-mono">closing</span>) sudah diposting. Untuk koreksi,
                balikkan jurnal penutup via Jurnal lalu tutup ulang.
              </p>
            </div>
          </div>
        ) : !hasData ? (
          <div className="bg-border-subtle/40 border border-border rounded-lg px-4 py-3">
            <p className="text-sm text-text-secondary">
              Belum ada pendapatan atau beban yang diposting pada {year} — tidak ada yang perlu ditutup.
              Tutup buku akan tersedia setelah ada transaksi laba rugi (penjualan/BAST atau beban).
            </p>
          </div>
        ) : (
          <div className="space-y-4">
            {/* Ringkasan laba/rugi */}
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <Pill label="Total Pendapatan" value={preview.total_revenue} />
              <Pill label="Total Beban" value={preview.total_expense} />
              <div className={`rounded-lg px-4 py-3 border ${isLoss ? "border-danger/30 bg-danger-bg" : "border-success/30 bg-success-bg"}`}>
                <p className="text-xs text-text-secondary uppercase tracking-wide mb-1">{isLoss ? "Rugi Bersih" : "Laba Bersih"}</p>
                <p className={`text-lg font-bold tabular-nums ${isLoss ? "text-danger" : "text-success"}`}>
                  <Rupiah value={netIncome} colorSign={false} />
                </p>
              </div>
            </div>

            {/* Akun yang akan ditutup */}
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <AccountList title="Pendapatan ditutup" lines={preview.revenue_accounts} />
              <AccountList title="Beban ditutup" lines={preview.expense_accounts} />
            </div>

            <Can
              roles={["owner"]}
              fallback={<p className="text-xs text-text-tertiary">Hanya <strong>owner</strong> yang dapat menjalankan tutup buku tahunan.</p>}
            >
              <Button variant="danger" onClick={() => setConfirming(true)}>
                Tutup Buku Tahun {year}
              </Button>
            </Can>
          </div>
        )}
      </div>

      <ConfirmModal
        open={confirming}
        title={`Tutup Buku Tahun ${year}?`}
        variant="danger"
        confirmLabel="Ya, Tutup Buku"
        loading={closing}
        onCancel={() => setConfirming(false)}
        onConfirm={handleClose}
      >
        <div className="space-y-2 text-sm">
          <p>Tindakan ini akan memposting 2 jurnal penutup bertanggal 31 Des {year}:</p>
          <ol className="list-decimal list-inside text-text-secondary space-y-1">
            <li>Pendapatan &amp; Beban → Ikhtisar Laba Rugi</li>
            <li>Ikhtisar Laba Rugi → Laba Ditahan (<Rupiah value={netIncome} colorSign={false} />)</li>
          </ol>
          <p className="text-text-secondary">
            Jurnal penutup bersifat permanen (append-only). Koreksi hanya via jurnal pembalik.
          </p>
        </div>
      </ConfirmModal>
    </Card>
  );
}

function Pill({ label, value }: { label: string; value: string }) {
  return (
    <div className="bg-surface border border-border rounded-lg px-4 py-3">
      <p className="text-xs text-text-secondary uppercase tracking-wide mb-1">{label}</p>
      <p className="text-lg font-bold tabular-nums text-text-primary">
        <Rupiah value={value} colorSign={false} />
      </p>
    </div>
  );
}

function AccountList({ title, lines }: { title: string; lines: { code: string; name: string; amount: string }[] }) {
  return (
    <div className="border border-border rounded-lg p-3">
      <p className="text-xs font-semibold text-text-secondary uppercase tracking-wide mb-2">{title}</p>
      {lines.length === 0 ? (
        <p className="text-xs text-text-tertiary">—</p>
      ) : (
        <ul className="space-y-1.5">
          {lines.map((l) => (
            <li key={l.code} className="flex items-center justify-between text-sm">
              <span className="text-text-secondary"><span className="font-mono text-xs">{l.code}</span> {l.name}</span>
              <Rupiah value={l.amount} colorSign={false} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
