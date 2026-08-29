"use client";

import { useState } from "react";
import { Card, CardHeader, CardTitle } from "@/components/ui/Card";
import { Input, Select } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { useToast } from "@/components/ui/Toast";
import { setTaxRate } from "@/lib/api/tax";
import { ApiError } from "@/lib/api/client";
import { todayLocalStr } from "@/lib/date";

interface Props {
  token: string;
}

const RATE_CODES = [
  {
    code: "pph_final_pengalihan",
    label: "PPh Final Pengalihan (PP 34/2016)",
    hint: "Dikenakan atas penjualan unit properti. Default: 2,5%.",
    defaultRate: "2.5",
  },
  {
    code: "ppn_keluaran",
    label: "PPN Keluaran",
    hint: "Dikenakan untuk penjual PKP. Konfirmasi tarif 11% atau 12% dengan tax advisor.",
    defaultRate: "11",
  },
];

export function TaxRateForm({ token }: Props) {
  const { toast }             = useToast();
  const [loading, setLoading] = useState(false);
  const [rateCode, setRateCode] = useState("pph_final_pengalihan");
  const [ratePercent, setRatePercent] = useState("2.5");
  const [effectiveFrom, setEffectiveFrom] = useState(
    todayLocalStr(),
  );
  const [description, setDescription] = useState("");

  const selectedCode = RATE_CODES.find((r) => r.code === rateCode)!;

  function handleCodeChange(code: string) {
    setRateCode(code);
    const preset = RATE_CODES.find((r) => r.code === code);
    if (preset) setRatePercent(preset.defaultRate);
  }

  const rateNum   = parseFloat(ratePercent);
  const rateValid = !isNaN(rateNum) && rateNum > 0 && rateNum <= 100;
  const isValid   = rateValid && effectiveFrom;

  async function handleSubmit() {
    if (!isValid) return;
    setLoading(true);
    try {
      await setTaxRate(token, {
        rate_code: rateCode,
        rate: (rateNum / 100).toFixed(6),
        effective_from: effectiveFrom,
        description: description.trim() || selectedCode.label,
      });
      toast(`Tarif ${selectedCode.label} ${ratePercent}% berhasil disimpan`, "success");
      setDescription("");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal menyimpan tarif", "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Konfigurasi Tarif Pajak</CardTitle>
      </CardHeader>

      <div className="space-y-4">
        <div className="text-xs text-text-secondary bg-warning-bg border border-warning/30 rounded p-3 space-y-1">
          <p className="font-medium text-warning">Catatan:</p>
          <p>Perubahan tarif hanya berlaku untuk akrual baru (tanggal ≥ <em>Berlaku Mulai</em>).</p>
          <p>Akrual yang sudah tercatat menggunakan snapshot tarif saat itu — tidak berubah retroaktif.</p>
          {/* TODO(tax-advisor): Konfirmasi 12% vs 11% efektif untuk hunian mewah LITHOS */}
        </div>

        <Select
          label="Jenis Pajak"
          value={rateCode}
          onChange={(e) => handleCodeChange(e.target.value)}
        >
          {RATE_CODES.map((r) => (
            <option key={r.code} value={r.code}>{r.label}</option>
          ))}
        </Select>

        {selectedCode && (
          <p className="text-xs text-text-tertiary -mt-2">{selectedCode.hint}</p>
        )}

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <Input
            label="Tarif (%)"
            type="number"
            min="0.01"
            max="100"
            step="0.01"
            value={ratePercent}
            onChange={(e) => setRatePercent(e.target.value)}
            required
            error={ratePercent && !rateValid ? "Tarif harus antara 0,01% dan 100%" : undefined}
            hint={rateValid ? `= ${(rateNum / 100).toFixed(6)} desimal` : undefined}
          />
          <Input
            label="Berlaku Mulai"
            type="date"
            value={effectiveFrom}
            onChange={(e) => setEffectiveFrom(e.target.value)}
            required
          />
        </div>

        <Input
          label="Deskripsi (opsional)"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder={selectedCode.label}
        />

        <Button onClick={handleSubmit} loading={loading} disabled={!isValid}>
          Simpan Tarif
        </Button>
      </div>
    </Card>
  );
}
