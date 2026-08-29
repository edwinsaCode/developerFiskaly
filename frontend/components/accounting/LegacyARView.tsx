"use client";

import { useCallback, useMemo, useState } from "react";
import Link from "next/link";
import { Card } from "@/components/ui/Card";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Input, Select } from "@/components/ui/Input";
import { EmptyState } from "@/components/ui/EmptyState";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import { LegacyReconciliationCard } from "@/components/accounting/LegacyReconciliationCard";
import { LegacyARPaymentModal } from "@/components/accounting/LegacyARPaymentModal";
import { fetchLegacyReceivables, fetchLegacyReconciliation } from "@/lib/api/legacyar";
import type {
  LegacyListResult,
  LegacyReceivableView,
  LegacyReconciliation,
  LegacyStatus,
} from "@/lib/api/legacyar";

// Halaman kerja Piutang Proyek Lama.
//
// Ini BUKAN laporan aging kedua — aging tetap satu, di /accounting/receivable,
// dengan legacy sebagai salah satu sumbernya. Halaman ini adalah tempat
// mengelola rinciannya: impor, cocokkan dengan buku besar, terima pembayaran.
// Aging menjawab "siapa menunggak"; halaman ini menjawab "apakah rinciannya
// benar-benar sama dengan buku besar".

interface Props {
  token: string;
  initial: LegacyListResult;
  initialRec: LegacyReconciliation;
}

const STATUS_LABEL: Record<LegacyStatus, string> = {
  open: "Belum Lunas",
  paid: "Lunas",
  written_off: "Dihapusbukukan",
};

const STATUS_VARIANT: Record<LegacyStatus, "warning" | "success" | "neutral"> = {
  open: "warning",
  paid: "success",
  written_off: "neutral",
};

export function LegacyARView({ token, initial, initialRec }: Props) {
  const [data, setData] = useState(initial);
  const [rec, setRec] = useState(initialRec);
  const [status, setStatus] = useState<LegacyStatus | "">("");
  const [sourceLabel, setSourceLabel] = useState("");
  const [q, setQ] = useState("");
  const [loading, setLoading] = useState(false);
  const [selected, setSelected] = useState<number[]>([]);
  const [payOpen, setPayOpen] = useState(false);

  const reload = useCallback(
    async (next?: { status?: LegacyStatus | ""; sourceLabel?: string; q?: string }) => {
      setLoading(true);
      try {
        const [list, r] = await Promise.all([
          fetchLegacyReceivables(token, {
            status: next?.status ?? status,
            source_label: next?.sourceLabel ?? sourceLabel,
            q: next?.q ?? q,
          }),
          // Rekonsiliasi ikut disegarkan: setelah pembayaran, "efek pelunasan"
          // di kartu itu yang berubah — kalau kartunya basi, angkanya justru
          // membuat orang mengira ada selisih baru.
          fetchLegacyReconciliation(token, rec.as_of_date, rec.control_account_code),
        ]);
        setData(list);
        setRec(r);
      } finally {
        setLoading(false);
      }
    },
    [token, status, sourceLabel, q, rec.as_of_date, rec.control_account_code],
  );

  const rows = data.rows;
  const openRows = useMemo(() => rows.filter((r) => r.status === "open"), [rows]);
  const selectedRows = useMemo(
    () => rows.filter((r) => selected.includes(r.id)),
    [rows, selected],
  );

  function toggle(id: number) {
    setSelected((s) => (s.includes(id) ? s.filter((x) => x !== id) : [...s, id]));
  }
  function toggleAll() {
    setSelected((s) => (s.length === openRows.length ? [] : openRows.map((r) => r.id)));
  }

  const isEmpty = rows.length === 0 && !status && !sourceLabel && !q;

  return (
    <div className="space-y-5">
      <LegacyReconciliationCard rec={rec} />

      {isEmpty ? (
        <Card>
          <EmptyState
            title="Belum ada piutang proyek lama"
            description="Kalau ada sisa tagihan customer dari proyek yang berjalan sebelum sistem ini dipakai, impor rinciannya di sini. Impor tidak membuat jurnal — ia mengisi rincian atas saldo piutang yang sudah ada di buku besar."
            action={
              <Link href="/accounting/legacy-ar/import">
                <Button>Impor Piutang Proyek Lama</Button>
              </Link>
            }
          />
        </Card>
      ) : (
        <>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <Kpi label="Jumlah piutang" value={String(data.summary.count)} plain />
            <Kpi label="Nilai asli" value={data.summary.total_original} />
            <Kpi label="Sudah dibayar" value={data.summary.total_paid} />
            <Kpi label="Sisa tagihan" value={data.summary.total_outstanding} accent />
          </div>

          <Card padding="none">
            <div className="flex flex-wrap items-end gap-3 border-b border-border p-4">
              <Input
                label="Cari"
                placeholder="Nama customer / referensi"
                value={q}
                onChange={(e) => setQ(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") reload();
                }}
                className="w-56"
              />
              <div className="w-44">
                <Select
                  label="Status"
                  value={status}
                  onChange={(e) => {
                    const v = e.target.value as LegacyStatus | "";
                    setStatus(v);
                    reload({ status: v });
                  }}
                >
                  <option value="">Semua status</option>
                  <option value="open">Belum Lunas</option>
                  <option value="paid">Lunas</option>
                  <option value="written_off">Dihapusbukukan</option>
                </Select>
              </div>
              {data.source_labels.length > 0 && (
                <div className="w-52">
                  <Select
                    label="Proyek lama"
                    value={sourceLabel}
                    onChange={(e) => {
                      setSourceLabel(e.target.value);
                      reload({ sourceLabel: e.target.value });
                    }}
                  >
                    <option value="">Semua proyek</option>
                    {data.source_labels.map((s) => (
                      <option key={s} value={s}>
                        {s}
                      </option>
                    ))}
                  </Select>
                </div>
              )}
              <Button variant="secondary" onClick={() => reload()} loading={loading}>
                Terapkan
              </Button>

              <div className="ml-auto flex items-center gap-2">
                <Button
                  onClick={() => setPayOpen(true)}
                  disabled={selectedRows.length === 0}
                  title={
                    selectedRows.length === 0
                      ? "Pilih dulu piutang yang dibayar"
                      : undefined
                  }
                >
                  Terima Pembayaran
                  {selectedRows.length > 0 ? ` (${selectedRows.length})` : ""}
                </Button>
                <Link href="/accounting/legacy-ar/import">
                  <Button variant="secondary">Impor</Button>
                </Link>
              </div>
            </div>

            {rows.length === 0 ? (
              <EmptyState
                title="Tidak ada yang cocok"
                description="Ubah kata kunci atau filternya."
              />
            ) : (
              <Table>
                <TableHead>
                  <tr>
                    <Th>
                      <input
                        type="checkbox"
                        aria-label="Pilih semua yang belum lunas"
                        checked={openRows.length > 0 && selected.length === openRows.length}
                        onChange={toggleAll}
                        disabled={openRows.length === 0}
                      />
                    </Th>
                    <Th>Customer</Th>
                    <Th>Proyek Lama</Th>
                    <Th>Referensi</Th>
                    <Th>Jatuh Tempo</Th>
                    <Th right>Nilai Asli</Th>
                    <Th right>Dibayar</Th>
                    <Th right>Sisa</Th>
                    <Th>Status</Th>
                    <Th>Aksi</Th>
                  </tr>
                </TableHead>
                <TableBody>
                  {rows.map((r) => (
                    <TableRow key={r.id}>
                      <Td>
                        <input
                          type="checkbox"
                          aria-label={`Pilih ${r.customer_name}`}
                          checked={selected.includes(r.id)}
                          onChange={() => toggle(r.id)}
                          disabled={r.status !== "open"}
                        />
                      </Td>
                      <Td>{r.customer_name}</Td>
                      <Td>{r.source_label}</Td>
                      <Td mono>{r.external_ref || "—"}</Td>
                      <Td>{r.due_date ? <Tanggal value={r.due_date} /> : "—"}</Td>
                      <Td right>
                        <Rupiah value={r.original_amount} colorSign={false} />
                      </Td>
                      <Td right>
                        <Rupiah value={r.paid_amount} colorSign={false} />
                      </Td>
                      <Td right>
                        <Rupiah value={r.outstanding} colorSign={false} />
                      </Td>
                      <Td>
                        <Badge variant={STATUS_VARIANT[r.status]}>{STATUS_LABEL[r.status]}</Badge>
                      </Td>
                      <Td>
                        <Link
                          href={`/accounting/legacy-ar/${r.id}`}
                          className="text-accent hover:underline"
                        >
                          Rincian →
                        </Link>
                      </Td>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </Card>

          <p className="text-xs text-text-tertiary">
            Piutang ini juga muncul di{" "}
            <Link href="/accounting/receivable?source=legacy" className="text-accent hover:underline">
              Piutang Customer
            </Link>{" "}
            sebagai sumber &ldquo;Proyek Lama&rdquo; — umur piutangnya dihitung mesin yang sama
            dengan piutang rumah dan biaya realisasi.
          </p>
        </>
      )}

      <LegacyARPaymentModal
        token={token}
        open={payOpen}
        onClose={() => setPayOpen(false)}
        targets={selectedRows}
        onDone={() => {
          setSelected([]);
          reload();
        }}
      />
    </div>
  );
}

function Kpi({
  label,
  value,
  accent,
  plain,
}: {
  label: string;
  value: string;
  accent?: boolean;
  plain?: boolean;
}) {
  return (
    <Card padding="sm">
      <p className="text-xs uppercase tracking-wide text-text-secondary">{label}</p>
      <p
        className={`mt-1 text-lg font-bold tabular-nums ${
          accent ? "text-accent" : "text-text-primary"
        }`}
      >
        {plain ? value : <Rupiah value={value} colorSign={false} />}
      </p>
    </Card>
  );
}
