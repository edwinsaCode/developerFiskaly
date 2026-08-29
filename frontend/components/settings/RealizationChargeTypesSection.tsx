"use client";

// W-1 — master Jenis Biaya Realisasi: biaya yang ditagih ke customer atas nama
// pihak ketiga. Daftarnya milik admin sepenuhnya; program tidak mengenali kode
// apa pun secara khusus. Akun kewajiban adalah atribut tiap jenis biaya, jadi
// jenis yang berbeda boleh mendarat di akun yang berbeda.
//
// Rule klien FINAL 2026-08-06: SELURUHNYA titipan (kewajiban), tidak pernah
// menjadi pendapatan. Karena itu tidak ada kontrol "perlakuan" di UI ini —
// treatment adalah keputusan bisnis yang dikunci, bukan setelan admin.
//
// Yang BOLEH diatur admin sendiri (tanpa programmer): jenis biayanya apa saja,
// namanya apa, dan ke akun kewajiban mana uangnya mendarat.

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
  fetchChargeTypes,
  fetchChargeTypeHistory,
  createChargeType,
  updateChargeType,
  type RealizationChargeType,
  type MasterDataChange,
} from "@/lib/api/charge";
import { fetchAccounts } from "@/lib/api/ledger";
import type { Account } from "@/lib/types/api";

import { useMayWrite } from "@/lib/hooks/usePermissions";
function fmtDate(iso: string): string {
  const d = new Date(iso);
  return isNaN(d.getTime())
    ? iso
    : d.toLocaleDateString("id-ID", { day: "2-digit", month: "short", year: "numeric" });
}

// Audit ditulis backend dalam bentuk field/old/new mentah. Diterjemahkan di sini
// menjadi kalimat yang bisa dibaca admin — tanpa mengubah nilai yang tersimpan.
function describeChange(h: MasterDataChange): string {
  if (h.field === "created") return `dibuat — akun titipan ${h.new_value}`;
  if (h.field === "is_active") return h.new_value === "true" ? "diaktifkan kembali" : "dinonaktifkan";
  if (h.field === "deposit_account_code") return `akun titipan ${h.old_value} → ${h.new_value}`;
  if (h.field === "name") return `nama "${h.old_value}" → "${h.new_value}"`;
  return `${h.field}: ${h.old_value} → ${h.new_value}`;
}

export function RealizationChargeTypesSection({ token }: { token: string }) {
  const mayWrite = useMayWrite();
  const { toast } = useToast();
  const [rows, setRows] = useState<RealizationChargeType[]>([]);
  const [history, setHistory] = useState<MasterDataChange[]>([]);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<RealizationChargeType | null>(null);
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [account, setAccount] = useState("");
  const [busy, setBusy] = useState(false);
  // Hanya akun KEWAJIBAN aktif — backend menolak selain itu (fail-closed);
  // dropdown membuat aturan tersebut terlihat sebelum admin menekan Simpan.
  const [liabilityAccounts, setLiabilityAccounts] = useState<Account[]>([]);

  const load = useCallback(async () => {
    try {
      const [types, hist] = await Promise.all([
        fetchChargeTypes(token),
        fetchChargeTypeHistory(token, 20).catch(() => [] as MasterDataChange[]),
      ]);
      setRows(types);
      setHistory(hist);
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
        setLiabilityAccounts(all.filter((a) => a.type === "liability" && a.is_active));
      } catch {
        setLiabilityAccounts([]);
      }
    })();
  }, [token]);

  function openCreate() {
    // Default diambil dari DATA, bukan dari kode akun yang ditulis di program:
    // akun yang paling sering dipakai jenis biaya yang sudah ada. Bila master
    // masih kosong, jatuh ke akun kewajiban aktif pertama di COA. Tidak pernah
    // mengisi kode yang tidak ada — server akan menolaknya.
    const tally = new Map<string, number>();
    for (const t of rows) {
      if (liabilityAccounts.some((a) => a.code === t.deposit_account_code)) {
        tally.set(t.deposit_account_code, (tally.get(t.deposit_account_code) ?? 0) + 1);
      }
    }
    const mostUsed = [...tally.entries()].sort((a, b) => b[1] - a[1])[0]?.[0];
    const preferred = mostUsed ?? liabilityAccounts[0]?.code ?? "";
    setEditing(null); setCode(""); setName(""); setAccount(preferred);
    setModalOpen(true);
  }
  function openEdit(t: RealizationChargeType) {
    setEditing(t); setCode(t.code); setName(t.name); setAccount(t.deposit_account_code);
    setModalOpen(true);
  }

  async function handleSubmit() {
    setBusy(true);
    try {
      if (editing) {
        await updateChargeType(token, editing.id, { name, deposit_account_code: account });
        toast(`Jenis biaya "${name}" diperbarui.`, "success");
      } else {
        await createChargeType(token, { code, name, deposit_account_code: account });
        toast(`Jenis biaya "${name}" ditambahkan.`, "success");
      }
      setModalOpen(false);
      await load();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal menyimpan jenis biaya", "error");
    } finally {
      setBusy(false);
    }
  }

  async function toggleActive(t: RealizationChargeType) {
    try {
      await updateChargeType(token, t.id, { is_active: !t.is_active });
      await load();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal mengubah status", "error");
    }
  }

  return (
    <Card>
      <div className="mb-3 flex items-center justify-between">
        <div>
          <p className="text-sm font-semibold">Jenis Biaya Realisasi</p>
          <p className="text-xs text-text-secondary">
            Biaya yang ditagih ke customer atas nama pihak ketiga. Uangnya <strong>titipan</strong> (kewajiban) —
            tidak pernah menjadi pendapatan perusahaan.
          </p>
        </div>
        {mayWrite && <Button size="sm" onClick={openCreate}>+ Jenis Biaya</Button>}
      </div>

      {loading ? (
        <p className="py-4 text-sm text-text-secondary animate-pulse">Memuat…</p>
      ) : rows.length === 0 ? (
        <EmptyState
          title="Belum ada jenis biaya realisasi"
          description="Tambahkan jenis biaya pihak ketiga yang ditalangi customer, beserta akun kewajibannya, agar grup Biaya Realisasi bisa dibuat. Tanpa master ini, tagihan realisasi ditolak sistem."
          action={mayWrite ? <Button size="sm" onClick={openCreate}>+ Jenis Biaya</Button> : undefined}
        />
      ) : (
        <div className="divide-y divide-border">
          {rows.map((t) => (
            <div key={t.id} className="flex items-center justify-between gap-3 py-2.5">
              <div>
                <p className="text-sm font-medium">
                  {t.name} <span className="text-xs text-text-tertiary">({t.code})</span>{" "}
                  {!t.is_active && <Badge variant="neutral">nonaktif</Badge>}
                </p>
                <p className="text-xs text-text-secondary">
                  Titipan → akun <span className="font-mono">{t.deposit_account_code}</span>
                </p>
              </div>
              {mayWrite && (
                <div className="flex gap-2">
                  <Button size="sm" variant="ghost" onClick={() => openEdit(t)}>Ubah</Button>
                  <Button size="sm" variant="ghost" onClick={() => void toggleActive(t)}>
                    {t.is_active ? "Nonaktifkan" : "Aktifkan"}
                  </Button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {/* TD-8 — jejak audit. Mengubah akun titipan mengubah ke mana uang
          customer mendarat, jadi riwayatnya harus bisa dilihat admin sendiri. */}
      {history.length > 0 && (
        <div className="mt-4 border-t border-border pt-3">
          <button
            className="text-xs font-medium text-accent hover:underline"
            onClick={() => setHistoryOpen((v) => !v)}
          >
            {historyOpen ? "▾" : "▸"} Riwayat perubahan ({history.length})
          </button>
          {historyOpen && (
            <ul className="mt-2 space-y-1">
              {history.map((h) => (
                <li key={h.id} className="text-[11px] text-text-secondary">
                  <span className="text-text-tertiary">{fmtDate(h.created_at)}</span>{" "}
                  <span className="font-mono">{h.entity_code}</span> · {describeChange(h)}
                  {h.changed_by ? ` · oleh user #${h.changed_by}` : " · oleh sistem"}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      <Modal
        open={modalOpen}
        onClose={() => setModalOpen(false)}
        title={editing ? `Ubah Jenis Biaya — ${editing.code}` : "Tambah Jenis Biaya Realisasi"}
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
          {!editing ? (
            <>
              <Input label="Kode" value={code} onChange={(e) => setCode(e.target.value)} placeholder="mis. ipl" required />
              <Input label="Nama" value={name} onChange={(e) => setName(e.target.value)} placeholder="mis. Iuran Pengelolaan Lingkungan" required />
              <FormFull>
                <p className="-mt-1 text-[11px] text-text-tertiary">
                  Kode: huruf kecil tanpa spasi. Kode inilah yang menempel permanen pada tagihan —
                  <strong> tidak bisa diubah</strong> setelah dipakai.
                </p>
              </FormFull>
            </>
          ) : (
            <>
              <div className="flex items-center rounded-lg border border-border-subtle bg-bg px-3 py-2 text-[11px] text-text-secondary sm:mt-6">
                Kode: <strong className="font-mono ml-1">{editing.code}</strong>
                <span className="ml-1">(permanen — tagihan lama menunjuk kode ini).</span>
              </div>
              <Input label="Nama" value={name} onChange={(e) => setName(e.target.value)} placeholder="mis. Iuran Pengelolaan Lingkungan" required />
            </>
          )}

          {liabilityAccounts.length === 0 ? (
            <FormFull>
              <div className="rounded-lg border border-warning/40 bg-warning-bg px-3 py-2 text-xs text-text-secondary">
                Tidak ada akun <strong>Kewajiban</strong> aktif di COA. Buat akun 2-xxxx
                terlebih dahulu di menu Akun — titipan tanpa akun kewajiban yang sah
                tidak bisa dicatat.
              </div>
            </FormFull>
          ) : (
            <FormFull>
              <Select label="Akun Titipan (COA)" value={account} onChange={(e) => setAccount(e.target.value)}>
                {/* Akun lama yang kini nonaktif tetap ditampilkan agar mapping
                    existing tidak "hilang" diam-diam saat jenis biaya diubah. */}
                {!liabilityAccounts.some((a) => a.code === account) && account && (
                  <option value={account}>{account} — (akun saat ini, nonaktif/di luar daftar)</option>
                )}
                {liabilityAccounts.map((a) => (
                  <option key={a.id} value={a.code}>{a.code} — {a.name}</option>
                ))}
              </Select>
            </FormFull>
          )}

          <FormFull>
            <div className="rounded-lg border border-warning/40 bg-warning-bg/60 px-3 py-2 text-[11px] text-text-secondary">
              Saat customer membayar, uangnya dikreditkan ke akun di atas sebagai{" "}
              <strong>kewajiban</strong> — bukan pendapatan — dan baru berkurang ketika
              perusahaan membayarkannya ke vendor. Akun titipan adalah atribut{" "}
              <strong>tiap jenis biaya</strong>, jadi satu grup tagihan boleh memuat beberapa
              jenis yang mendarat di akun berbeda; sisa titipannya dirinci per akun.
            </div>
          </FormFull>
        </FormGrid>
      </Modal>
    </Card>
  );
}
