"use client";

// Titipan Notaris — LAYAR WARISAN (read + drain saja).
//
// W-1 (2026-08-06) menutup jalur intake notaris: satu-satunya cara menerima
// titipan pihak ketiga sekarang adalah Charge Group realisasi, yang jenis biaya
// dan akun kewajibannya berasal dari master (Pengaturan → Biaya Realisasi).
// `POST /notary-deposits` menjawab 410 Gone — form penerimaan di sini akan
// selalu gagal, jadi ia dihapus, bukan disembunyikan.
//
// Yang TIDAK boleh hilang: titipan lama yang masih tertahan wajib tetap bisa
// dibayarkan ke notaris. Selama masih ada baris berstatus `held`, halaman ini
// hidup. Riwayatnya tidak pernah ditulis ulang (ledger append-only).

import { useCallback, useEffect, useState } from "react";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { Modal } from "@/components/ui/Modal";
import { EmptyState } from "@/components/ui/EmptyState";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { CashBankSelect } from "@/components/accounting/CashBankSelect";
import { useToast } from "@/components/ui/Toast";
import { apiFetch, ApiError } from "@/lib/api/client";
import { fetchCustomers, type Customer } from "@/lib/api/party";

interface NotaryDeposit {
  id: number;
  customer_id: number;
  unit_id?: number;
  amount: string;
  status: "held" | "paid_out";
  notary_name: string;
  notes?: string;
  received_at: string;
  paid_out_at?: string;
}

export function NotaryDepositView({ token }: { token: string }) {
  const { toast } = useToast();
  const [rows, setRows] = useState<NotaryDeposit[]>([]);
  const [customers, setCustomers] = useState<Customer[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [payoutTarget, setPayoutTarget] = useState<NotaryDeposit | null>(null);
  const [payoutBank, setPayoutBank] = useState("");

  const load = useCallback(async () => {
    try {
      const res = await apiFetch<NotaryDeposit[]>("/notary-deposits", { token });
      setRows(res ?? []);
    } catch {
      setRows([]);
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => {
    void load();
    fetchCustomers(token).then(setCustomers).catch(() => setCustomers([]));
  }, [load, token]);

  async function handlePayout() {
    if (!payoutTarget || !payoutBank) return;
    setBusy(true);
    try {
      await apiFetch(`/notary-deposits/${payoutTarget.id}/payout`, {
        token, method: "POST",
        body: { bank_account_code: payoutBank },
      });
      toast("Titipan dibayarkan ke notaris.", "success");
      setPayoutTarget(null);
      setPayoutBank("");
      await load();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal membayarkan titipan", "error");
    } finally {
      setBusy(false);
    }
  }

  const customerName = (id: number) => customers.find((c) => c.id === id)?.name ?? `#${id}`;
  const heldCount = rows.filter((d) => d.status === "held").length;

  return (
    <Card padding="none">
      <CardHeader className="px-5 pt-5">
        <div>
          <CardTitle>Titipan Notaris (warisan)</CardTitle>
          <p className="text-xs text-text-secondary mt-0.5">
            Titipan yang dicatat sebelum jalur ini dipindahkan. Masih kewajiban sampai
            dibayarkan — bukan pendapatan. Titipan <strong>baru</strong> dicatat lewat
            Biaya Realisasi di halaman unit.
          </p>
        </div>
      </CardHeader>
      {loading ? (
        <p className="px-5 py-6 text-sm text-text-secondary animate-pulse">Memuat…</p>
      ) : rows.length === 0 ? (
        <div className="px-5 pb-5">
          <EmptyState
            title="Tidak ada titipan warisan"
            description="Semua titipan pihak ketiga kini dicatat sebagai Biaya Realisasi pada unit — buka unit terkait, lalu buat grup tagihan Biaya Realisasi."
          />
        </div>
      ) : (
        <>
          {heldCount > 0 && (
            <p className="mx-5 mb-3 rounded-md border border-warning/40 bg-warning-bg px-3 py-2 text-xs text-text-secondary">
              <strong>{heldCount}</strong> titipan masih tertahan sebagai kewajiban.
              Bayarkan ke notaris untuk menutupnya.
            </p>
          )}
          <Table>
            <TableHead>
              <TableRow>
                <Th>Tanggal</Th>
                <Th>Customer</Th>
                <Th>Penerima</Th>
                <Th right>Nominal</Th>
                <Th>Status</Th>
                <Th></Th>
              </TableRow>
            </TableHead>
            <TableBody>
              {rows.map((d) => (
                <TableRow key={d.id}>
                  <Td><Tanggal value={d.received_at} /></Td>
                  <Td>{customerName(d.customer_id)}</Td>
                  <Td className="text-text-secondary">{d.notary_name || "—"}</Td>
                  <Td right><Rupiah value={d.amount} colorSign={false} /></Td>
                  <Td>
                    {d.status === "held"
                      ? <Badge variant="warning">Tertahan (kewajiban)</Badge>
                      : <Badge variant="success">Dibayarkan</Badge>}
                  </Td>
                  <Td right>
                    {d.status === "held" && (
                      <Button size="sm" variant="ghost" onClick={() => setPayoutTarget(d)}>
                        Bayarkan →
                      </Button>
                    )}
                  </Td>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </>
      )}

      {/* Payout — satu-satunya aksi yang tersisa di jalur warisan. */}
      <Modal
        open={payoutTarget !== null}
        onClose={() => setPayoutTarget(null)}
        title={payoutTarget ? `Bayarkan Titipan — Rp ${Number(payoutTarget.amount).toLocaleString("id-ID")}` : ""}
        size="sm"
        footer={
          <>
            <Button variant="ghost" onClick={() => setPayoutTarget(null)} disabled={busy}>Batal</Button>
            <Button onClick={handlePayout} loading={busy} disabled={!payoutBank || busy}>Bayarkan</Button>
          </>
        }
      >
        <div className="space-y-4">
          <CashBankSelect token={token} value={payoutBank} onChange={setPayoutBank} label="Bayar dari Rekening" required />
          <p className="rounded-md bg-border-subtle/40 px-3 py-2 text-[11px] text-text-secondary">
            Jurnal: Dr akun titipan · Cr Kas/Bank. Kewajiban lunas.
          </p>
        </div>
      </Modal>
    </Card>
  );
}
