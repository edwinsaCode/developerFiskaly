"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Card } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Input, Select } from "@/components/ui/Input";
import { Badge } from "@/components/ui/Badge";
import { Table, TableHead, TableBody, TableRow, Th, Td } from "@/components/ui/Table";
import { Rupiah } from "@/components/format/Rupiah";
import { Tanggal } from "@/components/format/Tanggal";
import { useToast } from "@/components/ui/Toast";
import { LegacyReconciliationCard } from "@/components/accounting/LegacyReconciliationCard";
import { ApiError } from "@/lib/api/client";
import { fetchAccounts } from "@/lib/api/ledger";
import {
  commitLegacyBatch,
  discardLegacyBatch,
  legacyTemplateUrl,
  uploadLegacyBatch,
} from "@/lib/api/legacyar";
import type { LegacyCommitResult, LegacyPreview } from "@/lib/api/legacyar";
import type { Account } from "@/lib/types/api";

// Wizard impor piutang proyek lama — lima langkah, dan langkah ketiganya
// (Cocokkan dengan Buku Besar) adalah alasan wizard ini ada.
//
// Tanpa langkah itu, impor menjadi "masukkan angka ke tabel" dan tidak ada yang
// tahu apakah rincian yang baru masuk benar-benar menjelaskan saldo piutang di
// neraca. Selisih ditampilkan apa adanya dan TIDAK PERNAH ditutup sendiri oleh
// sistem: kalau memang perlu jurnal saldo awal, akun lawannya dipilih user dari
// bagan akun — menebak akun lawan berarti mengarang jurnal atas nama klien.

type Step = 1 | 2 | 3 | 4 | 5;

// Posisi piutang lama hampir selalu per akhir tahun buku terakhir, bukan hari
// ini. Default yang benar menghemat satu kesalahan yang mahal untuk diperbaiki
// setelah impor terlanjur dilakukan.
function defaultAsOf(): string {
  const y = new Date().getFullYear() - 1;
  return `${y}-12-31`;
}

/** Pilihan penyelesaian selisih. Sengaja tanpa nilai awal — user harus memilih. */
type DiffChoice = "" | "journal" | "accept";

const STEPS: { n: Step; label: string }[] = [
  { n: 1, label: "Berkas" },
  { n: 2, label: "Pratinjau" },
  { n: 3, label: "Cocokkan Buku Besar" },
  { n: 4, label: "Konfirmasi" },
  { n: 5, label: "Selesai" },
];

export function LegacyARImportWizard({ token }: { token: string }) {
  const router = useRouter();
  const { toast } = useToast();

  const [step, setStep] = useState<Step>(1);
  const [file, setFile] = useState<File | null>(null);
  const [asOf, setAsOf] = useState(defaultAsOf());
  const [controlAccount, setControlAccount] = useState("1-2000");
  const [notes, setNotes] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [preview, setPreview] = useState<LegacyPreview | null>(null);
  const [result, setResult] = useState<LegacyCommitResult | null>(null);

  const [choice, setChoice] = useState<DiffChoice>("");
  const [counterAccount, setCounterAccount] = useState("");
  const [openingDesc, setOpeningDesc] = useState("");
  const [skipReason, setSkipReason] = useState("");

  const [accounts, setAccounts] = useState<Account[]>([]);
  useEffect(() => {
    fetchAccounts(token)
      .then((a) => setAccounts(a.filter((x) => x.is_active)))
      .catch(() => setAccounts([]));
  }, [token]);

  async function doUpload() {
    if (!file) return;
    setBusy(true);
    setError(null);
    try {
      const p = await uploadLegacyBatch(token, {
        file,
        asOfDate: asOf,
        controlAccountCode: controlAccount || undefined,
        notes: notes || undefined,
      });
      setPreview(p);
      setStep(2);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Berkas gagal dibaca.");
    } finally {
      setBusy(false);
    }
  }

  async function doDiscard() {
    if (!preview) return;
    setBusy(true);
    try {
      await discardLegacyBatch(token, preview.batch.id);
    } catch {
      // Batch draft yang gagal dibuang bukan alasan menahan user di layar ini;
      // ia tidak pernah menjadi piutang dan tidak menyentuh buku besar.
    } finally {
      setPreview(null);
      setFile(null);
      setChoice("");
      setCounterAccount("");
      setSkipReason("");
      setBusy(false);
      setStep(1);
    }
  }

  async function doCommit() {
    if (!preview) return;
    setBusy(true);
    setError(null);
    try {
      const res = await commitLegacyBatch(token, preview.batch.id, {
        opening_counter_account_code: choice === "journal" ? counterAccount : undefined,
        opening_description: choice === "journal" ? openingDesc || undefined : undefined,
        skip_reason: choice === "accept" ? skipReason : undefined,
      });
      setResult(res);
      setStep(5);
      toast(`${res.imported_count} piutang proyek lama berhasil diimpor.`, "success");
      router.refresh();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Impor gagal.");
    } finally {
      setBusy(false);
    }
  }

  const rec = preview?.reconciliation;
  const hasErrors = (preview?.batch.error_count ?? 0) > 0;
  const matched = rec?.matched === true;
  const ledgerUnavailable = rec?.ledger_available === false;

  // Kalau saldo buku besar tidak terbaca, selisihnya tidak bisa ditafsirkan —
  // maka satu-satunya jalan maju yang jujur adalah mencatat alasan melanjutkan,
  // bukan menyilakan user "membuat jurnal penutup selisih" atas angka semu.
  const needsChoice = !matched;
  const choiceOk =
    !needsChoice ||
    (choice === "journal" && counterAccount !== "" && !ledgerUnavailable) ||
    (choice === "accept" && skipReason.trim().length >= 5);

  return (
    <div className="space-y-5">
      <Stepper current={step} />

      {error && (
        <div className="rounded border border-danger/40 bg-danger-bg/40 px-4 py-3 text-sm text-danger">
          {error}
        </div>
      )}

      {step === 1 && (
        <Card>
          <h2 className="text-sm font-semibold text-text-primary">1. Berkas</h2>
          <p className="mt-1 text-sm text-text-secondary">
            Gunakan template resmi agar kolomnya terbaca persis. Nominal boleh ditulis apa adanya
            (<span className="font-mono text-xs">150.000.000</span> atau{" "}
            <span className="font-mono text-xs">150000000</span>) — dibaca sebagai teks, bukan
            angka desimal, supaya tidak ada rupiah yang hilang karena pembulatan.
          </p>

          <div className="mt-4">
            <a href={legacyTemplateUrl()} download>
              <Button variant="secondary">⬇ Unduh Template Excel</Button>
            </a>
          </div>

          <div className="mt-5 grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="flex flex-col gap-1 sm:col-span-2">
              <label className="text-sm font-medium text-text-primary">
                Berkas Excel <span className="text-danger">*</span>
              </label>
              <input
                type="file"
                accept=".xlsx"
                onChange={(e) => setFile(e.target.files?.[0] ?? null)}
                className="text-sm text-text-secondary file:mr-3 file:rounded file:border file:border-border file:bg-surface file:px-3 file:py-1.5 file:text-sm file:text-text-primary"
              />
            </div>
            <Input
              label="Posisi Piutang (per tanggal)"
              type="date"
              value={asOf}
              onChange={(e) => setAsOf(e.target.value)}
              hint="Biasanya akhir tahun buku terakhir sebelum sistem ini dipakai."
            />
            <Input
              label="Akun Kontrol Piutang"
              value={controlAccount}
              onChange={(e) => setControlAccount(e.target.value)}
              hint="Default 1-2000 Piutang Customer — sama dengan piutang lain."
            />
            <Input
              label="Catatan Batch (opsional)"
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
              className="sm:col-span-2"
            />
          </div>

          <div className="mt-5 flex justify-end">
            <Button onClick={doUpload} disabled={!file || !asOf || busy} loading={busy}>
              Baca Berkas
            </Button>
          </div>
        </Card>
      )}

      {step === 2 && preview && (
        <Card padding="none">
          <div className="p-5">
            <h2 className="text-sm font-semibold text-text-primary">2. Pratinjau</h2>
            <p className="mt-1 text-sm text-text-secondary">
              Belum ada apa pun yang tersimpan. Periksa dulu — impor berjalan sekali jalan dan tidak
              bisa sebagian: satu baris bermasalah membatalkan seluruh berkas.
            </p>
            <div className="mt-4 flex flex-wrap gap-6 text-sm">
              <Stat label="Baris" value={String(preview.batch.row_count)} />
              <Stat label="Valid" value={String(preview.batch.valid_count)} />
              <Stat label="Peringatan" value={String(preview.batch.warning_count)} />
              <Stat label="Bermasalah" value={String(preview.batch.error_count)} danger={hasErrors} />
              <div>
                <p className="text-xs uppercase tracking-wide text-text-secondary">Total</p>
                <p className="mt-0.5 font-semibold text-text-primary">
                  <Rupiah value={preview.batch.total_amount} colorSign={false} />
                </p>
              </div>
            </div>
            {hasErrors && (
              <p className="mt-3 text-sm text-danger">
                Ada {preview.batch.error_count} baris yang tidak bisa dibaca. Perbaiki di Excel lalu
                unggah ulang — impor tidak akan melewatkan baris diam-diam.
              </p>
            )}
          </div>

          <Table>
            <TableHead>
              <tr>
                <Th>#</Th>
                <Th>Customer</Th>
                <Th>Proyek Lama</Th>
                <Th>Referensi</Th>
                <Th right>Nominal (asli)</Th>
                <Th right>Terbaca</Th>
                <Th>Jatuh Tempo</Th>
                <Th>Status</Th>
              </tr>
            </TableHead>
            <TableBody>
              {preview.rows.map((r) => (
                <TableRow key={r.id}>
                  <Td mono>{r.line_no}</Td>
                  <Td>{r.raw_customer_name || "—"}</Td>
                  <Td>{r.raw_source_label || "—"}</Td>
                  <Td mono>{r.raw_external_ref || "—"}</Td>
                  <Td right mono>
                    {r.raw_outstanding || "—"}
                  </Td>
                  <Td right>
                    <Rupiah value={r.amount} colorSign={false} />
                  </Td>
                  {/* Yang ditampilkan adalah tanggal hasil BACA, bukan stempel
                      waktu mentahnya — admin membandingkannya dengan isi Excel,
                      dan "2025-06-30T00:00:00+08:00" tidak bisa dibandingkan
                      dengan apa pun yang mereka ketik. */}
                  <Td>
                    {r.due_date ? <Tanggal value={r.due_date} /> : r.raw_due_date || "—"}
                  </Td>
                  <Td>
                    {r.parse_status === "ok" ? (
                      <Badge variant="success">OK</Badge>
                    ) : r.parse_status === "warning" ? (
                      <Badge variant="warning">
                        {r.issues?.[0]?.message ?? "Perlu diperiksa"}
                      </Badge>
                    ) : (
                      <Badge variant="danger">
                        {r.issues?.map((i) => `${i.column}: ${i.message}`).join("; ") ?? "Gagal"}
                      </Badge>
                    )}
                  </Td>
                </TableRow>
              ))}
            </TableBody>
          </Table>

          <div className="flex justify-between gap-2 border-t border-border p-4">
            <Button variant="secondary" onClick={doDiscard} disabled={busy}>
              Ganti Berkas
            </Button>
            <Button onClick={() => setStep(3)} disabled={hasErrors || preview.batch.valid_count === 0}>
              Lanjut — Cocokkan dengan Buku Besar
            </Button>
          </div>
        </Card>
      )}

      {step === 3 && preview && rec && (
        <Card>
          <h2 className="text-sm font-semibold text-text-primary">3. Cocokkan dengan Buku Besar</h2>
          <p className="mt-1 mb-4 text-sm text-text-secondary">
            Rincian yang akan diimpor dibandingkan dengan porsi <strong>saldo awal</strong> akun
            kontrol di buku besar. Keduanya harus menjelaskan angka yang sama.
          </p>

          <LegacyReconciliationCard
            rec={rec}
            title="Rincian yang akan diimpor vs Buku Besar"
            defaultOpen
          />

          {matched ? (
            <p className="mt-4 text-sm text-text-secondary">
              Cocok. Impor akan berjalan <strong>tanpa membuat jurnal apa pun</strong> — saldo di
              buku besar sudah benar, yang belum ada hanya rinciannya.
            </p>
          ) : (
            <div className="mt-4 space-y-3">
              <p className="text-sm text-text-primary font-medium">
                Ada selisih. Pilih bagaimana menyelesaikannya — sistem tidak akan memilih sendiri.
              </p>

              {!ledgerUnavailable && (
                <label className="flex gap-3 rounded border border-border p-3 cursor-pointer">
                  <input
                    type="radio"
                    name="diff"
                    className="mt-1"
                    checked={choice === "journal"}
                    onChange={() => setChoice("journal")}
                  />
                  <span className="text-sm">
                    <span className="font-medium text-text-primary">
                      Buat jurnal saldo awal untuk menutup selisih
                    </span>
                    <span className="block text-text-secondary">
                      Dr {rec.control_account_code} / Cr akun lawan pilihan Anda. Dipakai bila
                      saldo awal piutang memang belum pernah dimasukkan ke buku besar.
                    </span>
                    {choice === "journal" && (
                      <span className="mt-3 block space-y-3">
                        <Select
                          label="Akun Lawan"
                          value={counterAccount}
                          onChange={(e) => setCounterAccount(e.target.value)}
                        >
                          <option value="">— pilih akun —</option>
                          {accounts
                            .filter((a) => a.code !== rec.control_account_code)
                            .map((a) => (
                              <option key={a.id} value={a.code}>
                                {a.code} — {a.name}
                              </option>
                            ))}
                        </Select>
                        <Input
                          label="Keterangan Jurnal (opsional)"
                          value={openingDesc}
                          onChange={(e) => setOpeningDesc(e.target.value)}
                          placeholder="Saldo awal piutang proyek lama"
                        />
                      </span>
                    )}
                  </span>
                </label>
              )}

              <label className="flex gap-3 rounded border border-border p-3 cursor-pointer">
                <input
                  type="radio"
                  name="diff"
                  className="mt-1"
                  checked={choice === "accept"}
                  onChange={() => setChoice("accept")}
                />
                <span className="text-sm">
                  <span className="font-medium text-text-primary">
                    Lanjutkan dengan selisih, catat alasannya
                  </span>
                  <span className="block text-text-secondary">
                    Tidak ada jurnal dibuat. Selisih boleh ada — misalnya sebagian piutang lama
                    diimpor menyusul — tetapi alasannya tersimpan pada batch ini.
                  </span>
                  {choice === "accept" && (
                    <span className="mt-3 block">
                      <Input
                        label="Alasan"
                        value={skipReason}
                        onChange={(e) => setSkipReason(e.target.value)}
                        placeholder="mis. sisa proyek Griya Asri diimpor pada batch berikutnya"
                      />
                    </span>
                  )}
                </span>
              </label>
            </div>
          )}

          <div className="mt-5 flex justify-between gap-2">
            <Button variant="secondary" onClick={() => setStep(2)}>
              Kembali
            </Button>
            <Button onClick={() => setStep(4)} disabled={!choiceOk}>
              Lanjut — Konfirmasi
            </Button>
          </div>
        </Card>
      )}

      {step === 4 && preview && rec && (
        <Card>
          <h2 className="text-sm font-semibold text-text-primary">4. Konfirmasi</h2>
          <p className="mt-1 text-sm text-text-secondary">
            Setelah ini piutang tersimpan dan langsung muncul di daftar piutang customer.
          </p>

          <dl className="mt-4 divide-y divide-border rounded border border-border">
            <Row label="Berkas" value={preview.batch.file_name} />
            <Row label="Jumlah piutang" value={`${preview.batch.valid_count} baris`} />
            <Row label="Total nilai" value="" money={preview.batch.total_amount} />
            <Row label="Posisi per" value="" date={preview.batch.as_of_date} />
            <Row label="Akun kontrol" value={rec.control_account_code} />
            <Row
              label="Jurnal yang dibuat"
              value={
                choice === "journal"
                  ? `Jurnal saldo awal — Dr ${rec.control_account_code} / Cr ${counterAccount}`
                  : "TIDAK ADA"
              }
              emphasis={choice !== "journal"}
            />
            {choice === "accept" && <Row label="Alasan selisih" value={skipReason} />}
          </dl>

          {choice !== "journal" && (
            <p className="mt-3 text-sm text-text-secondary">
              Impor ini <strong>tidak menambah aset atau piutang di buku besar</strong>. Ia hanya
              memberi nama pada saldo yang sudah ada di sana.
            </p>
          )}

          <div className="mt-5 flex justify-between gap-2">
            <Button variant="secondary" onClick={() => setStep(3)} disabled={busy}>
              Kembali
            </Button>
            <Button onClick={doCommit} loading={busy} disabled={busy}>
              Impor Sekarang
            </Button>
          </div>
        </Card>
      )}

      {step === 5 && result && (
        <Card>
          <h2 className="text-sm font-semibold text-success">5. Selesai</h2>
          <p className="mt-1 text-sm text-text-secondary">
            {result.imported_count} piutang proyek lama senilai{" "}
            <Rupiah value={result.total_imported} colorSign={false} /> tersimpan.
            {result.opening_journal_id
              ? " Jurnal saldo awal ikut dibuat sesuai pilihan Anda."
              : " Tidak ada jurnal yang dibuat."}
          </p>

          <div className="mt-4">
            <LegacyReconciliationCard
              rec={result.reconciliation}
              title="Kecocokan setelah impor"
              defaultOpen
            />
          </div>

          <div className="mt-5 flex gap-2">
            <Link href="/accounting/legacy-ar">
              <Button>Lihat Daftar Piutang Proyek Lama</Button>
            </Link>
            <Link href="/accounting/receivable?source=legacy">
              <Button variant="secondary">Lihat di Piutang Customer</Button>
            </Link>
          </div>
        </Card>
      )}
    </div>
  );
}

function Stepper({ current }: { current: Step }) {
  return (
    <ol className="flex flex-wrap items-center gap-2 text-xs">
      {STEPS.map((s, i) => {
        const state = s.n < current ? "done" : s.n === current ? "now" : "next";
        return (
          <li key={s.n} className="flex items-center gap-2">
            <span
              className={`rounded-full px-2.5 py-1 ${
                state === "now"
                  ? "bg-accent text-white font-medium"
                  : state === "done"
                    ? "bg-success-bg text-success"
                    : "bg-border-subtle text-text-tertiary"
              }`}
            >
              {state === "done" ? "✓" : s.n}. {s.label}
            </span>
            {i < STEPS.length - 1 && <span className="text-text-tertiary">›</span>}
          </li>
        );
      })}
    </ol>
  );
}

function Stat({ label, value, danger }: { label: string; value: string; danger?: boolean }) {
  return (
    <div>
      <p className="text-xs uppercase tracking-wide text-text-secondary">{label}</p>
      <p className={`mt-0.5 font-semibold ${danger ? "text-danger" : "text-text-primary"}`}>
        {value}
      </p>
    </div>
  );
}

function Row({
  label,
  value,
  money,
  date,
  emphasis,
}: {
  label: string;
  value: string;
  money?: string;
  date?: string;
  emphasis?: boolean;
}) {
  return (
    <div className="flex items-baseline justify-between gap-4 px-4 py-2.5">
      <dt className="text-sm text-text-secondary">{label}</dt>
      <dd
        className={`text-sm ${emphasis ? "font-semibold text-text-primary" : "text-text-primary"}`}
      >
        {money !== undefined ? (
          <Rupiah value={money} colorSign={false} />
        ) : date !== undefined ? (
          <Tanggal value={date} />
        ) : (
          value
        )}
      </dd>
    </div>
  );
}
