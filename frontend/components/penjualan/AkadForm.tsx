"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Modal } from "@/components/ui/Modal";
import { Input } from "@/components/ui/Input";
import { FormGrid, FormFull } from "@/components/ui/Form";
import { Button } from "@/components/ui/Button";
import { RupiahInput, validateRupiah } from "@/components/ui/RupiahInput";
import { useToast } from "@/components/ui/Toast";
import { recordAkad } from "@/lib/api/sale";
import { ApiError } from "@/lib/api/client";
import { todayLocalStr } from "@/lib/date";

interface Props {
  open: boolean;
  onClose: () => void;
  unitId: number;
  listPrice: string;
  token: string;
  /** KPR: dipanggil TEPAT SEBELUM recordAkad — FinancingMilestones memakainya
   *  untuk ApplySchemeEvent(akad) dulu (bank_approved → akad), karena gate
   *  KPR (GateAkad) mensyaratkan state sudah "akad" saat RecordAkad dipanggil.
   *  Lempar error di sini membatalkan submit sebelum recordAkad tereksekusi. */
  onBeforeSubmit?: () => Promise<void>;
  onSubmitted?: () => void;
  /** Gap 1 (UAT 2026-09-07): bila true, form ini dipakai utk Akad Kredit KPR —
   *  tampilkan & wajibkan "Nilai Persetujuan KPR Bank" (source of truth Dana
   *  Jaminan Bank KPR). Non-KPR (cash, via UnitSalePanel) tidak mengisi ini. */
  isKPR?: boolean;
}

// Terjemahkan error backend teknis → langkah yang bisa dikerjakan user.
function friendlyAkadError(err: unknown): string {
  const msg = err instanceof ApiError ? err.message : "";
  if (msg.includes("alokasi belum diatur") || msg.includes("basis alokasi")) {
    return "Proyek belum punya dasar HPP: susun & approve RAB (tab RAB di proyek) atau atur basis alokasi biaya (tab Alokasi HPP), lalu catat Akad lagi.";
  }
  if (msg.includes("akad")) {
    return "Skema KPR mensyaratkan Akad Kredit tercatat di scheme flow sebelum Akad ini diproses — catat Akad Kredit terlebih dahulu.";
  }
  if (msg.includes("lunas") || msg.includes("full_payment")) {
    return "Skema ini mensyaratkan pelunasan penuh HARGA RUMAH sebelum Akad — masih ada tagihan berjalan. (Biaya realisasi tidak pernah menahan Akad.)";
  }
  if (msg.includes("pengakuan piutang biaya realisasi")) {
    return "Akad gagal saat menerbitkan tagihan biaya realisasi. Periksa jenis biaya & akun titipannya di Pengaturan → Jenis Biaya Realisasi, lalu ulangi.";
  }
  return msg || "Gagal mencatat Akad";
}

export function AkadForm({ open, onClose, unitId, listPrice, token, onBeforeSubmit, onSubmitted, isKPR }: Props) {
  const router = useRouter();
  const { toast } = useToast();
  const [loading, setLoading] = useState(false);

  const [salePrice, setSalePrice]         = useState(listPrice ?? "");
  const [isVAT, setIsVAT]                 = useState(false);
  const [vatRate, setVatRate]             = useState("0.11");
  const [buyerRef, setBuyerRef]           = useState("");
  const [recognitionDate, setRecognitionDate] = useState(todayLocalStr());
  const [bankApprovedAmount, setBankApprovedAmount] = useState("");

  const priceErr   = validateRupiah(salePrice);
  const buyerErr   = !buyerRef.trim() ? "Nama/referensi pembeli wajib diisi" : null;
  const vatRateErr = isVAT && (parseFloat(vatRate) <= 0 || isNaN(parseFloat(vatRate)))
    ? "Rate PPN tidak valid" : null;
  const bankApprovedErr = isKPR
    ? (validateRupiah(bankApprovedAmount) || (Number(bankApprovedAmount) <= 0 ? "Nilai Persetujuan KPR Bank wajib diisi" : null))
    : null;
  const isValid    = !priceErr && !buyerErr && !vatRateErr && !bankApprovedErr && recognitionDate;

  async function handleSubmit() {
    if (!isValid) return;
    setLoading(true);
    try {
      if (onBeforeSubmit) await onBeforeSubmit();
      const record = await recordAkad(token, unitId, {
        sale_price: salePrice,
        is_vat: isVAT,
        vat_rate: isVAT ? vatRate : undefined,
        buyer_ref: buyerRef.trim(),
        recognition_date: new Date(recognitionDate).toISOString(),
        bank_approved_amount: isKPR ? bankApprovedAmount : undefined,
      });
      toast(`Akad berhasil. Pendapatan & HPP sudah diakui (Jurnal #${record.revenue_journal_id}).`, "success");
      onSubmitted?.();
      onClose();
      router.refresh();
    } catch (err) {
      toast(friendlyAkadError(err), "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Catat Akad"
      size="md"
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={loading}>Batal</Button>
          <Button onClick={handleSubmit} loading={loading} disabled={!isValid}>
            Catat Akad
          </Button>
        </>
      }
    >
      <FormGrid>
        <FormFull>
          <div className="bg-warning-bg border border-warning/30 rounded-lg px-3 py-2.5 text-xs text-warning">
            Akad bersifat permanen — pendapatan dan HPP diakui saat ini, langsung ke jurnal
            akuntansi. Serah terima fisik unit terpisah dan dicatat belakangan.
          </div>
        </FormFull>

        <RupiahInput
          label="Harga Jual (DPP)"
          value={salePrice}
          onChange={setSalePrice}
          required
          error={salePrice && priceErr ? priceErr : undefined}
          hint="Harga neto sebelum PPN"
        />
        <Input
          label="Tanggal Akad"
          type="date"
          value={recognitionDate}
          onChange={(e) => setRecognitionDate(e.target.value)}
          required
        />

        {isKPR && (
          <FormFull>
            <RupiahInput
              label="Nilai Persetujuan KPR Bank"
              value={bankApprovedAmount}
              onChange={setBankApprovedAmount}
              required
              error={bankApprovedAmount && bankApprovedErr ? bankApprovedErr : undefined}
              hint="Nominal yang benar-benar disetujui bank — jadi source of truth Dana Jaminan Bank KPR."
            />
          </FormFull>
        )}
        <FormFull>
          <Input
            label="Referensi Pembeli"
            value={buyerRef}
            onChange={(e) => setBuyerRef(e.target.value)}
            placeholder="Nama pembeli atau nomor SPK"
            required
            error={buyerRef && buyerErr ? buyerErr : undefined}
          />
        </FormFull>

        <label
          htmlFor="is-vat"
          className="flex cursor-pointer items-center gap-3 rounded-lg border border-border px-3.5 py-2.5
            transition-colors hover:bg-border-subtle/40"
        >
          <input
            id="is-vat"
            type="checkbox"
            checked={isVAT}
            onChange={(e) => setIsVAT(e.target.checked)}
            className="h-4 w-4 accent-accent cursor-pointer"
          />
          <span className="text-sm text-text-primary">Penjual PKP (kenakan PPN)</span>
        </label>

        {isVAT && (
          <Input
            label="Rate PPN"
            value={vatRate}
            onChange={(e) => setVatRate(e.target.value)}
            placeholder="0.11"
            hint="Desimal: 0.11 = 11%"
            error={vatRateErr ?? undefined}
          />
        )}

        <FormFull>
          <p className="text-xs text-text-secondary bg-border-subtle/60 border border-border-subtle rounded-lg px-3 py-2">
            Jurnal Event 3: Dr Piutang/UMP / Cr Pendapatan{isVAT ? " + Cr PPN Keluaran" : ""}.
            Jurnal Event 4: Dr HPP / Cr Persediaan (berdasarkan alokasi biaya unit).
          </p>
        </FormFull>

        {/* W-5 / D-3: biaya realisasi TIDAK menahan Akad. Yang terjadi
            saat Akad adalah penerbitan tagihannya, bukan penuntutan pelunasan —
            dan itu harus disebut di muka supaya admin tidak menahan Akad sendiri
            sambil menunggu customer melunasi. */}
        <FormFull>
          <p className="text-xs text-text-secondary border border-border rounded-lg px-3 py-2">
            Biaya realisasi yang belum ditagihkan akan <strong>otomatis diterbitkan invoice-nya</strong>{" "}
            dalam transaksi Akad ini, dan sisanya menjadi <strong>Piutang Customer</strong>. Akad
            tidak menunggu biaya realisasi lunas.
          </p>
        </FormFull>
      </FormGrid>
    </Modal>
  );
}
