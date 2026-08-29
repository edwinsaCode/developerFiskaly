// Pure logika stepper Pembiayaan KPR — dipisah dari FinancingMilestones.tsx
// supaya bisa diuji lewat node:test (harness tidak transpile JSX/.tsx).

export const FINANCING_STEPS = [
  { state: "signed", label: "Kontrak" },
  { state: "submitted_to_bank", label: "Pengajuan" },
  { state: "bank_approved", label: "SP3K" },
  { state: "akad", label: "Akad Kredit" },
  { state: "disbursed", label: "Dana Cair" },
];

// dp_paid setara signed dalam progres visual (DP tidak mengubah alur bank).
// fully_paid/handed_over hanya tercapai lewat disbursed (lihat KPR policy di
// internal/scheme/policies.go) — tanpa mapping ini stepIndex balik -1 dan
// seluruh stepper tampil kosong walau Akad+pencairan sudah tuntas.
export function stepIndex(state: string): number {
  if (state === "dp_paid") return 0;
  if (state === "fully_paid" || state === "handed_over") return FINANCING_STEPS.length - 1;
  return FINANCING_STEPS.findIndex((s) => s.state === state);
}
