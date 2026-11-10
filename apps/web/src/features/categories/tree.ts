import type { Category } from "@/lib/api/client";

export type CategoryNode = Category & { children: Category[] };

export type CategoryTree = {
  EXPENSE: CategoryNode[];
  INCOME: CategoryNode[];
};

const byOrder = (a: Category, b: Category) =>
  a.sortOrder - b.sortOrder || a.name.localeCompare(b.name, "pt-BR");

/**
 * Groups a flat category list by kind and nests children under their parents.
 * Children whose parent is not in the list (e.g. filtered out) become top-level entries.
 */
export function buildTree(list: Category[]): CategoryTree {
  const ids = new Set(list.map((c) => c.id));
  const tree: CategoryTree = { EXPENSE: [], INCOME: [] };
  const nodes = new Map<string, CategoryNode>();

  for (const c of [...list].sort(byOrder)) {
    if (c.parentId === null || !ids.has(c.parentId)) {
      const node = { ...c, children: [] };
      nodes.set(c.id, node);
      tree[c.kind].push(node);
    }
  }
  for (const c of [...list].sort(byOrder)) {
    if (c.parentId !== null && nodes.has(c.parentId)) {
      nodes.get(c.parentId)!.children.push(c);
    }
  }
  return tree;
}
