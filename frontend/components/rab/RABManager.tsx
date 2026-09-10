"use client";

import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { BudgetPlan, BudgetItem, BudgetCategory, ProjectPhase } from "@/lib/types/api";
import { CONSTRUCTION_SUBCATEGORIES, constructionSubcategoryLabel } from "@/lib/constants/constructionSubcategory";
import { Rupiah } from "@/components/format/Rupiah";
import { Persen } from "@/components/format/Persen";
import { Tanggal } from "@/components/format/Tanggal";
import { StatusBadge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Input, Select, Textarea } from "@/components/ui/Input";
import { FormGrid, FormFull } from "@/components/ui/Form";
import { RupiahInput, validateRupiah } from "@/components/ui/RupiahInput";
import { ConfirmModal } from "@/components/ui/ConfirmModal";
import { Modal } from "@/components/ui/Modal";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import {
  TableSearch,
  TablePagination,
  NoSearchResult,
  useTableView,
} from "@/components/ui/TableView";
import { EmptyState } from "@/components/ui/EmptyState";
import { useToast } from "@/components/ui/Toast";
import {
  createBudgetPlanAction,
  addBudgetItemAction,
  deleteBudgetItemAction,
  approveBudgetPlanAction,
} from "@/app/(app)/proyek/[id]/rab/actions";

// ── Konstanta kategori ────────────────────────────────────────────────────────

// RULE KLIEN FREEZE (2026-09-04): HPP hanya land + construction (Tanah +
// Hard Cost). soft dan operational (dahulu "financing") BUKAN lagi
// kapitalisasi walaupun tercatat di RAB — isBeban=true untuk keduanya, sama
// seperti marketing/other.
const BUDGET_CATEGORIES: { value: BudgetCategory; label: string; isBeban: boolean }[] = [
  { value: "land",         label: "Tanah",                                          isBeban: false },
  { value: "construction", label: "Konstruksi (Produksi Subsidi/Komersial, Sarana & Prasarana, Perizinan)", isBeban: false },
  { value: "soft",         label: "Biaya Lunak (Desain, Legal) — Beban, bukan HPP", isBeban: true  },
  { value: "operational",  label: "Operasional — Beban, bukan HPP",                 isBeban: true  },
  { value: "marketing",    label: "Pemasaran (Beban)",                              isBeban: true  },
  { value: "other",        label: "Lain-lain (Beban)",                              isBeban: true  },
];

function isBebanCategory(cat: BudgetCategory): boolean {
  return BUDGET_CATEGORIES.find(c => c.value === cat)?.isBeban ?? false;
}

function categoryLabel(cat: BudgetCategory): string {
  return BUDGET_CATEGORIES.find(c => c.value === cat)?.label ?? cat;
}

// Saran subkategori untuk kategori NON-Konstruksi — HANYA memandu penamaan
// agar konsisten. Kategori "construction" TIDAK lagi memakai saran bebas-teks:
// pilihannya WAJIB salah satu dari CONSTRUCTION_SUBCATEGORIES di atas (lihat
// AddItemForm), karena "Produksi" saja tidak cukup untuk menentukan HPP mana
// (Subsidi/Komersial) yang menerima alokasi biayanya.
const SUBCATEGORY_SUGGESTIONS: Partial<Record<BudgetCategory, string[]>> = {
  soft: ["Desain", "Legal"],
};

// ── Props ─────────────────────────────────────────────────────────────────────

interface PlanWithItems { plan: BudgetPlan; items: BudgetItem[] }

interface RABManagerProps {
  projectId: number;
  plansWithItems: PlanWithItems[];
  phases: ProjectPhase[];
  userEmail: string;
  canWrite: boolean;
}

// ── Komponen utama ────────────────────────────────────────────────────────────

export function RABManager({ projectId, plansWithItems, phases, userEmail, canWrite }: RABManagerProps) {
  const router = useRouter();
  const { toast } = useToast();
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [expandedPlanId, setExpandedPlanId] = useState<number | null>(
    plansWithItems.find(p => p.plan.status === "draft")?.plan.id ?? null
  );

  function refresh() { router.refresh(); }

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-base font-semibold text-text-primary">Versi RAB</h2>
          <p className="text-xs text-text-secondary mt-0.5">
            {plansWithItems.length} plan — hanya 1 yang bisa aktif sekaligus
          </p>
        </div>
        {canWrite && (
          <Button size="sm" onClick={() => setShowCreateModal(true)}>
            + Buat RAB Baru
          </Button>
        )}
      </div>

      {/* Daftar plan */}
      {plansWithItems.length === 0 ? (
        <Card>
          <EmptyState
            title="Belum ada RAB"
            description="Buat RAB pertama untuk mulai merencanakan anggaran proyek."
          />
        </Card>
      ) : (
        plansWithItems.map(({ plan, items }) => (
          <PlanCard
            key={plan.id}
            plan={plan}
            items={items}
            phases={phases}
            projectId={projectId}
            userEmail={userEmail}
            canWrite={canWrite}
            expanded={expandedPlanId === plan.id}
            onToggle={() => setExpandedPlanId(p => p === plan.id ? null : plan.id)}
            onMutate={refresh}
            toast={toast}
          />
        ))
      )}

      {/* Modal buat plan baru */}
      {canWrite && (
        <CreatePlanModal
          open={showCreateModal}
          projectId={projectId}
          phases={phases}
          onClose={() => setShowCreateModal(false)}
          onCreated={(plan) => {
            setShowCreateModal(false);
            setExpandedPlanId(plan.id);
            refresh();
            toast("RAB baru berhasil dibuat", "success");
          }}
          toast={toast}
        />
      )}
    </div>
  );
}

// ── PlanCard ──────────────────────────────────────────────────────────────────

interface PlanCardProps {
  plan: BudgetPlan;
  items: BudgetItem[];
  phases: ProjectPhase[];
  projectId: number;
  userEmail: string;
  canWrite: boolean;
  expanded: boolean;
  onToggle: () => void;
  onMutate: () => void;
  toast: (msg: string, variant?: "success" | "error" | "warning" | "info") => void;
}

function PlanCard({ plan, items, phases, projectId, userEmail, canWrite, expanded, onToggle, onMutate, toast }: PlanCardProps) {
  const [showApprove, setShowApprove] = useState(false);
  const [showAddItem, setShowAddItem] = useState(false);
  const [approvePending, startApprove] = useTransition();
  const [deletingId, setDeletingId] = useState<number | null>(null);

  // RAB terinci bisa ratusan baris; yang dicari orang biasanya satu pekerjaan
  // ("pondasi", "sertifikat") — bukan urutannya di daftar.
  const itemView = useTableView({
    rows: items,
    searchFields: (it) => [
      categoryLabel(it.category),
      it.subcategory,
      it.description,
      it.budgeted_amount,
    ],
    pageSize: 25,
  });

  const isDraft = plan.status === "draft";
  const phaseName = phases.find(p => p.id === plan.phase_id)?.name;

  async function handleApprove() {
    startApprove(async () => {
      try {
        await approveBudgetPlanAction(projectId, plan.id, userEmail);
        setShowApprove(false);
        onMutate();
        toast("RAB berhasil diapprove — plan aktif sebelumnya menjadi superseded", "success");
      } catch (e) {
        toast(`Gagal approve: ${e instanceof Error ? e.message : "Kesalahan server"}`, "error");
      }
    });
  }

  async function handleDeleteItem(itemId: number) {
    setDeletingId(itemId);
    try {
      await deleteBudgetItemAction(projectId, plan.id, itemId);
      onMutate();
      toast("Item berhasil dihapus", "success");
    } catch (e) {
      toast(`Gagal hapus: ${e instanceof Error ? e.message : "Kesalahan server"}`, "error");
    } finally {
      setDeletingId(null);
    }
  }

  return (
    <Card padding="none">
      {/* Header plan */}
      <div
        role="button"
        tabIndex={0}
        onClick={onToggle}
        onKeyDown={e => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onToggle();
          }
        }}
        className="w-full flex items-center justify-between p-4 text-left hover:bg-border-subtle/40 transition-colors rounded-lg cursor-pointer"
      >
        <div className="flex items-center gap-3 flex-wrap">
          <StatusBadge status={plan.status} />
          <span className="font-semibold text-sm text-text-primary">
            v{plan.version} — {plan.label}
          </span>
          {phaseName && (
            <span className="text-xs text-text-tertiary bg-border-subtle px-2 py-0.5 rounded">
              {phaseName}
            </span>
          )}
          {plan.approved_at && (
            <span className="text-xs text-text-tertiary">
              Diapprove <Tanggal value={plan.approved_at} /> oleh {plan.approved_by}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {isDraft && canWrite && (
            <Button
              size="sm"
              variant="ghost"
              onClick={e => { e.stopPropagation(); setShowApprove(true); }}
            >
              Approve
            </Button>
          )}
          <span className="text-text-tertiary text-sm">{expanded ? "▲" : "▼"}</span>
        </div>
      </div>

      {/* Body plan (expanded) */}
      {expanded && (
        <div className="border-t border-border">
          {/* Items table */}
          {items.length === 0 ? (
            <div className="px-4 py-8 text-center text-sm text-text-tertiary">
              Belum ada item — {isDraft && canWrite ? "tambahkan item di bawah" : "tidak ada anggaran"}
            </div>
          ) : (
            <>
            <TableSearch
              value={itemView.query}
              onChange={itemView.setQuery}
              placeholder="Cari kategori, subkategori, atau deskripsi…"
            />
            <Table>
              <TableHead>
                <TableRow>
                  <Th>Kategori</Th>
                  <Th>Subkategori</Th>
                  <Th>Deskripsi</Th>
                  <Th right>Anggaran</Th>
                  {isDraft && canWrite && <Th></Th>}
                </TableRow>
              </TableHead>
              <TableBody>
                {itemView.visible.length === 0 ? (
                  <NoSearchResult query={itemView.query} colSpan={isDraft && canWrite ? 5 : 4} />
                ) : itemView.visible.map(item => (
                  <TableRow key={item.id}>
                    <Td>
                      <span className={`text-xs font-medium px-1.5 py-0.5 rounded
                        ${isBebanCategory(item.category)
                          ? "bg-warning-bg text-warning"
                          : "bg-accent-light text-accent"}`}>
                        {categoryLabel(item.category)}
                      </span>
                    </Td>
                    <Td>
                      {item.subcategory
                        ? (item.category === "construction" ? constructionSubcategoryLabel(item.subcategory) : item.subcategory)
                        : <span className="text-text-tertiary">—</span>}
                    </Td>
                    <Td>{item.description || <span className="text-text-tertiary">—</span>}</Td>
                    <Td right><Rupiah value={item.budgeted_amount} /></Td>
                    {isDraft && canWrite && (
                      <Td>
                        <Button
                          size="sm"
                          variant="danger"
                          loading={deletingId === item.id}
                          onClick={() => handleDeleteItem(item.id)}
                        >
                          Hapus
                        </Button>
                      </Td>
                    )}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <TablePagination
              page={itemView.page}
              pages={itemView.pages}
              pageSize={itemView.pageSize}
              total={itemView.total}
              onPage={itemView.setPage}
            />
            </>
          )}

          {/* Tambah item (hanya draft + canWrite) */}
          {isDraft && canWrite && (
            <div className="p-4 border-t border-border-subtle">
              {showAddItem ? (
                <AddItemForm
                  projectId={projectId}
                  planId={plan.id}
                  onAdded={() => { setShowAddItem(false); onMutate(); toast("Item berhasil ditambahkan", "success"); }}
                  onCancel={() => setShowAddItem(false)}
                  toast={toast}
                />
              ) : (
                <Button size="sm" variant="secondary" onClick={() => setShowAddItem(true)}>
                  + Tambah Item
                </Button>
              )}
            </div>
          )}

          {/* Info read-only untuk active/superseded */}
          {!isDraft && (
            <div className="px-4 py-3 text-xs text-text-tertiary border-t border-border-subtle">
              Plan {plan.status === "active" ? "aktif" : "lama"} — buat draft baru untuk revisi
            </div>
          )}
        </div>
      )}

      {/* Confirm approve */}
      <ConfirmModal
        open={showApprove}
        title="Approve RAB?"
        confirmLabel="Approve"
        onConfirm={handleApprove}
        onCancel={() => setShowApprove(false)}
        loading={approvePending}
      >
        <p className="text-sm text-text-secondary">
          Plan aktif sebelumnya (jika ada) akan menjadi <strong>superseded</strong>.
          Hanya satu RAB yang bisa aktif per proyek/fase.
          <br /><br />
          Plan yang sudah di-approve <strong>tidak bisa diedit</strong>.
          Untuk revisi, buat draft baru.
        </p>
      </ConfirmModal>
    </Card>
  );
}

// ── AddItemForm ───────────────────────────────────────────────────────────────

interface AddItemFormProps {
  projectId: number;
  planId: number;
  onAdded: () => void;
  onCancel: () => void;
  toast: (msg: string, variant?: "success" | "error" | "warning" | "info") => void;
}

function AddItemForm({ projectId, planId, onAdded, onCancel, toast }: AddItemFormProps) {
  const [form, setForm] = useState({
    category: "" as BudgetCategory | "",
    subcategory: "",
    description: "",
    budgeted_amount: "",
  });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [saving, startSave] = useTransition();

  const isConstruction = form.category === "construction";

  function validate(): boolean {
    const e: Record<string, string> = {};
    if (!form.category) e.category = "Pilih kategori";
    if (isConstruction && !CONSTRUCTION_SUBCATEGORIES.some(s => s.value === form.subcategory)) {
      // UAT 2026-09-07: "Produksi" tunggal tidak cukup untuk menentukan HPP —
      // wajib pilih salah satu dari 4 subkategori kanonik.
      e.subcategory = "Pilih subkategori Konstruksi";
    }
    const amountErr = validateRupiah(form.budgeted_amount);
    if (amountErr) e.budgeted_amount = amountErr;
    setErrors(e);
    return Object.keys(e).length === 0;
  }

  function handleSubmit() {
    if (!validate()) return;
    startSave(async () => {
      try {
        await addBudgetItemAction(projectId, planId, {
          category: form.category as BudgetCategory,
          subcategory: form.subcategory || undefined,
          description: form.description || undefined,
          budgeted_amount: form.budgeted_amount,
        });
        onAdded();
      } catch (e) {
        toast(`Gagal tambah item: ${e instanceof Error ? e.message : "Kesalahan server"}`, "error");
      }
    });
  }

  return (
    <div className="grid grid-cols-1 md:grid-cols-4 gap-3 p-4 bg-border-subtle/30 rounded-lg border border-border-subtle">
      <Select
        label="Kategori"
        required
        value={form.category}
        onChange={e => {
          const category = e.target.value as BudgetCategory;
          // Ganti kategori → reset subkategori: nilai lama (mis. dari
          // Konstruksi) tidak relevan untuk kategori lain, dan sebaliknya.
          setForm(f => ({ ...f, category, subcategory: "" }));
        }}
        error={errors.category}
      >
        <option value="">Pilih...</option>
        {BUDGET_CATEGORIES.map(c => (
          <option key={c.value} value={c.value}>{c.label}</option>
        ))}
      </Select>
      {isConstruction ? (
        <Select
          label="Subkategori"
          required
          value={form.subcategory}
          onChange={e => setForm(f => ({ ...f, subcategory: e.target.value }))}
          error={errors.subcategory}
          hint="Produksi Subsidi dan Komersial dihitung terpisah — HPP masing-masing hanya jatuh ke unit dengan klasifikasi yang sama"
        >
          <option value="">Pilih...</option>
          {CONSTRUCTION_SUBCATEGORIES.map(s => (
            <option key={s.value} value={s.value}>{s.label}</option>
          ))}
        </Select>
      ) : (
        <>
          <Input
            label="Subkategori"
            placeholder="mis. Material"
            value={form.subcategory}
            onChange={e => setForm(f => ({ ...f, subcategory: e.target.value }))}
            list="subcategory-suggestions"
            hint={
              SUBCATEGORY_SUGGESTIONS[form.category as BudgetCategory]
                ? "Opsional — sekadar penamaan konsisten"
                : undefined
            }
          />
          <datalist id="subcategory-suggestions">
            {(SUBCATEGORY_SUGGESTIONS[form.category as BudgetCategory] ?? []).map(s => (
              <option key={s} value={s} />
            ))}
          </datalist>
        </>
      )}
      <Input
        label="Deskripsi"
        placeholder="Uraian singkat"
        value={form.description}
        onChange={e => setForm(f => ({ ...f, description: e.target.value }))}
      />
      <RupiahInput
        label="Anggaran"
        required
        value={form.budgeted_amount}
        onChange={v => setForm(f => ({ ...f, budgeted_amount: v }))}
        error={errors.budgeted_amount}
        hint="Rupiah bulat (tanpa sen)"
      />
      <div className="md:col-span-4 flex gap-2">
        <Button size="sm" onClick={handleSubmit} loading={saving}>Simpan Item</Button>
        <Button size="sm" variant="ghost" onClick={onCancel} disabled={saving}>Batal</Button>
      </div>
    </div>
  );
}

// ── CreatePlanModal ───────────────────────────────────────────────────────────

interface CreatePlanModalProps {
  open: boolean;
  projectId: number;
  phases: ProjectPhase[];
  onClose: () => void;
  onCreated: (plan: BudgetPlan) => void;
  toast: (msg: string, variant?: "success" | "error" | "warning" | "info") => void;
}

function CreatePlanModal({ open, projectId, phases, onClose, onCreated, toast }: CreatePlanModalProps) {
  const [form, setForm] = useState({ label: "", phase_id: "", notes: "" });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [saving, startSave] = useTransition();

  function validate(): boolean {
    const e: Record<string, string> = {};
    if (!form.label.trim()) e.label = "Label wajib diisi";
    setErrors(e);
    return Object.keys(e).length === 0;
  }

  function handleSubmit() {
    if (!validate()) return;
    startSave(async () => {
      try {
        const plan = await createBudgetPlanAction(projectId, {
          label: form.label.trim(),
          phase_id: form.phase_id ? parseInt(form.phase_id, 10) : null,
          notes: form.notes || undefined,
        });
        setForm({ label: "", phase_id: "", notes: "" });
        onCreated(plan);
      } catch (e) {
        toast(`Gagal buat RAB: ${e instanceof Error ? e.message : "Kesalahan server"}`, "error");
      }
    });
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Buat RAB Baru (Draft)"
      size="md"
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={saving}>Batal</Button>
          <Button onClick={handleSubmit} loading={saving}>Buat Draft</Button>
        </>
      }
    >
      <FormGrid>
        <Input
          label="Label RAB"
          required
          placeholder="mis. RAB Fase 1 v1"
          value={form.label}
          onChange={e => setForm(f => ({ ...f, label: e.target.value }))}
          error={errors.label}
        />
        {phases.length > 0 && (
          <Select
            label="Fase (opsional)"
            value={form.phase_id}
            onChange={e => setForm(f => ({ ...f, phase_id: e.target.value }))}
          >
            <option value="">— Semua Fase / Proyek —</option>
            {phases.map(p => (
              <option key={p.id} value={String(p.id)}>{p.name}</option>
            ))}
          </Select>
        )}
        <FormFull>
          <Textarea
            label="Catatan (opsional)"
            rows={3}
            placeholder="Catatan untuk RAB ini..."
            value={form.notes}
            onChange={e => setForm(f => ({ ...f, notes: e.target.value }))}
          />
        </FormFull>
        <FormFull>
          <p className="text-xs text-text-tertiary">
            RAB dibuat sebagai <strong>draft</strong>. Tambahkan item anggaran, lalu approve untuk mengaktifkan.
          </p>
        </FormFull>
      </FormGrid>
    </Modal>
  );
}

// ── RABvsRealisasiTable ───────────────────────────────────────────────────────
// Dipisah ke file sendiri tapi re-export di sini untuk kemudahan impor

export { RABvsRealisasiSection } from "./RABvsRealisasiSection";
