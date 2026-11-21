import { useEffect, useMemo, useState } from 'react';
import type { GraphSlice } from '../../shell/map.ts';
import type { LayoutInput, LayoutResult } from './layout.ts';

export function useLayout(graph: GraphSlice, scope: string | null, enabled: boolean): LayoutResult | null {
  // Topology is sufficient for the drawing. Labels, selection and hover do
  // not affect positions. Keep the memo key separate from domain graph IDs.
  const topology = useMemo(() => {
    const ids = new Set(graph.nodes.map(node => node.node_id));
    return JSON.stringify({
      nodes: [...ids].sort(),
      edges: graph.edges.filter(edge => ids.has(edge.from_node_id) && ids.has(edge.to_node_id))
        .map(edge => [edge.edge_id, edge.from_node_id, edge.to_node_id]).sort(),
    });
  }, [graph.nodes, graph.edges]);
  const key = scope === null ? null : scope + ':' + topology;
  const [settled, setSettled] = useState<{ key: string; result: LayoutResult } | null>(null);
  const current = settled?.key === key ? settled?.result : null;

  useEffect(() => {
    if (!enabled || !key || current || graph.nodes.length === 0) return;
    let active = true;
    let worker: Worker;
    try {
      worker = new Worker(new URL('./layout.worker.ts', import.meta.url), { type: 'module' });
    } catch {
      setSettled({ key, result: { error: true } });
      return;
    }
    const finish = (result: LayoutResult) => {
      clearTimeout(timeout);
      if (active) setSettled({ key, result });
      worker.terminate();
    };
    const timeout = setTimeout(() => finish({ error: true }), 3000);
    worker.onmessage = (event: MessageEvent<LayoutResult>) => finish(event.data);
    worker.onerror = () => finish({ error: true });
    worker.postMessage(JSON.parse(topology) as LayoutInput);
    return () => {
      active = false;
      clearTimeout(timeout);
      worker.terminate();
    };
  }, [current, enabled, graph.nodes.length, key, topology]);

  return current ?? null;
}
