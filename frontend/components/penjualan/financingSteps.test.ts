import test from "node:test";
import assert from "node:assert/strict";
import { FINANCING_STEPS, stepIndex } from "./financingSteps.ts";

test("stepIndex: kondisi normal mengikuti urutan STEPS", () => {
  assert.equal(stepIndex("signed"), 0);
  assert.equal(stepIndex("submitted_to_bank"), 1);
  assert.equal(stepIndex("bank_approved"), 2);
  assert.equal(stepIndex("akad"), 3);
  assert.equal(stepIndex("disbursed"), 4);
});

test("stepIndex: dp_paid setara signed", () => {
  assert.equal(stepIndex("dp_paid"), 0);
});

test("stepIndex: fully_paid/handed_over tetap tampil sebagai langkah terakhir tuntas", () => {
  // Regresi: sebelum perbaikan, stepIndex("fully_paid") == -1 (findIndex tidak
  // menemukan state ini di STEPS) sehingga seluruh stepper KPR tampil kosong
  // begitu kontrak lunas — walau Akad Kredit + pencairan sudah tuntas.
  assert.equal(stepIndex("fully_paid"), FINANCING_STEPS.length - 1);
  assert.equal(stepIndex("handed_over"), FINANCING_STEPS.length - 1);
});

test("stepIndex: state tak dikenal tetap -1 (bukan disamarkan jadi progres)", () => {
  assert.equal(stepIndex("bank_rejected"), -1);
  assert.equal(stepIndex("entah"), -1);
});
