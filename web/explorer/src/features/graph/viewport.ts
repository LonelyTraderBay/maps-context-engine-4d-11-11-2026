export const hardNodes = 500;
export const hardEdges = 1500;
export const nodeWidth = 178;
export const nodeHeight = 58;

export function compareID(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0;
}
