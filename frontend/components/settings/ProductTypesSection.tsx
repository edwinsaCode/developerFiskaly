"use client";

// UAT Batch 2 §2 — master Product Catalog: apa saja yang DIJUAL developer,
// plus mapping akun pendapatan per produk (COA-driven; jurnal BAST mengikuti
// mapping ini). Isinya sepenuhnya data — kode maupun nama produk tidak pernah
// dikenali khusus oleh kode program.

import { useCallback, useEffect, useState } from "react";
import { Card } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { Modal } from "@/components/ui/Modal";
import { Input, Select } from "@/components/ui/Input";
import { FormGrid, FormFull } from "@/components/ui/Form";
import { EmptyState } from "@/components/ui/EmptyState";
import { useToast } from "@/components/ui/Toast";
import { ApiError } from "@/lib/api/client";
import {
  fetchProductTypes,
  createProductType,
  updateProductType,
  type ProductType,
} from "@/lib/api/projects";
import { fetchAccounts } from "@/lib/api/ledger";
import type { Account } from "@/lib/types/api";

import { useMayWrite } from "@/lib/hooks/usePermissions";
const CATEGORY_LABEL: Record<string, string> = {
  property: "Properti (ber-HPP/alokasi)",
  non_property: "Non-properti (barang/jasa)",
};

// Konsekuensi akuntansi kategori — ditulis eksplisit karena inilah business
// rule utamanya: kategori menentukan apakah produk ikut HPP & progress proyek.
const CATEGORY_HINT: Record<string, string> = {
  property:
    "Ikut alokasi biaya proyek (HPP) dan progress fisik. Saat serah terima: pendapatan diakui DAN HPP unit dilepas dari persediaan.",
  non_property:
    "TIDAK ikut alokasi HPP maupun progress fisik proyek — biaya proyek tidak boleh dibebankan ke unit ini. Tetap bisa dijual, ditagih, dibayar, dan masuk Laba Rugi lewat akun pendapatannya sendiri.",
};

export function ProductTypesSection({ token }: { token: string }) {
  const mayWrite = useMayWrite();
  const { toast } = useToast();
  const [rows, setRows] = useState<ProductType[]>([]);
  const [loading, setLoading] = useState(true);
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<ProductType | null>(null);
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [category, setCategory] = useState("property");
  const [account, setAccount] = useState("4-1000");
  const [busy, setBusy] = useState(false);
  // Hanya akun PENDAPATAN yang aktif — backend menolak selain ini
  // (fail-closed); dropdown membuat aturan itu terlihat sebelum disubmit.
  const [revenueAccounts, setRevenueAccounts] = useState<Account[]>([]);

  const load = useCallback(async () => {
    try {
      setRows(await fetchProductTypes(token));
    } catch {
      setRows([]);
    } finally {
      setLoading(false);
    }
  }, [token]);
  useEffect(() => { void load(); }, [load]);

  useEffect(() => {
    void (async () => {
      try {
        const all = await fetchAccounts(token);
        // 4-2000 (Pendapatan Luar Usaha) & 4-2100 (Pendapatan Booking) punya peran
        // khusus di Laba Rugi — bukan akun katalog produk (lihat ValidateRevenueAccount
        // backend, yang fail-closed menolak keduanya juga).
        setRevenueAccounts(
          all.filter((a) => a.type === "revenue" && a.is_active && a.code !== "4-2000" && a.code !== "4-2100"),
        );
      } catch {
        setRevenueAccounts([]);
      }
    })();
  }, [token]);

  function openCreate() {
    // Default = 4-1000 bila ada di COA; kalau tidak, akun pendapatan aktif
    // pertama. Tidak pernah mengisi kode yang tidak ada (server akan menolak).
    const preferred =
      revenueAccounts.find((a) => a.code === "4-1000")?.code ??
      revenueAccounts[0]?.code ??
      "";
    setEditing(null); setCode(""); setName(""); setCategory("property"); setAccount(preferred);
    setModalOpen(true);
  }
  function openEdit(p: ProductType) {
    setEditing(p); setCode(p.code); setName(p.name); setCategory(p.category); setAccount(p.revenue_account_code);
    setModalOpen(true);
  }

  async function handleSubmit() {
    setBusy(true);
    try {
      if (editing) {
        await updateProductType(token, editing.id, { name, revenue_account_code: account });
        toast(`Produk "${name}" diperbarui.`, "success");
      } else {
        await createProductType(token, { code, name, category, revenue_account_code: account });
        toast(`Produk "${name}" ditambahkan.`, "success");
      }
      setModalOpen(false);
      await load();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal menyimpan produk", "error");
    } finally {
      setBusy(false);
    }
  }

  async function toggleActive(p: ProductType) {
    try {
      await updateProductType(token, p.id, { is_active: !p.is_active });
      await load();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal mengubah status", "error");
    }
  }

  return (
    <Card>
      <div className="mb-3 flex items-center justify-between">
        <div>
          <p className="text-sm font-semibold">Katalog Produk</p>
          <p className="text-xs text-text-secondary">
            Jenis produk yang dijual + akun pendapatan masing-masing (jurnal BAST mengikuti mapping ini).
          </p>
        </div>
        {mayWrite && <Button size="sm" onClick={openCreate}>+ Produk</Button>}
      </div>
      {loading ? (
        <p className="py-4 text-sm text-text-secondary animate-pulse">Memuat…</p>
      ) : rows.length === 0 ? (
        <EmptyState title="Belum ada produk" description="Tambahkan jenis produk yang dijual agar unit baru bisa dibuat. Setiap produk menentukan akun pendapatan yang dipakai saat BAST." />
      ) : (
        <div className="divide-y divide-border">
          {rows.map((p) => (
            <div key={p.id} className="flex items-center justify-between gap-3 py-2.5">
              <div>
                <p className="text-sm font-medium">
                  {p.name} <span className="text-xs text-text-tertiary">({p.code})</span>
                  {!p.is_active && <Badge variant="neutral">nonaktif</Badge>}
                </p>
                <p className="text-xs text-text-secondary">
                  {CATEGORY_LABEL[p.category] ?? p.category} · Akun pendapatan: <span className="font-mono">{p.revenue_account_code}</span>
                </p>
              </div>
              {mayWrite && (
                <div className="flex gap-2">
                  <Button size="sm" variant="ghost" onClick={() => openEdit(p)}>Ubah</Button>
                  <Button size="sm" variant="ghost" onClick={() => void toggleActive(p)}>
                    {p.is_active ? "Nonaktifkan" : "Aktifkan"}
                  </Button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      <Modal
        open={modalOpen}
        onClose={() => setModalOpen(false)}
        title={editing ? `Ubah Produk — ${editing.code}` : "Tambah Produk"}
        size="md"
        footer={
          <>
            <Button variant="ghost" onClick={() => setModalOpen(false)} disabled={busy}>Batal</Button>
            <Button
              onClick={handleSubmit}
              loading={busy}
              disabled={busy || !name.trim() || !account.trim() || (!editing && !code.trim())}
            >
              Simpan
            </Button>
          </>
        }
      >
        <FormGrid>
          {!editing && (
            <Input label="Kode" value={code} onChange={(e) => setCode(e.target.value)} placeholder="kode singkat, huruf kecil" required />
          )}
          <Input label="Nama" value={name} onChange={(e) => setName(e.target.value)} placeholder="nama produk" required />
          {!editing ? (
            <>
              <Select label="Kategori" value={category} onChange={(e) => setCategory(e.target.value)}>
                <option value="property">{CATEGORY_LABEL.property}</option>
                <option value="non_property">{CATEGORY_LABEL.non_property}</option>
              </Select>
              <FormFull>
                <p className="-mt-1 text-[11px] text-text-tertiary">
                  {CATEGORY_HINT[category]} Kategori <strong>tidak bisa diubah</strong> setelah
                  produk dipakai bertransaksi — pilih dengan benar sekarang.
                </p>
              </FormFull>
            </>
          ) : (
            <FormFull>
              <div className="rounded-lg border border-border-subtle bg-bg px-3 py-2 text-[11px] text-text-secondary">
                Kategori: <strong>{CATEGORY_LABEL[editing.category] ?? editing.category}</strong> (permanen).
                {" "}{CATEGORY_HINT[editing.category]}
              </div>
            </FormFull>
          )}

          {revenueAccounts.length === 0 ? (
            <FormFull>
              <div className="rounded-lg border border-warning/40 bg-warning-bg px-3 py-2 text-xs text-text-secondary">
                Tidak ada akun <strong>Pendapatan</strong> aktif di COA. Buat akun 4-xxxx
                terlebih dahulu di menu Akun — produk tanpa akun pendapatan yang sah
                tidak bisa dipakai bertransaksi.
              </div>
            </FormFull>
          ) : (
            <FormFull>
              <Select label="Akun Pendapatan (COA)" value={account} onChange={(e) => setAccount(e.target.value)}>
                {/* Akun lama yang kini nonaktif tetap ditampilkan agar mapping
                    existing tidak "hilang" diam-diam saat produk diubah. */}
                {!revenueAccounts.some((a) => a.code === account) && account && (
                  <option value={account}>{account} — (akun saat ini, nonaktif/di luar daftar)</option>
                )}
                {revenueAccounts.map((a) => (
                  <option key={a.id} value={a.code}>{a.code} — {a.name}</option>
                ))}
              </Select>
            </FormFull>
          )}
          <FormFull>
            <p className="text-[11px] text-text-tertiary">
              Saat serah terima unit produk ini, pendapatan dikreditkan ke akun di atas.
              Hanya akun bertipe <strong>Pendapatan</strong> yang boleh dipilih — akun
              Aset/Kewajiban/Ekuitas/Beban akan ditolak server.
            </p>
          </FormFull>
        </FormGrid>
      </Modal>
    </Card>
  );
}
