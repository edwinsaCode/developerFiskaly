"use client";

import { useState, useEffect, useCallback } from "react";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Rupiah } from "@/components/format/Rupiah";
import { TaxObligationTable } from "./TaxObligationTable";
import { TaxRateForm } from "./TaxRateForm";
import {
  fetchCombinedTaxReport,
  fetchBASTWithoutPPh,
} from "@/lib/api/tax";
import type { CombinedTaxReport, BASTWithoutPPhItem, Unit } from "@/lib/types/api";

interface Props {
  soldUnits: Unit[];
  token: string;
}

const TABS = [
  { id: "kewajiban", label: "Kewajiban PPh" },
  { id: "ppn",       label: "Laporan PPN" },
  { id: "setting",   label: "Pengaturan Tarif" },
] as const;
type TabId = (typeof TABS)[number]["id"];

// Default: rentang lebar agar menangkap data demo 2025 dan transaksi 2026
const DEFAULT_FROM = "2024-01-01";
const DEFAULT_TO   = "2027-12-31";

export function TaxDashboard({ soldUnits, token }: Props) {
  const [tab,          setTab]     = useState<TabId>("kewajiban");
  const [from,         setFrom]    = useState(DEFAULT_FROM);
  const [to,           setTo]      = useState(DEFAULT_TO);
  const [report,       setReport]  = useState<CombinedTaxReport | null>(null);
  const [bastAlert,    setBastAlert] = useState<BASTWithoutPPhItem[]>([]);
  const [loading,      setLoading] = useState(false);
  const [error,        setError]   = useState<string | null>(null);
  const [refreshKey,   setRefreshKey] = useState(0);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [rep, bast] = await Promise.all([
        fetchCombinedTaxReport(token, from, to),
        fetchBASTWithoutPPh(token, from, to),
      ]);
      setReport(rep);
      setBastAlert(bast ?? []);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Gagal memuat data pajak");
    } finally {
      setLoading(false);
    }
  }, [token, from, to, refreshKey]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => { load(); }, [load]);

  const pph   = report?.pph_final;
  const ppn   = report?.ppn;
  const items = pph?.items ?? [];

  function SummaryCard({
    label, value, sub, colorClass,
  }: { label: string; value: string; sub?: string; colorClass?: string }) {
    return (
      <Card padding="sm">
        <p className="text-xs text-text-secondary mb-1">{label}</p>
        <p className={`text-lg font-bold ${colorClass ?? "text-text-primary"}`}>
          <Rupiah value={value} colorSign={false} />
        </p>
        {sub && <p className="text-xs text-text-tertiary mt-0.5">{sub}</p>}
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      {/* BAST audit alert */}
      {bastAlert.length > 0 && (
        <div className="flex items-start gap-3 bg-warning-bg border border-warning/40 rounded p-3 text-sm">
          <span className="text-warning mt-0.5 shrink-0">⚠</span>
          <div>
            <p className="font-semibold text-warning">
              {bastAlert.length} unit sudah BAST belum diakrualkan PPh Final
            </p>
            <p className="text-text-secondary text-xs mt-0.5">
              Unit ID: {bastAlert.map((b) => b.unit_id).join(", ")}.
              Gunakan <strong>+ Akrual Baru</strong> di tab Kewajiban PPh untuk mencatatnya.
            </p>
          </div>
        </div>
      )}

      {/* Period filter */}
      <div className="flex flex-wrap items-end gap-3">
        <div>
          <label className="block text-xs text-text-secondary mb-1">Periode dari</label>
          <input
            type="date"
            value={from}
            onChange={(e) => setFrom(e.target.value)}
            className="h-9 rounded border border-border bg-surface px-3 text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
          />
        </div>
        <div>
          <label className="block text-xs text-text-secondary mb-1">sampai</label>
          <input
            type="date"
            value={to}
            onChange={(e) => setTo(e.target.value)}
            className="h-9 rounded border border-border bg-surface px-3 text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
          />
        </div>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => setRefreshKey((k) => k + 1)}
          loading={loading}
        >
          Muat Ulang
        </Button>
      </div>

      {/* Summary cards — PPh Final */}
      {pph && (
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <SummaryCard
            label="Total Kewajiban PPh"
            value={pph.total_obligation}
            sub="akrual dalam periode"
          />
          <SummaryCard
            label="Sudah Disetor"
            value={pph.total_paid}
            colorClass="text-success"
            sub="status paid"
          />
          <SummaryCard
            label="Belum Disetor"
            value={pph.total_outstanding}
            colorClass={parseFloat(pph.total_outstanding) > 0 ? "text-danger" : "text-text-primary"}
            sub="status outstanding"
          />
        </div>
      )}

      {error && (
        <Card padding="sm">
          <p className="text-sm text-danger">{error}</p>
        </Card>
      )}

      {/* Tabs */}
      <div>
        <div className="flex gap-0.5 border-b border-border mb-4">
          {TABS.map((t) => (
            <button
              key={t.id}
              onClick={() => setTab(t.id)}
              className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px transition-colors
                ${tab === t.id
                  ? "border-accent text-accent"
                  : "border-transparent text-text-secondary hover:text-accent"
                }`}
            >
              {t.label}
            </button>
          ))}
        </div>

        {/* Tab: Kewajiban PPh */}
        {tab === "kewajiban" && (
          <TaxObligationTable
            items={items}
            soldUnits={soldUnits}
            token={token}
            onRefresh={() => setRefreshKey((k) => k + 1)}
          />
        )}

        {/* Tab: Laporan PPN */}
        {tab === "ppn" && (
          // Konten sempit (kartu laporan / form) dipusatkan, bukan menempel
          // kiri dengan ruang kosong menganga di kanan.
          <div className="space-y-4 mx-auto w-full max-w-2xl">
            {ppn ? (
              <Card>
                <CardHeader>
                  <CardTitle>Laporan PPN</CardTitle>
                </CardHeader>
                <div className="space-y-2 text-sm">
                  <Row label="PPN Keluaran (Cr 2-3000)" value={ppn.ppn_keluaran} />
                  <Row label="PPN Masukan (Dr 1-5100)"  value={ppn.ppn_masukan} />
                  <div className="border-t border-border pt-2 mt-2">
                    <Row
                      label="PPN Terutang (Keluaran − Masukan)"
                      value={ppn.ppn_terutang}
                      bold
                    />
                  </div>
                </div>
                <p className="mt-4 text-xs text-text-tertiary">
                  {/* TODO(tax-advisor): Konfirmasi 12% vs 11% efektif untuk hunian mewah LITHOS sebelum filing SPT Masa PPN */}
                  Sumber: rekonsiliasi ke saldo akun 2-3000 (PPN Keluaran) dan 1-5100 (PPN Masukan)
                  di buku besar periode {from} — {to}.
                </p>
              </Card>
            ) : (
              <Card padding="sm">
                <p className="text-sm text-text-secondary py-4 text-center">
                  Tidak ada data PPN dalam periode ini, atau tidak ada transaksi PKP.
                </p>
              </Card>
            )}
          </div>
        )}

        {/* Tab: Pengaturan */}
        {tab === "setting" && (
          <div className="mx-auto w-full max-w-2xl">
            <TaxRateForm token={token} />
          </div>
        )}
      </div>
    </div>
  );
}

function Row({
  label, value, bold,
}: { label: string; value: string; bold?: boolean }) {
  return (
    <div className={`flex justify-between ${bold ? "font-semibold" : ""}`}>
      <span className={bold ? "text-text-primary" : "text-text-secondary"}>{label}</span>
      <Rupiah value={value} colorSign={false} />
    </div>
  );
}
