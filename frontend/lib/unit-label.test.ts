import test from "node:test";
import assert from "node:assert/strict";
import { unitLabel, buyerRoleLabel } from "./unit-label.ts";

test("unitLabel: unit tanpa pemegang tampil apa adanya", () => {
  assert.equal(unitLabel("C-01"), "C-01");
  assert.equal(unitLabel("C-01", undefined), "C-01");
  assert.equal(unitLabel("C-01", null), "C-01");
});

test("unitLabel: nama kosong/spasi tidak menghasilkan pemisah menggantung", () => {
  // Backend mengirim string kosong untuk unit tanpa pemegang; "C-01 · " adalah
  // cacat tampilan yang gampang lolos kalau hanya dicek truthiness.
  assert.equal(unitLabel("C-01", ""), "C-01");
  assert.equal(unitLabel("C-01", "   "), "C-01");
});

test("unitLabel: nama pemegang digabung dengan pemisah titik-tengah", () => {
  assert.equal(unitLabel("C-01", "Udin"), "C-01 · Udin");
  assert.equal(unitLabel("A-12", " Budi Santoso "), "A-12 · Budi Santoso");
});

test("buyerRoleLabel: booking disebut Pemesan, sisanya Pembeli", () => {
  assert.equal(buyerRoleLabel("booking"), "Pemesan");
  assert.equal(buyerRoleLabel("contract"), "Pembeli");
  assert.equal(buyerRoleLabel("unit"), "Pembeli");
  assert.equal(buyerRoleLabel(undefined), "Pembeli");
  assert.equal(buyerRoleLabel("entah"), "Pembeli");
});
