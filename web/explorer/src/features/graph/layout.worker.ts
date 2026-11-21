import { layout } from './layout.ts';
import type { LayoutInput, LayoutResult } from './layout.ts';

self.onmessage = (event: MessageEvent<LayoutInput>) => {
  let result: LayoutResult;
  try {
    result = { positions: layout(event.data) };
  } catch {
    result = { error: true };
  }
  self.postMessage(result);
};
