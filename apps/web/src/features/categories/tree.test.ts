import { describe, expect, it } from "vitest";

import type { Category } from "@/lib/api/client";

import { buildTree } from "./tree";

const cat = (
  c: Partial<Category> & Pick<Category, "id" | "name">,
): Category => ({
  parentId: null,
  kind: "EXPENSE",
  icon: null,
  color: null,
  isActive: true,
  sortOrder: 0,
  createdAt: "2026-11-10T00:00:00Z",
  ...c,
});

describe("buildTree", () => {
  it("groups by kind and nests children in order", () => {
    const tree = buildTree([
      cat({ id: "m", name: "Mercado", parentId: "a", sortOrder: 10 }),
      cat({ id: "a", name: "Alimentação", sortOrder: 10 }),
      cat({ id: "r", name: "Restaurantes", parentId: "a", sortOrder: 0 }),
      cat({ id: "mo", name: "Moradia", sortOrder: 0 }),
      cat({ id: "s", name: "Salário", kind: "INCOME" }),
    ]);

    expect(tree.EXPENSE.map((n) => n.name)).toEqual(["Moradia", "Alimentação"]);
    expect(tree.EXPENSE[1].children.map((c) => c.name)).toEqual([
      "Restaurantes",
      "Mercado",
    ]);
    expect(tree.INCOME.map((n) => n.name)).toEqual(["Salário"]);
  });

  it("keeps orphans visible as top-level entries", () => {
    const tree = buildTree([
      cat({ id: "x", name: "Órfã", parentId: "missing" }),
    ]);
    expect(tree.EXPENSE.map((n) => n.name)).toEqual(["Órfã"]);
  });
});
