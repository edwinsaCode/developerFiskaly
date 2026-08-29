"use client";

// W-2 — Penomoran Dokumen. Satu mesin untuk SELURUH dokumen perusahaan.
//
// Yang berubah bagi admin: bentuk nomor kwitansi/invoice dulu adalah konstanta
// di dalam program — mengubah prefix berarti menunggu rilis. Sekarang itu data,
// dan layar ini tempatnya.
//
// Yang TIDAK ada di layar ini, dan sengaja: mengubah nomor dokumen yang sudah
// terbit, dan menyetel ulang penghitung. Registry dokumen append-only, dan
// menurunkan penghitung berarti menerbitkan nomor kembar.

import { useCallback, useEffect, useMemo, useState } from "react";
import { Card } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { Modal } from "@/components/ui/Modal";
import { Input, Select } from "@/components/ui/Input";
import { FormGrid, FormFull } from "@/components/ui/Form";
import { EmptyState } from "@/components/ui/EmptyState";
import { useToast } from "@/components/ui/Toast";
import { ApiError } from "@/lib/api/client";
import { useMayWrite } from "@/lib/hooks/usePermissions";
import {
  fetchDocumentTypes,
  fetchDocumentPreview,
  fetchDocuments,
  fetchDocumentTypeHistory,
  createDocumentType,
  updateDocumentType,
  renderNumber,
  type DocumentType,
  type DocumentPreview,
  type DocumentRow,
  type DocumentMasterChange,
  type ResetPolicy,
} from "@/lib/api/document";

const DEFAULT_FORMAT = "{prefix}/{year}/{seq}";

function fmtDate(iso: string): string {
  const d = new Date(iso);
  return isNaN(d.getTime())
    ? iso
    : d.toLocaleDateString("id-ID", { day: "2-digit", month: "short", year: "numeric" });
}

function fmtRupiah(raw: string): string {
  const n = Number(raw);
  return isNaN(n) ? raw : `Rp ${Math.round(n).toLocaleString("id-ID")}`;
}

function describeChange(h: DocumentMasterChange): string {
  if (h.field === "created") return `dibuat — prefix ${h.new_value}`;
  if (h.field === "is_active") return h.new_value === "true" ? "diaktifkan kembali" : "dinonaktifkan";
  if (h.field === "prefix") return `prefix "${h.old_value}" → "${h.new_value}"`;
  if (h.field === "number_format") return `format "${h.old_value}" → "${h.new_value}"`;
  if (h.field === "padding") return `jumlah digit ${h.old_value} → ${h.new_value}`;
  if (h.field === "reset_policy") return `kebijakan reset ${h.old_value} → ${h.new_value}`;
  if (h.field === "name") return `nama "${h.old_value}" → "${h.new_value}"`;
  return `${h.field}: ${h.old_value} → ${h.new_value}`;
}

function resetLabel(p: ResetPolicy): string {
  return p === "never" ? "tidak pernah reset" : "reset tahunan";
}

interface FormState {
  code: string;
  name: string;
  prefix: string;
  format: string;
  padding: number;
  reset: ResetPolicy;
}

const emptyForm: FormState = {
  code: "",
  name: "",
  prefix: "",
  format: DEFAULT_FORMAT,
  padding: 6,
  reset: "yearly",
};

export function DocumentNumberingSection({ token }: { token: string }) {
  const mayWrite = useMayWrite();
  const { toast } = useToast();
  const [types, setTypes] = useState<DocumentType[]>([]);
  const [preview, setPreview] = useState<DocumentPreview[]>([]);
  const [docs, setDocs] = useState<DocumentRow[]>([]);
  const [history, setHistory] = useState<DocumentMasterChange[]>([]);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);

  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<DocumentType | null>(null);
  const [form, setForm] = useState<FormState>(emptyForm);
  const [busy, setBusy] = useState(false);

  const [filterType, setFilterType] = useState("");
  const [filterYear, setFilterYear] = useState("");

  const previewOf = useCallback(
    (code: string) => preview.find((p) => p.code === code),
    [preview],
  );

  const load = useCallback(async () => {
    try {
      const [t, p, h] = await Promise.all([
        fetchDocumentTypes(token),
        fetchDocumentPreview(token).catch(() => [] as DocumentPreview[]),
        fetchDocumentTypeHistory(token, 20).catch(() => [] as DocumentMasterChange[]),
      ]);
      setTypes(t);
      setPreview(p);
      setHistory(h);
      setFailed(false);
    } catch {
      setTypes([]);
      setFailed(true);
    } finally {
      setLoading(false);
    }
  }, [token]);
  useEffect(() => { void load(); }, [load]);

  const loadDocs = useCallback(async () => {
    try {
      setDocs(
        await fetchDocuments(token, {
          type: filterType || undefined,
          year: filterYear ? Number(filterYear) : undefined,
          limit: 50,
        }),
      );
    } catch {
      setDocs([]);
    }
  }, [token, filterType, filterYear]);
  useEffect(() => { void loadDocs(); }, [loadDocs]);

  // Tahun yang benar-benar punya dokumen — bukan rentang tahun karangan.
  const years = useMemo(() => {
    const s = new Set<number>(preview.map((p) => p.fiscal_year).filter((y) => y > 0));
    docs.forEach((d) => { if (d.fiscal_year > 0) s.add(d.fiscal_year); });
    return [...s].sort((a, b) => b - a);
  }, [preview, docs]);

  const nameByCode = useMemo(() => {
    const m = new Map<string, string>();
    types.forEach((t) => m.set(t.code, t.name));
    return m;
  }, [types]);

  // Pratinjau modal: nomor berikutnya menurut penghitung yang BERJALAN, bukan
  // selalu 1 — supaya admin melihat bentuk nomor yang sungguh akan terbit.
  const formPreview = useMemo(() => {
    const seq = editing ? (previewOf(editing.code)?.last_val ?? 0) + 1 : 1;
    return renderNumber(
      { prefix: form.prefix, number_format: form.format, padding: form.padding },
      seq,
    );
  }, [form, editing, previewOf]);

  function openCreate() {
    setEditing(null);
    setForm(emptyForm);
    setModalOpen(true);
  }

  function openEdit(t: DocumentType) {
    setEditing(t);
    setForm({
      code: t.code,
      name: t.name,
      prefix: t.prefix,
      format: t.number_format || DEFAULT_FORMAT,
      padding: t.padding || 6,
      reset: t.reset_policy,
    });
    setModalOpen(true);
  }

  async function handleSubmit() {
    setBusy(true);
    try {
      if (editing) {
        await updateDocumentType(token, editing.id, {
          name: form.name,
          prefix: form.prefix,
          number_format: form.format,
          reset_policy: form.reset,
          padding: form.padding,
        });
        toast(`Penomoran "${form.name}" diperbarui.`, "success");
      } else {
        await createDocumentType(token, {
          code: form.code,
          name: form.name,
          prefix: form.prefix,
          number_format: form.format,
          reset_policy: form.reset,
          padding: form.padding,
        });
        toast(`Jenis dokumen "${form.name}" ditambahkan.`, "success");
      }
      setModalOpen(false);
      await load();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal menyimpan jenis dokumen", "error");
    } finally {
      setBusy(false);
    }
  }

  async function toggleActive(t: DocumentType) {
    try {
      await updateDocumentType(token, t.id, { is_active: !t.is_active });
      await load();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal mengubah status", "error");
    }
  }

  return (
    <div className="space-y-4">
      <Card>
        <div className="mb-3 flex items-center justify-between">
          <div>
            <p className="text-sm font-semibold">Penomoran Dokumen</p>
            <p className="text-xs text-text-secondary">
              Kwitansi, invoice, dan memo transfer keluar dari <strong>satu</strong> mesin
              penomoran. Yang membedakan hanya konfigurasi di bawah ini.
            </p>
          </div>
          {mayWrite && <Button size="sm" onClick={openCreate}>+ Jenis Dokumen</Button>}
        </div>

        {loading ? (
          <p className="py-4 text-sm text-text-secondary animate-pulse">Memuat…</p>
        ) : types.length === 0 ? (
          <EmptyState
            title={failed ? "Konfigurasi penomoran tidak terbaca" : "Belum ada jenis dokumen"}
            description="Tanpa jenis dokumen, kwitansi dan invoice tidak bisa terbit sama sekali — sistem menolak menerbitkan nomor yang tidak dikonfigurasi (fail-closed). Tambahkan minimal jenis kwitansi pembayaran dan invoice."
            action={mayWrite ? <Button size="sm" onClick={openCreate}>+ Jenis Dokumen</Button> : undefined}
          />
        ) : (
          <div className="divide-y divide-border">
            {types.map((t) => {
              const p = previewOf(t.code);
              return (
                <div key={t.id} className="flex items-start justify-between gap-3 py-3">
                  <div className="min-w-0">
                    <p className="text-sm font-medium">
                      {t.name}{" "}
                      {t.is_system && <Badge variant="accent">inti</Badge>}{" "}
                      {!t.is_active && <Badge variant="neutral">nonaktif</Badge>}
                    </p>
                    <p className="text-xs text-text-secondary">
                      <span className="font-mono">{t.code}</span> · prefix{" "}
                      <span className="font-mono">{t.prefix}</span> · {resetLabel(t.reset_policy)} ·{" "}
                      {t.padding} digit
                    </p>
                    {p && (
                      <p className="mt-1 text-xs text-text-secondary">
                        {p.fiscal_year > 0 ? `Tahun ${p.fiscal_year}` : "Seri berjalan"} ·{" "}
                        {p.issued} dokumen terbit · berikutnya{" "}
                        <span className="font-mono font-semibold text-text-primary">
                          {p.next_number || "—"}
                        </span>
                      </p>
                    )}
                  </div>
                  {mayWrite && (
                    <div className="flex shrink-0 gap-2">
                      <Button size="sm" variant="ghost" onClick={() => openEdit(t)}>Ubah</Button>
                      {/* Jenis inti tidak boleh dimatikan — server menolaknya, dan
                          tombolnya pun tidak ditawarkan agar tidak menjebak admin. */}
                      {!t.is_system && (
                        <Button size="sm" variant="ghost" onClick={() => void toggleActive(t)}>
                          {t.is_active ? "Nonaktifkan" : "Aktifkan"}
                        </Button>
                      )}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}

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
      </Card>

      {/* Registry — bukti nomor mana yang sudah terpakai. Read-only selamanya. */}
      <Card>
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <div>
            <p className="text-sm font-semibold">Dokumen Terbit</p>
            <p className="text-xs text-text-secondary">
              Catatan setiap nomor yang pernah terbit. Tidak bisa diubah maupun dipakai ulang.
            </p>
          </div>
          <div className="flex gap-2">
            <Select value={filterType} onChange={(e) => setFilterType(e.target.value)}>
              <option value="">Semua jenis</option>
              {types.map((t) => (
                <option key={t.id} value={t.code}>{t.name}</option>
              ))}
            </Select>
            <Select value={filterYear} onChange={(e) => setFilterYear(e.target.value)}>
              <option value="">Semua tahun</option>
              {years.map((y) => (
                <option key={y} value={String(y)}>{y}</option>
              ))}
            </Select>
          </div>
        </div>

        {docs.length === 0 ? (
          <p className="py-3 text-sm text-text-secondary">
            Belum ada dokumen terbit untuk filter ini.
          </p>
        ) : (
          <div className="divide-y divide-border">
            {docs.map((d) => (
              <div key={d.id} className="flex items-center justify-between gap-3 py-2">
                <div className="min-w-0">
                  <p className="font-mono text-sm">{d.number}</p>
                  <p className="text-xs text-text-secondary">
                    {nameByCode.get(d.document_type_code) ?? d.document_type_code} ·{" "}
                    {fmtDate(d.issued_at)}
                  </p>
                </div>
                <p className="shrink-0 text-sm tabular-nums">{fmtRupiah(d.amount)}</p>
              </div>
            ))}
          </div>
        )}
      </Card>

      <Modal
        open={modalOpen}
        onClose={() => setModalOpen(false)}
        title={editing ? `Ubah Penomoran — ${editing.name}` : "Tambah Jenis Dokumen"}
        size="md"
        footer={
          <>
            <Button variant="ghost" onClick={() => setModalOpen(false)} disabled={busy}>Batal</Button>
            <Button
              onClick={handleSubmit}
              loading={busy}
              disabled={
                busy ||
                !form.name.trim() ||
                !form.prefix.trim() ||
                !formPreview ||
                (!editing && !form.code.trim())
              }
            >
              Simpan
            </Button>
          </>
        }
      >
        <FormGrid>
          {!editing ? (
            <>
              <Input
                label="Kode"
                value={form.code}
                onChange={(e) => setForm({ ...form, code: e.target.value })}
                placeholder="mis. cash_voucher"
                required
              />
              <Input
                label="Nama"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="mis. Bukti Kas Keluar"
                required
              />
              <FormFull>
                <p className="-mt-1 text-[11px] text-text-tertiary">
                  Kode: huruf kecil tanpa spasi. Kode inilah yang dipakai sistem saat meminta nomor —
                  <strong> tidak bisa diubah</strong> setelah dipakai.
                </p>
              </FormFull>
            </>
          ) : (
            <>
              <div className="flex items-center rounded-lg border border-border-subtle bg-bg px-3 py-2 text-[11px] text-text-secondary sm:mt-6">
                Kode: <strong className="font-mono mx-1">{editing.code}</strong> (permanen — modul lain
                meminta nomor memakai kode ini).
              </div>
              <Input
                label="Nama"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="mis. Bukti Kas Keluar"
                required
              />
            </>
          )}

          <Input
            label="Prefix"
            value={form.prefix}
            onChange={(e) => setForm({ ...form, prefix: e.target.value.toUpperCase() })}
            placeholder="BKK"
            required
          />
          <Input
            label="Jumlah digit"
            type="number"
            min={1}
            max={12}
            value={String(form.padding)}
            onChange={(e) => setForm({ ...form, padding: Number(e.target.value) || 0 })}
          />

          <Input
            label="Format nomor"
            value={form.format}
            onChange={(e) => setForm({ ...form, format: e.target.value })}
            placeholder={DEFAULT_FORMAT}
          />
          <Select
            label="Kebijakan reset"
            value={form.reset}
            onChange={(e) => setForm({ ...form, reset: e.target.value as ResetPolicy })}
          >
            <option value="yearly">Tahunan — kembali ke 1 setiap ganti tahun</option>
            <option value="never">Tidak pernah — nomor berjalan terus</option>
          </Select>
          <FormFull>
            <p className="-mt-1 text-[11px] text-text-tertiary">
              Placeholder: <span className="font-mono">{"{prefix}"}</span>{" "}
              <span className="font-mono">{"{year}"}</span>{" "}
              <span className="font-mono">{"{month}"}</span>{" "}
              <span className="font-mono">{"{seq}"}</span>.{" "}
              <span className="font-mono">{"{seq}"}</span> wajib ada — tanpa itu semua dokumen
              akan bernomor sama.
            </p>
          </FormFull>

          <FormFull>
            <div className="rounded-lg border border-border-subtle bg-bg px-3 py-2">
              <p className="text-[11px] text-text-tertiary">Pratinjau nomor berikutnya</p>
              {formPreview ? (
                <p className="font-mono text-sm font-semibold">{formPreview}</p>
              ) : (
                <p className="text-xs text-danger">
                  Format belum sah — pastikan memuat {"{seq}"} dan tidak ada placeholder asing.
                </p>
              )}
            </div>
          </FormFull>
          {editing && (
            <FormFull>
              <div className="rounded-lg border border-warning/40 bg-warning-bg/60 px-3 py-2 text-[11px] text-text-secondary">
                Perubahan berlaku untuk dokumen <strong>berikutnya</strong> saja. Dokumen yang sudah
                terbit tetap memakai nomor lamanya — tidak pernah diubah, tidak pernah dinomori ulang.
              </div>
            </FormFull>
          )}
        </FormGrid>
      </Modal>
    </div>
  );
}
