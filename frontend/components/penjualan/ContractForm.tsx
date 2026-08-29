"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { Modal } from "@/components/ui/Modal";
import { Input, Select } from "@/components/ui/Input";
import { FormGrid, FormFull } from "@/components/ui/Form";
import { Button } from "@/components/ui/Button";
import { RupiahInput, validateRupiah } from "@/components/ui/RupiahInput";
import { useToast } from "@/components/ui/Toast";
import { createContract } from "@/lib/api/sale";
import { ApiError } from "@/lib/api/client";
import { todayLocalStr } from "@/lib/date";
import {
  fetchCustomers,
  fetchSalesPersons,
  fetchPaymentSchemes,
  fetchFinancingSources,
  createSalesPerson,
  createFinancingSource,
  type Customer,
  type SalesPerson,
  type PaymentScheme,
  type FinancingSource,
} from "@/lib/api/party";
import { fetchProductTypes, isAddonProduct, type ProductType } from "@/lib/api/projects";
import { createChargeGroup } from "@/lib/api/charge";
import { fetchLandStock, type LandStock } from "@/lib/api/land";
import { formatRupiah } from "@/components/format/Rupiah";

interface AddonRow {
  key: number;   // stabil seumur baris — bukan index, supaya menghapus baris
  code: string;  // tidak me-remount field tetangga dan merebut fokus.
  amount: string;
}

// Saran kode master berikutnya dari daftar yang ada (prefix-NNN).
// availableM2 — kuantitas Kelebihan Tanah yang belum direservasi/terjual.
// Murni display; pool.reserved/sold_quantity_m2 sudah dijaga backend row-lock.
function availableM2(pool: LandStock): number {
  return Number(pool.total_quantity_m2) - Number(pool.reserved_quantity_m2) - Number(pool.sold_quantity_m2);
}

function nextMasterCode(prefix: string, existing: { code?: string }[]): string {
  let max = 0;
  for (const e of existing) {
    const m = (e.code ?? "").match(new RegExp(`^${prefix}-?(\\d+)$`, "i"));
    if (m) max = Math.max(max, parseInt(m[1], 10));
  }
  return `${prefix}-${String(max + 1).padStart(3, "0")}`;
}

// Inline create — master data bisa dibuat DI DALAM workflow (tanpa pindah
// halaman): dropdown kosong tidak pernah memblokir user.
function InlineCreate({
  label, placeholder, onCreate,
}: {
  label: string;
  placeholder: string;
  onCreate: (name: string) => Promise<void>;
}) {
  const { toast } = useToast();
  const [openForm, setOpenForm] = useState(false);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);

  if (!openForm) {
    return (
      <button type="button" className="mt-1 text-xs text-accent hover:underline cursor-pointer"
        onClick={() => setOpenForm(true)}>
        {label}
      </button>
    );
  }
  return (
    <div className="mt-1.5 flex gap-1.5">
      <input
        autoFocus
        className="flex-1 rounded border border-border bg-surface px-2 py-1 text-xs outline-none focus:border-accent"
        placeholder={placeholder}
        value={name}
        onChange={(e) => setName(e.target.value)}
        onKeyDown={async (e) => {
          if (e.key === "Enter" && name.trim() && !busy) {
            e.preventDefault();
            setBusy(true);
            try {
              await onCreate(name.trim());
              setOpenForm(false);
              setName("");
            } catch (err) {
              toast(err instanceof ApiError ? err.message : "Gagal membuat", "error");
            } finally {
              setBusy(false);
            }
          }
          if (e.key === "Escape") setOpenForm(false);
        }}
      />
      <button type="button" disabled={busy || !name.trim()}
        className="rounded bg-accent px-2 py-1 text-xs text-white disabled:opacity-50 cursor-pointer"
        onClick={async () => {
          setBusy(true);
          try {
            await onCreate(name.trim());
            setOpenForm(false);
            setName("");
          } catch (err) {
            toast(err instanceof ApiError ? err.message : "Gagal membuat", "error");
          } finally {
            setBusy(false);
          }
        }}>
        Simpan
      </button>
    </div>
  );
}

interface Props {
  open: boolean;
  onClose: () => void;
  unitId: number;
  projectId: number;
  listPrice: string;
  token: string;
  /** Increment 7 — konversi booking → kontrak (atomik; fee jadi Saldo Kredit Buyer). */
  bookingId?: number;
  /** Customer booking (terkunci saat konversi — backend memvalidasi kecocokan). */
  lockedCustomerId?: number;
  /** P1 — Sales person booking (terkunci saat konversi): Sales ditetapkan saat
   *  Booking dan komisi selalu mengikutinya, jadi kontrak konversi tidak boleh
   *  membiarkannya diganti diam-diam. */
  lockedSalesPersonId?: number;
}

// Kontrak baru WAJIB scheme + customer + sales person (kebijakan Increment 3 —
// backend menolak tanpa ini). Field diambil dari master; skema KPR butuh bank
// (financing source). payment_type legacy diturunkan dari policy scheme.
export function ContractForm({
  open, onClose, unitId, projectId, listPrice, token, bookingId, lockedCustomerId, lockedSalesPersonId,
}: Props) {
  const router = useRouter();
  const { toast } = useToast();
  const [loading, setLoading] = useState(false);

  const [schemes, setSchemes] = useState<PaymentScheme[]>([]);
  const [customers, setCustomers] = useState<Customer[]>([]);
  const [salesPersons, setSalesPersons] = useState<SalesPerson[]>([]);
  const [finSources, setFinSources] = useState<FinancingSource[]>([]);

  const [schemeId, setSchemeId] = useState("");
  const [customerId, setCustomerId] = useState(lockedCustomerId ? String(lockedCustomerId) : "");
  const [salesPersonId, setSalesPersonId] = useState(lockedSalesPersonId ? String(lockedSalesPersonId) : "");
  // P1 — Admin Marketing: peran terpisah dari Sales (komisi tetap ke salesPersonId).
  // Opsional; reuse master personil yang sama, dibedakan lewat kolom bukan tabel.
  const [adminMarketingPersonId, setAdminMarketingPersonId] = useState("");
  const [financingSourceId, setFinancingSourceId] = useState("");

  const [buyerName, setBuyerName] = useState("");
  const [buyerId, setBuyerId] = useState("");
  const [loanAmount, setLoanAmount] = useState("");
  const [contractDate, setContractDate] = useState(todayLocalStr());
  const [totalPrice, setTotalPrice] = useState(listPrice ?? "");

  // W-13 — produk tambahan ikut dijual di sini, bukan sebagai unit tersendiri.
  // Kelebihan tanah dan sejenisnya tidak pernah menyerap biaya proyek, jadi
  // tidak boleh berdiri sebagai unit (unit tanpa HPP). Tempatnya menempel pada
  // penjualan unit sebagai baris tagihan addon, dan produklah yang menentukan
  // akun pendapatannya.
  const [addonProducts, setAddonProducts] = useState<ProductType[]>([]);
  const [addons, setAddons] = useState<AddonRow[]>([]);
  const addonKey = useRef(0);

  // Produk Tambahan: Kelebihan Tanah (kelebihan-tanah-booking-integration-2026-08)
  // — HANYA untuk kontrak LANGSUNG (tanpa booking). Jalur konversi booking sudah
  // membawa komponen tanah dari Booking di backend; menampilkan picker lagi di
  // sini akan membingungkan/bisa dobel input, jadi disembunyikan bila bookingId ada.
  const [landPool, setLandPool] = useState<LandStock | null>(null);
  const [wantsLand, setWantsLand] = useState(false);
  const [landQty, setLandQty] = useState("");

  useEffect(() => {
    if (!open) return;
    fetchPaymentSchemes(token).then(setSchemes).catch(() => {});
    fetchCustomers(token).then(setCustomers).catch(() => {});
    fetchSalesPersons(token).then(setSalesPersons).catch(() => {});
    fetchFinancingSources(token).then(setFinSources).catch(() => {});
    fetchProductTypes(token)
      .then((pts) => setAddonProducts(pts.filter(isAddonProduct)))
      .catch(() => {});
    if (!bookingId) {
      fetchLandStock(token, projectId).then(setLandPool).catch(() => setLandPool(null));
    }
  }, [open, token, projectId, bookingId]);

  useEffect(() => {
    if (!open) {
      setWantsLand(false);
      setLandQty("");
    }
  }, [open]);

  const landAvailable = landPool ? availableM2(landPool) : 0;
  const landQtyNum = Number(landQty.replace(",", "."));
  const landQtyErr = !wantsLand
    ? null
    : !landQty || isNaN(landQtyNum) || landQtyNum <= 0
      ? "Kuantitas harus angka positif"
      : landQtyNum > landAvailable
        ? `Melebihi tersedia (${landAvailable.toLocaleString("id-ID")} m²)`
        : null;
  const landTotalPreview =
    wantsLand && landPool && !landQtyErr ? landQtyNum * Number(landPool.unit_price) : null;

  function addAddonRow() {
    addonKey.current += 1;
    setAddons((prev) => [...prev, { key: addonKey.current, code: "", amount: "" }]);
  }

  function patchAddonRow(key: number, patch: Partial<AddonRow>) {
    setAddons((prev) => prev.map((r) => (r.key === key ? { ...r, ...patch } : r)));
  }

  // Baris addon yang benar-benar dikirim: produk terpilih DAN nominal > 0.
  // Baris setengah terisi diabaikan diam-diam — pengguna yang membuka baris lalu
  // berubah pikiran tidak boleh terhalang menyimpan kontraknya.
  const filledAddons = addons.filter((r) => r.code !== "" && parseInt(r.amount || "0", 10) > 0);

  useEffect(() => {
    if (lockedCustomerId) setCustomerId(String(lockedCustomerId));
  }, [lockedCustomerId]);

  useEffect(() => {
    if (lockedSalesPersonId) setSalesPersonId(String(lockedSalesPersonId));
  }, [lockedSalesPersonId]);

  // Prefill nama pembeli dari customer terpilih (tetap bisa diedit).
  useEffect(() => {
    const c = customers.find((x) => String(x.id) === customerId);
    if (c) setBuyerName((prev) => (prev ? prev : c.name));
  }, [customerId, customers]);

  const selectedScheme = useMemo(
    () => schemes.find((s) => String(s.id) === schemeId),
    [schemes, schemeId],
  );
  const isKPR = selectedScheme?.policy_type === "kpr";

  // Pesan-pesan di bawah dipakai untuk MENGUNCI tombol simpan, bukan untuk
  // diwarnai merah sejak modal dibuka. Field yang belum diisi belum salah —
  // asterisk merah pada label dan tombol simpan yang mati sudah menyampaikan
  // itu. Merah disimpan untuk isian yang benar-benar tidak sah.
  const buyerNameErr = buyerName.trim().length === 0 ? "Nama pembeli wajib diisi" : null;
  const buyerIdErr = buyerId.trim().length === 0 ? "NIK/KTP wajib diisi" : null;
  const schemeErr = !schemeId ? "Skema pembayaran wajib dipilih" : null;
  const customerErr = !customerId ? "Customer wajib dipilih" : null;
  const salesErr = !salesPersonId ? "Sales person wajib dipilih" : null;
  const finErr = isKPR && !financingSourceId ? "Bank KPR wajib dipilih untuk skema KPR" : null;
  const priceErr = validateRupiah(totalPrice);

  function isValid() {
    return !buyerNameErr && !buyerIdErr && !schemeErr && !customerErr &&
      !salesErr && !finErr && !priceErr && !!contractDate && !landQtyErr;
  }

  async function handleSubmit() {
    if (!isValid()) return;
    setLoading(true);
    try {
      const contract = await createContract(token, {
        unit_id: unitId,
        buyer_name: buyerName.trim(),
        buyer_id: buyerId.trim(),
        payment_type: isKPR ? "kpr" : "tunai",
        loan_amount: isKPR && loanAmount ? loanAmount : undefined,
        contract_date: new Date(contractDate).toISOString(),
        total_price: totalPrice,
        payment_scheme_id: parseInt(schemeId, 10),
        customer_id: parseInt(customerId, 10),
        sales_person_id: parseInt(salesPersonId, 10),
        admin_marketing_person_id: adminMarketingPersonId ? parseInt(adminMarketingPersonId, 10) : undefined,
        financing_source_id: isKPR ? parseInt(financingSourceId, 10) : undefined,
        booking_id: bookingId,
        land_quantity_m2: !bookingId && wantsLand && landQty ? landQty : undefined,
      });

      // Grup addon butuh sale_contract_id, jadi baru bisa dibuat setelah
      // kontraknya ada. Kegagalan di sini TIDAK membatalkan kontrak yang sudah
      // tersimpan — katakan apa adanya dan arahkan menambahkannya dari halaman
      // penjualan, jangan menelan errornya.
      let addonWarning: string | null = null;
      if (filledAddons.length > 0) {
        try {
          await createChargeGroup(token, {
            sale_contract_id: contract.id,
            kind: "addon",
            label: "Produk Tambahan",
            items: filledAddons.map((r) => ({
              label: addonProducts.find((p) => p.code === r.code)?.name ?? r.code,
              amount: r.amount,
              product_code: r.code,
            })),
          });
        } catch (err) {
          addonWarning = err instanceof ApiError ? err.message : "gagal menyimpan produk tambahan";
        }
      }

      const landNote = contract.land_quantity_m2
        ? ` + Kelebihan Tanah ${Number(contract.land_quantity_m2).toLocaleString("id-ID")} m² direservasi`
        : "";

      if (addonWarning) {
        toast(
          `Kontrak #${contract.id} tersimpan, tetapi produk tambahan gagal: ${addonWarning}. ` +
            `Tambahkan lagi dari panel Tagihan di halaman penjualan.`,
          "error",
        );
      } else {
        toast(
          (bookingId
            ? `Kontrak #${contract.id} dibuat — booking terkonversi, fee menjadi Saldo Kredit Buyer`
            : `Kontrak #${contract.id} berhasil dibuat`) + landNote,
          "success",
        );
      }
      onClose();
      router.push(`/penjualan/${unitId}?contract=${contract.id}`);
      router.refresh();
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "Gagal membuat kontrak", "error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={bookingId ? `Konversi Booking #${bookingId} → Kontrak` : "Buat Kontrak Penjualan"}
      size="xl"
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={loading}>Batal</Button>
          <Button onClick={handleSubmit} loading={loading} disabled={!isValid()}>
            {bookingId ? "Konversi & Simpan Kontrak" : "Simpan Kontrak"}
          </Button>
        </>
      }
    >
      <FormGrid columns={3}>
        {bookingId && (
          <FormFull>
            <p className="rounded-lg border border-accent/40 bg-accent-light px-3.5 py-2.5 text-sm text-text-primary">
              Konversi booking: fee direklas menjadi <strong>Saldo Kredit Buyer</strong>{" "}
              dan bisa dipakai menutup DP setelah jadwal cicilan dibuat.
            </p>
          </FormFull>
        )}

          <Select
            label="Skema Pembayaran"
            value={schemeId}
            onChange={(e) => setSchemeId(e.target.value)}
            required
          >
            <option value="">— pilih skema —</option>
            {schemes.filter((s) => s.is_active).map((s) => (
              <option key={s.id} value={s.id}>{s.name}</option>
            ))}
          </Select>

          <Select
            label="Customer"
            value={customerId}
            onChange={(e) => setCustomerId(e.target.value)}
            required
            disabled={!!lockedCustomerId}
          >
            <option value="">— pilih customer —</option>
            {customers.map((c) => (
              <option key={c.id} value={c.id}>{c.name} ({c.code})</option>
            ))}
          </Select>

          <div>
            <Select
              label="Sales Person"
              value={salesPersonId}
              onChange={(e) => setSalesPersonId(e.target.value)}
              required
              disabled={!!lockedSalesPersonId}
            >
              <option value="">— pilih sales —</option>
              {salesPersons.filter((s) => s.is_active).map((s) => (
                <option key={s.id} value={s.id}>{s.name}</option>
              ))}
            </Select>
            {lockedSalesPersonId ? (
              <p className="mt-1 text-xs text-text-tertiary">Ditetapkan saat Booking — komisi mengikuti sales ini.</p>
            ) : (
              <InlineCreate
                label="+ sales baru"
                placeholder="nama sales"
                onCreate={async (name) => {
                  const p = await createSalesPerson(token, {
                    code: nextMasterCode("SP", salesPersons),
                    name,
                  });
                  setSalesPersons((prev) => [...prev, p]);
                  setSalesPersonId(String(p.id));
                }}
              />
            )}
          </div>

          <div>
            <Select
              label="Admin Marketing"
              value={adminMarketingPersonId}
              onChange={(e) => setAdminMarketingPersonId(e.target.value)}
            >
              <option value="">— opsional —</option>
              {salesPersons.filter((s) => s.is_active).map((s) => (
                <option key={s.id} value={s.id}>{s.name}</option>
              ))}
            </Select>
            <p className="mt-1 text-xs text-text-tertiary mb-1">Menangani dokumen/KPR — bisa berbeda orang dari Sales.</p>
            <InlineCreate
              label="+ personil baru"
              placeholder="nama personil"
              onCreate={async (name) => {
                const p = await createSalesPerson(token, {
                  code: nextMasterCode("SP", salesPersons),
                  name,
                });
                setSalesPersons((prev) => [...prev, p]);
                setAdminMarketingPersonId(String(p.id));
              }}
            />
          </div>

          {isKPR && (
            <div>
              <Select
                label="Bank KPR"
                value={financingSourceId}
                onChange={(e) => setFinancingSourceId(e.target.value)}
                required
              >
                <option value="">— pilih bank —</option>
                {finSources.filter((f) => f.is_active).map((f) => (
                  <option key={f.id} value={f.id}>{f.name}</option>
                ))}
              </Select>
              <InlineCreate
                label="+ bank baru"
                placeholder="nama bank (cth: Bank BTN)"
                onCreate={async (name) => {
                  const code = name.replace(/[^A-Za-z0-9]/g, "").toUpperCase().slice(0, 10) || "BANK";
                  const f = await createFinancingSource(token, {
                    code, name, type: "bank_kpr_subsidi",
                  });
                  setFinSources((prev) => [...prev, f]);
                  setFinancingSourceId(String(f.id));
                }}
              />
            </div>
          )}

        <Input
          label="Nama Pembeli (di kontrak)"
          value={buyerName}
          onChange={(e) => setBuyerName(e.target.value)}
          required
          error={buyerName && buyerNameErr ? buyerNameErr : undefined}
        />
        <Input
          label="NIK / KTP"
          value={buyerId}
          onChange={(e) => setBuyerId(e.target.value)}
          placeholder="16 digit NIK"
          required
          error={buyerId && buyerIdErr ? buyerIdErr : undefined}
        />
        <Input
          label="Tanggal Kontrak"
          type="date"
          value={contractDate}
          onChange={(e) => setContractDate(e.target.value)}
          required
        />
        <RupiahInput
          label="Harga Jual (DPP)"
          value={totalPrice}
          onChange={setTotalPrice}
          hint="Harga dasar sebelum PPN. Terisi dari harga list unit."
          required
          error={totalPrice && priceErr ? priceErr : undefined}
        />
        {isKPR && (
          <RupiahInput
            label="Nilai Pengajuan KPR"
            value={loanAmount}
            onChange={setLoanAmount}
            hint="Opsional; bisa diisi saat pengajuan ke bank."
          />
        )}

        {/* Produk Tambahan: Kelebihan Tanah — hanya untuk kontrak LANGSUNG
            (bookingId kosong). Jalur konversi booking membawa komponen tanah
            dari Booking-nya sendiri di backend. Sales HANYA mengisi kuantitas;
            harga & total di bawah murni preview dari harga pool proyek. */}
        {!bookingId && landPool && landAvailable > 0 && (
          <FormFull>
            {!wantsLand ? (
              <button
                type="button"
                className="text-xs text-accent hover:underline"
                onClick={() => setWantsLand(true)}
              >
                + Tambahkan Produk Tambahan: Kelebihan Tanah
              </button>
            ) : (
              <div className="rounded-lg border border-border-subtle p-3 space-y-2">
                <div className="flex items-center justify-between">
                  <p className="text-xs font-medium text-text-secondary">
                    Produk Tambahan: Kelebihan Tanah
                  </p>
                  <button
                    type="button"
                    className="text-xs text-text-tertiary hover:underline"
                    onClick={() => { setWantsLand(false); setLandQty(""); }}
                  >
                    Hapus
                  </button>
                </div>
                <Input
                  label="Kuantitas (m²)" required
                  value={landQty}
                  onChange={(e) => setLandQty(e.target.value)}
                  suffix="m²"
                  error={landQtyErr ?? undefined}
                  hint={!landQtyErr ? `Tersedia ${landAvailable.toLocaleString("id-ID")} m² · Rp ${Number(landPool.unit_price).toLocaleString("id-ID")}/m²` : undefined}
                  placeholder="10"
                />
                {landTotalPreview !== null && (
                  <p className="text-xs text-text-secondary">
                    Total: <strong>{formatRupiah(landTotalPreview)}</strong> (otomatis, harga dari pool proyek)
                  </p>
                )}
              </div>
            )}
          </FormFull>
        )}

        <FormFull>
          <div className="rounded-lg border border-border-subtle bg-bg p-3.5">
            <div className="flex items-baseline justify-between gap-3">
              <div>
                <h4 className="text-sm font-semibold text-text-primary">
                  Produk Tambahan <span className="font-normal text-text-tertiary">(opsional)</span>
                </h4>
                <p className="mt-0.5 text-xs text-text-secondary">
                  Kelebihan tanah dan produk non-properti lain dijual menempel pada unit ini —
                  bukan sebagai unit tersendiri. Nilainya di luar Harga Jual di atas, dan diakui
                  sebagai pendapatan pada momen yang sama dengan unitnya.
                </p>
              </div>
              {addonProducts.length > 0 && (
                <Button variant="secondary" size="sm" onClick={addAddonRow}>
                  + Produk
                </Button>
              )}
            </div>

            {addonProducts.length === 0 ? (
              <p className="mt-2.5 text-xs text-text-tertiary">
                Belum ada produk non-properti di Katalog Produk. Tambahkan dulu di{" "}
                <strong>Pengaturan → Katalog Produk</strong> (kategori non-properti), lalu produk
                itu bisa dipilih di sini.
              </p>
            ) : addons.length === 0 ? (
              <p className="mt-2.5 text-xs text-text-tertiary">
                Tidak ada produk tambahan untuk penjualan ini. Bisa juga ditambahkan nanti dari
                panel Tagihan di halaman penjualan.
              </p>
            ) : (
              <div className="mt-3 space-y-2">
                {addons.map((row) => (
                  <div key={row.key} className="flex items-start gap-2">
                    <div className="flex-1">
                      <Select
                        value={row.code}
                        onChange={(e) => patchAddonRow(row.key, { code: e.target.value })}
                      >
                        <option value="">— pilih produk —</option>
                        {addonProducts.map((p) => (
                          <option key={p.code} value={p.code}>{p.name}</option>
                        ))}
                      </Select>
                    </div>
                    <div className="w-52">
                      <RupiahInput
                        value={row.amount}
                        onChange={(v) => patchAddonRow(row.key, { amount: v })}
                      />
                    </div>
                    <button
                      type="button"
                      className="mt-2.5 px-1.5 text-xs text-text-tertiary hover:text-danger cursor-pointer"
                      onClick={() => setAddons((prev) => prev.filter((r) => r.key !== row.key))}
                      aria-label="Hapus baris produk tambahan"
                    >
                      Hapus
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        </FormFull>
      </FormGrid>
    </Modal>
  );
}
