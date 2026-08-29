"use client";

// Catat tagihan vendor — dua langkah: ISI → PRATINJAU.
//
// Layar pratinjau TIDAK menghitung apa pun. Seluruh angka dan seluruh baris
// jurnal yang ditampilkannya datang dari /ap/invoices/preview, yang menjalankan
// validasi dan komposisi yang SAMA dengan penyimpanan. Kalau layar ini
// menghitung sendiri, ia akan menjadi definisi kedua aturan akuntansi — dan
// pratinjau yang bisa berbeda dari hasilnya membuat orang berhenti memeriksa.

import { useEffect, useMemo, useState, useTransition } from "react";
import { useRouter } from "next/navigation";

import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { ConfirmModal } from "@/components/ui/ConfirmModal";
import { Input, Select, Textarea } from "@/components/ui/Input";
import { RupiahInput } from "@/components/ui/RupiahInput";
import { Table, TableBody, TableHead, TableRow, Td, Th } from "@/components/ui/Table";
import { useToast } from "@/components/ui/Toast";
import { Rupiah, formatRupiah } from "@/components/format/Rupiah";
import type {
  APPreviewResult,
  BudgetItem,
  Project,
  Unit,
  Vendor,
} from "@/lib/types/api";
import {
  AP_OVERHEAD_CATEGORIES,
  AP_PROJECT_CATEGORIES,
  AP_PROJECT_TIERS,
  buildInvoiceBody,
  emptyInvoiceForm,
  emptyInvoiceLine,
  hasInvoiceErrors,
  sumRupiah,
  validateInvoice,
  type APScope,
  type InvoiceErrors,
  type InvoiceFormState,
  type InvoiceLineState,
} from "@/components/ap/apRules";
import {
  createInvoiceAction,
  fetchProjectContextAction,
  postInvoiceAction,
  previewInvoiceAction,
} from "@/app/(app)/accounting/hutang/actions";

interface Props {
  vendors: Vendor[];
  projects: Project[];
  today: string; // YYYY-MM-DD dari server — jangan pakai jam browser
}

const NO_ERRORS: InvoiceErrors = { fields: {}, lines: {} };

export function APInvoiceForm({ vendors, projects, today }: Props) {
  const router = useRouter();
  const { toast } = useToast();

  const [form, setForm] = useState<InvoiceFormState>(() => emptyInvoiceForm(today));
  const [errors, setErrors] = useState<InvoiceErrors>(NO_ERRORS);
  const [serverError, setServerError] = useState<string | null>(null);

  const [preview, setPreview] = useState<APPreviewResult | null>(null);
  const [previewing, startPreview] = useTransition();
  const [saving, startSave] = useTransition();
  const [confirmPost, setConfirmPost] = useState(false);

  const [units, setUnits] = useState<Unit[]>([]);
  const [budgetItems, setBudgetItems] = useState<BudgetItem[]>([]);
  const [planLabel, setPlanLabel] = useState("");
  const [loadingCtx, setLoadingCtx] = useState(false);

  const vendor = useMemo(
    () => vendors.find((v) => String(v.id) === form.vendor_id),
    [vendors, form.vendor_id],
  );

  const projectId = form.scope === "proyek" ? Number(form.project_id || 0) : 0;

  // Konteks proyek (unit + item RAB) dimuat saat proyeknya dipilih, bukan di
  // muka: memuat unit seluruh proyek sekaligus akan sia-sia.
  useEffect(() => {
    if (!projectId) {
      setUnits([]);
      setBudgetItems([]);
      setPlanLabel("");
      return;
    }
    let alive = true;
    setLoadingCtx(true);
    fetchProjectContextAction(projectId)
      .then((ctx) => {
        if (!alive) return;
        setUnits(ctx.units);
        setBudgetItems(ctx.budgetItems);
        setPlanLabel(ctx.budgetPlanLabel);
      })
      .finally(() => {
        if (alive) setLoadingCtx(false);
      });
    return () => {
      alive = false;
    };
  }, [projectId]);

  // Setiap perubahan isian membatalkan pratinjau lama. Pratinjau yang bertahan
  // setelah formnya berubah adalah janji atas angka yang tidak lagi dikirim.
  function touch() {
    setPreview(null);
    setServerError(null);
  }

  function set<K extends keyof InvoiceFormState>(k: K, v: InvoiceFormState[K]) {
    setForm((f) => ({ ...f, [k]: v }));
    touch();
  }

  function switchScope(scope: APScope) {
    setForm((f) => ({
      ...f,
      scope,
      project_id: scope === "overhead" ? "" : f.project_id,
      // Kategori & tier tidak bisa dibawa lintas scope: matriks tier × kategori
      // di backend menolak overhead+hard maupun direct+marketing.
      lines: f.lines.map(() => emptyInvoiceLine(scope)),
    }));
    setErrors(NO_ERRORS);
    touch();
  }

  function setLine(i: number, patch: Partial<InvoiceLineState>) {
    setForm((f) => ({
      ...f,
      lines: f.lines.map((ln, idx) => (idx === i ? { ...ln, ...patch } : ln)),
    }));
    touch();
  }

  function addLine() {
    setForm((f) => ({ ...f, lines: [...f.lines, emptyInvoiceLine(f.scope)] }));
    touch();
  }

  function removeLine(i: number) {
    setForm((f) => ({ ...f, lines: f.lines.filter((_, idx) => idx !== i) }));
    setErrors(NO_ERRORS);
    touch();
  }

  const lineTotal = sumRupiah(form.lines.map((l) => l.amount));

  function runPreview() {
    const e = validateInvoice(form, vendor?.is_pkp);
    setErrors(e);
    if (hasInvoiceErrors(e)) return;

    startPreview(async () => {
      setServerError(null);
      const res = await previewInvoiceAction(buildInvoiceBody(form));
      if (!res.ok) {
        setPreview(null);
        setServerError(res.error);
        return;
      }
      setPreview(res.data);
    });
  }

  function saveDraft() {
    startSave(async () => {
      setServerError(null);
      const res = await createInvoiceAction(buildInvoiceBody(form));
      if (!res.ok) {
        setServerError(res.error);
        return;
      }
      toast("Tagihan tersimpan sebagai draft", "success");
      router.push(`/accounting/hutang/${res.data.id}`);
    });
  }

  function saveAndPost() {
    startSave(async () => {
      setServerError(null);
      const created = await createInvoiceAction(buildInvoiceBody(form));
      if (!created.ok) {
        setConfirmPost(false);
        setServerError(created.error);
        return;
      }
      const posted = await postInvoiceAction(created.data.id);
      setConfirmPost(false);
      if (!posted.ok) {
        // Draftnya SUDAH tersimpan. Menyembunyikan itu akan membuat operator
        // mencatat ulang tagihan yang sama.
        toast(
          `Tagihan tersimpan sebagai draft, tetapi posting ditolak: ${posted.error}`,
          "warning",
        );
        router.push(`/accounting/hutang/${created.data.id}`);
        return;
      }
      toast("Tagihan diposting — kewajiban vendor sudah diakui", "success");
      router.push(`/accounting/hutang/${created.data.id}`);
    });
  }

  const categories = form.scope === "overhead" ? AP_OVERHEAD_CATEGORIES : AP_PROJECT_CATEGORIES;
  const busy = previewing || saving;

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>1. Isi Tagihan</CardTitle>
          <p className="mt-0.5 text-xs text-text-secondary">
            Nomor tagihan berasal dari vendor, bukan terbitan kita. Nilai PPN diambil
            dari faktur pajak vendor apa adanya — sistem tidak mengalikan tarif sendiri.
          </p>
        </CardHeader>

        {/* ── Scope ── */}
        <div className="mb-5">
          <span className="text-sm font-medium text-text-primary">Jenis Tagihan</span>
          <div className="mt-2 grid grid-cols-1 gap-3 sm:grid-cols-2">
            <ScopeCard
              active={form.scope === "proyek"}
              onClick={() => switchScope("proyek")}
              title="Tagihan Proyek"
              desc="Material, subkontraktor, perizinan. Dikapitalisasi ke persediaan dan terhitung sebagai realisasi RAB."
            />
            <ScopeCard
              active={form.scope === "overhead"}
              onClick={() => switchScope("overhead")}
              title="Tagihan Overhead"
              desc="Jasa & beban kantor tanpa proyek. Menjadi beban periode berjalan, tidak masuk HPP maupun RAB."
            />
          </div>
        </div>

        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Select
            label="Vendor"
            required
            value={form.vendor_id}
            onChange={(e) => set("vendor_id", e.target.value)}
            error={errors.fields.vendor_id}
          >
            <option value="">Pilih vendor...</option>
            {vendors
              .filter((v) => v.is_active)
              .map((v) => (
                <option key={v.id} value={String(v.id)}>
                  {v.name} {v.is_pkp ? "(PKP)" : "(Non-PKP)"}
                </option>
              ))}
          </Select>

          <div className="flex items-end pb-1">
            {vendor ? (
              <p className="text-[11px] text-text-tertiary">
                {vendor.is_pkp
                  ? "Vendor PKP — tagihan boleh membawa PPN Masukan bila fakturnya ada."
                  : "Vendor non-PKP — tagihan dari vendor ini tidak boleh membawa PPN Masukan."}
              </p>
            ) : vendors.filter((v) => v.is_active).length === 0 ? (
              <p className="text-[11px] text-danger">
                Belum ada vendor aktif. Tambahkan di Master Vendor lebih dulu.
              </p>
            ) : null}
          </div>

          {form.scope === "proyek" && (
            <>
              <Select
                label="Proyek"
                required
                value={form.project_id}
                onChange={(e) => set("project_id", e.target.value)}
                error={errors.fields.project_id}
              >
                <option value="">Pilih proyek...</option>
                {projects.map((p) => (
                  <option key={p.id} value={String(p.id)}>
                    {p.name}
                  </option>
                ))}
              </Select>
              <div className="flex items-end pb-1">
                <p className="text-[11px] text-text-tertiary">
                  {loadingCtx
                    ? "Memuat unit & RAB proyek…"
                    : planLabel
                      ? `RAB aktif: ${planLabel}. Baris boleh ditautkan ke item RAB.`
                      : projectId
                        ? "Proyek ini belum punya RAB aktif — baris tetap bisa dicatat, hanya tanpa tautan item."
                        : ""}
                </p>
              </div>
            </>
          )}

          <Input
            label="Nomor Tagihan Vendor"
            required
            placeholder="mis. INV/2026/0912"
            value={form.invoice_number}
            onChange={(e) => set("invoice_number", e.target.value)}
            error={errors.fields.invoice_number}
          />

          <Input
            label="Uraian"
            placeholder="Uraian singkat tagihan"
            value={form.description}
            onChange={(e) => set("description", e.target.value)}
          />

          <Input
            label="Tanggal Tagihan"
            required
            type="date"
            value={form.invoice_date}
            onChange={(e) => set("invoice_date", e.target.value)}
            error={errors.fields.invoice_date}
          />

          <Input
            label="Jatuh Tempo"
            required
            type="date"
            value={form.due_date}
            onChange={(e) => set("due_date", e.target.value)}
            error={errors.fields.due_date}
          />
        </div>

        {/* ── Baris biaya ── */}
        <div className="mt-6 rounded-lg border border-border bg-border-subtle/30 p-4">
          <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
            <div>
              <p className="text-sm font-semibold text-text-primary">Baris Biaya</p>
              <p className="mt-0.5 text-[11px] text-text-tertiary">
                Inilah nilai pekerjaan (DPP). Baris-baris ini yang menjadi biaya dan
                realisasi RAB — PPN tidak pernah ikut.
              </p>
            </div>
            <Button size="sm" variant="secondary" onClick={addLine} disabled={busy}>
              Tambah Baris
            </Button>
          </div>

          {errors.fields.lines && (
            <p className="mb-2 text-xs text-danger">{errors.fields.lines}</p>
          )}

          <div className="space-y-3">
            {form.lines.map((ln, i) => {
              const le = errors.lines[i] ?? {};
              return (
                <div
                  key={i}
                  className="rounded-lg border border-border bg-surface p-3"
                >
                  <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
                    <Select
                      label="Kategori"
                      required
                      value={ln.category}
                      onChange={(e) => setLine(i, { category: e.target.value })}
                      error={le.category}
                    >
                      <option value="">Pilih kategori...</option>
                      {categories.map((c) => (
                        <option key={c.value} value={c.value}>
                          {c.label}
                        </option>
                      ))}
                    </Select>

                    {form.scope === "proyek" ? (
                      <Select
                        label="Pembebanan"
                        required
                        value={ln.cost_tier}
                        onChange={(e) =>
                          setLine(i, {
                            cost_tier: e.target.value,
                            unit_id: e.target.value === "direct" ? ln.unit_id : "",
                          })
                        }
                        error={le.cost_tier}
                      >
                        {AP_PROJECT_TIERS.map((t) => (
                          <option key={t.value} value={t.value}>
                            {t.label}
                          </option>
                        ))}
                      </Select>
                    ) : (
                      <Input
                        label="Pembebanan"
                        value="Beban periode (overhead)"
                        readOnly
                        disabled
                      />
                    )}

                    <RupiahInput
                      label="Nominal (DPP)"
                      required
                      value={ln.amount}
                      onChange={(v) => setLine(i, { amount: v })}
                      error={le.amount}
                      showErrorWhenEmpty
                      hint="Rupiah bulat, tanpa sen"
                    />

                    {form.scope === "proyek" && (
                      <>
                        <div>
                          <Select
                            label="Unit"
                            value={ln.unit_id}
                            onChange={(e) => setLine(i, { unit_id: e.target.value })}
                            error={le.unit_id}
                            disabled={ln.cost_tier !== "direct" || units.length === 0}
                          >
                            <option value="">
                              {ln.cost_tier === "direct" ? "Pilih unit..." : "— Biaya bersama —"}
                            </option>
                            {units.map((u) => (
                              <option key={u.id} value={String(u.id)}>
                                {u.code} ({u.unit_type})
                              </option>
                            ))}
                          </Select>
                          {ln.cost_tier === "direct" && units.length === 0 && projectId > 0 && (
                            <p className="mt-1 text-[11px] text-danger">
                              Proyek ini belum punya unit — biaya langsung belum bisa dicatat.
                            </p>
                          )}
                        </div>

                        <Select
                          label="Item RAB (opsional)"
                          value={ln.budget_item_id}
                          onChange={(e) => setLine(i, { budget_item_id: e.target.value })}
                          disabled={budgetItems.length === 0}
                          hint={
                            budgetItems.length === 0
                              ? "Tidak ada RAB aktif untuk ditautkan"
                              : "Menautkan baris ke item RAB agar realisasi per item ikut terbaca"
                          }
                        >
                          <option value="">— Tanpa tautan item —</option>
                          {budgetItems.map((b) => (
                            <option key={b.id} value={String(b.id)}>
                              {b.subcategory || b.description || `Item #${b.id}`} —{" "}
                              {formatRupiah(b.budgeted_amount)}
                            </option>
                          ))}
                        </Select>
                      </>
                    )}

                    <Input
                      label="Uraian Baris"
                      value={ln.description}
                      onChange={(e) => setLine(i, { description: e.target.value })}
                      wrapperClassName={form.scope === "proyek" ? "md:col-span-2" : ""}
                    />
                  </div>

                  {form.lines.length > 1 && (
                    <div className="mt-2 flex justify-end">
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => removeLine(i)}
                        disabled={busy}
                      >
                        Hapus baris
                      </Button>
                    </div>
                  )}
                </div>
              );
            })}
          </div>

          <div className="mt-3 flex items-baseline justify-between border-t border-border pt-3">
            <span className="text-xs text-text-secondary">
              Jumlah baris (alat bantu isi — nilai yang mengikat dihitung backend)
            </span>
            <span className="font-mono text-sm font-semibold text-text-primary">
              {formatRupiah(lineTotal)}
            </span>
          </div>
        </div>

        {/* ── Pajak & DPP tertulis ── */}
        <div className="mt-6 grid grid-cols-1 gap-4 md:grid-cols-2">
          <RupiahInput
            label="PPN Masukan (dari faktur pajak)"
            value={form.ppn_amount}
            onChange={(v) => set("ppn_amount", v)}
            error={errors.fields.ppn_amount}
            hint="Kosongkan bila tagihan tidak ber-PPN. Nilai diambil dari faktur, bukan hasil kali tarif."
            disabled={vendor ? !vendor.is_pkp : false}
          />

          <Input
            label="Nomor Faktur Pajak"
            value={form.faktur_pajak_number}
            onChange={(e) => set("faktur_pajak_number", e.target.value)}
            error={errors.fields.faktur_pajak_number}
            disabled={vendor ? !vendor.is_pkp : false}
            hint="Wajib bila ada PPN — PPN Masukan tanpa faktur tidak bisa dikreditkan."
          />

          <RupiahInput
            label="DPP Tertulis di Tagihan (opsional)"
            value={form.dpp_amount}
            onChange={(v) => set("dpp_amount", v)}
            error={errors.fields.dpp_amount}
            hint="Bila diisi, backend menolak tagihan yang jumlah barisnya berbeda — selisihnya tidak dibulatkan."
          />
        </div>

        {serverError && (
          <div className="mt-5 rounded-lg border border-danger/30 bg-danger-bg/50 p-3">
            <p className="text-sm font-medium text-danger">Ditolak backend</p>
            <p className="mt-0.5 text-sm text-text-primary">{serverError}</p>
          </div>
        )}

        <div className="mt-5 flex justify-end">
          <Button onClick={runPreview} loading={previewing} disabled={saving}>
            Tinjau Tagihan
          </Button>
        </div>
      </Card>

      {/* ── Langkah 2: pratinjau ── */}
      {preview && (
        <APInvoicePreview
          preview={preview}
          form={form}
          vendorName={vendor?.name ?? ""}
          units={units}
          budgetItems={budgetItems}
          onBack={() => setPreview(null)}
          onSaveDraft={saveDraft}
          onPost={() => setConfirmPost(true)}
          saving={saving}
        />
      )}

      <ConfirmModal
        open={confirmPost}
        title="Posting tagihan vendor?"
        confirmLabel="Ya, akui kewajiban"
        loading={saving}
        onCancel={() => setConfirmPost(false)}
        onConfirm={saveAndPost}
      >
        <p>
          Tagihan akan disimpan lalu langsung diposting. Sejak saat itu saldo Hutang
          Usaha bertambah <strong>{formatRupiah(preview?.payable_amount ?? "0")}</strong>{" "}
          dan biayanya terbaca sebagai realisasi RAB.
        </p>
        <p className="mt-2">
          Jurnal yang sudah diposting tidak bisa diubah maupun dihapus — koreksinya
          hanya lewat pembalikan.
        </p>
      </ConfirmModal>
    </>
  );
}

// ── Pratinjau ────────────────────────────────────────────────────────────────

function APInvoicePreview({
  preview,
  form,
  vendorName,
  units,
  budgetItems,
  onBack,
  onSaveDraft,
  onPost,
  saving,
}: {
  preview: APPreviewResult;
  form: InvoiceFormState;
  vendorName: string;
  units: Unit[];
  budgetItems: BudgetItem[];
  onBack: () => void;
  onSaveDraft: () => void;
  onPost: () => void;
  saving: boolean;
}) {
  const unitCode = (id: string) => units.find((u) => String(u.id) === id)?.code ?? "";
  const itemLabel = (id: string) => {
    const b = budgetItems.find((x) => String(x.id) === id);
    if (!b) return "";
    return b.subcategory || b.description || `Item #${b.id}`;
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>2. Pratinjau — belum tersimpan</CardTitle>
        <p className="mt-0.5 text-xs text-text-secondary">
          Seluruh angka di bawah dihitung backend dengan jalur yang sama seperti saat
          menyimpan. Tidak ada yang dihitung ulang di layar ini.
        </p>
      </CardHeader>

      <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <Amount label="DPP (nilai pekerjaan)" value={preview.dpp_amount} />
        <Amount label="PPN Masukan" value={preview.ppn_amount} />
        <Amount label="Vendor" text={vendorName} />
        <Amount label="Total Hutang Vendor" value={preview.payable_amount} strong />
      </dl>

      {/* Baris biaya + dampak RAB */}
      <div className="mt-6">
        <p className="text-sm font-semibold text-text-primary">Baris Biaya & Dampak RAB</p>
        <div className="mt-2 overflow-hidden rounded-lg border border-border">
          <Table>
            <TableHead>
              <tr>
                <Th>Kategori</Th>
                <Th>Pembebanan</Th>
                <Th>Unit</Th>
                <Th>Item RAB</Th>
                <Th right>Nominal</Th>
              </tr>
            </TableHead>
            <TableBody>
              {form.lines.map((ln, i) => (
                <TableRow key={i}>
                  <Td>{ln.category}</Td>
                  <Td>{ln.cost_tier}</Td>
                  <Td>{ln.unit_id ? unitCode(ln.unit_id) : "—"}</Td>
                  <Td>{ln.budget_item_id ? itemLabel(ln.budget_item_id) : "—"}</Td>
                  <Td right mono>
                    <Rupiah value={ln.amount} colorSign={false} />
                  </Td>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        <p className="mt-2 text-[11px] text-text-tertiary">
          {form.scope === "overhead"
            ? "Tagihan overhead menjadi beban periode berjalan — tidak menambah realisasi RAB dan tidak masuk HPP."
            : "Baris di atas akan terbaca sebagai realisasi RAB proyek saat tagihan diposting. PPN Masukan tidak pernah ikut."}
        </p>
      </div>

      {/* Jurnal yang akan terbit */}
      <div className="mt-6">
        <p className="text-sm font-semibold text-text-primary">Jurnal yang Akan Terbentuk</p>
        <div className="mt-2 overflow-hidden rounded-lg border border-border">
          <Table>
            <TableHead>
              <tr>
                <Th>Akun</Th>
                <Th>Keterangan</Th>
                <Th>Tag</Th>
                <Th right>Debit</Th>
                <Th right>Kredit</Th>
              </tr>
            </TableHead>
            <TableBody>
              {preview.lines.map((l, i) => (
                <TableRow key={i}>
                  <Td>
                    <span className="font-mono text-xs text-text-secondary">
                      {l.account_code}
                    </span>{" "}
                    {l.account_name}
                  </Td>
                  <Td>{l.description}</Td>
                  <Td>
                    {l.project_id ? (
                      <Badge variant="accent">Proyek #{l.project_id}</Badge>
                    ) : (
                      <span className="text-text-tertiary">Tanpa tag proyek</span>
                    )}
                  </Td>
                  <Td right mono>
                    <Rupiah value={l.debit} colorSign={false} />
                  </Td>
                  <Td right mono>
                    <Rupiah value={l.credit} colorSign={false} />
                  </Td>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </div>

      <div className="mt-6 flex flex-wrap justify-end gap-3">
        <Button variant="ghost" onClick={onBack} disabled={saving}>
          Kembali Edit
        </Button>
        <Button variant="secondary" onClick={onSaveDraft} loading={saving}>
          Simpan Draft
        </Button>
        <Button onClick={onPost} disabled={saving}>
          Simpan &amp; Posting
        </Button>
      </div>
      <p className="mt-2 text-right text-[11px] text-text-tertiary">
        Draft belum menyentuh buku: tidak ada saldo hutang dan tidak ada realisasi RAB
        sampai tagihan diposting.
      </p>
    </Card>
  );
}

function Amount({
  label,
  value,
  text,
  strong,
}: {
  label: string;
  value?: string;
  text?: string;
  strong?: boolean;
}) {
  return (
    <div>
      <dt className="text-[11px] uppercase tracking-wide text-text-tertiary">{label}</dt>
      <dd
        className={`mt-0.5 font-mono text-sm ${
          strong ? "font-bold text-text-primary" : "text-text-primary"
        }`}
      >
        {text !== undefined ? (
          <span className="font-sans">{text || "—"}</span>
        ) : (
          <Rupiah value={value} colorSign={false} />
        )}
      </dd>
    </div>
  );
}

function ScopeCard({
  active,
  onClick,
  title,
  desc,
}: {
  active: boolean;
  onClick: () => void;
  title: string;
  desc: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={`rounded-lg border p-3 text-left transition-colors ${
        active
          ? "border-accent bg-accent-light"
          : "border-border bg-surface hover:border-accent/40"
      }`}
    >
      <span
        className={`block text-sm font-semibold ${active ? "text-accent" : "text-text-primary"}`}
      >
        {title}
      </span>
      <span className="mt-0.5 block text-[11px] text-text-secondary">{desc}</span>
    </button>
  );
}
