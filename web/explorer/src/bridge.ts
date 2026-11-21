import validate from '../.generated/validators.cjs';
import { bridgeDirections } from '../.generated/wire.ts';
import type { BridgeMessage, InstanceRef, Rect } from '../.generated/types';
import { parseJSON, utf8 } from './json.ts';

export type BridgeDirection = keyof typeof bridgeDirections;
export const bridgeByteLimit = 16 * 1024;

function isIdentityNumber(path: readonly (string | number)[]): boolean {
  const key = path.at(-1);
  return !(path[0] === 'payload' && (
    path.length === 2 && (key === 'x_css' || key === 'y_css') ||
    path.at(-3) === 'rects' && (key === 'x' || key === 'y' || key === 'width' || key === 'height')
  ));
}

export function decodeBridgeMessage(
  input: Uint8Array | string,
  direction: BridgeDirection,
  cssMagnitude: number,
): BridgeMessage {
  if (!Number.isFinite(cssMagnitude) || cssMagnitude <= 0) throw new Error('Invalid CSS coordinate bound');
  if (typeof input === 'string' && (input.length > bridgeByteLimit || !input.isWellFormed())) {
    throw new Error('Invalid bridge JSON text');
  }
  const value = parseJSON(typeof input === 'string' ? utf8.encode(input) : input, bridgeByteLimit, isIdentityNumber);
  if (!validate.BridgeMessage(value)) throw new Error('Invalid bridge message');
  const allowed: readonly string[] | undefined = Object.hasOwn(bridgeDirections, direction)
    ? bridgeDirections[direction] : undefined;
  if (!allowed?.includes(value.type)) throw new Error('Bridge direction rejected');
  const { session_id, document_epoch, hmr_epoch } = value;

  function instance(ref: InstanceRef | null): void {
    if (ref && (ref.session_id !== session_id || ref.document_epoch !== document_epoch ||
        ref.hmr_epoch !== hmr_epoch)) {
      throw new Error('Stale bridge instance');
    }
  }
  function coordinate(n: number): void {
    if (Math.abs(n) > cssMagnitude) throw new Error('CSS coordinate outside bounds');
  }
  function rectangles(rects: Rect[]): void {
    for (const { x, y, width, height } of rects) {
      coordinate(x);
      coordinate(y);
      coordinate(width);
      coordinate(height);
    }
  }
  switch (value.type) {
    case 'PICK_AT':
      coordinate(value.payload.x_css);
      coordinate(value.payload.y_css);
      break;
    case 'PICK_RESULT':
      for (const target of value.payload.targets) {
        instance(target.instance_ref);
        instance(target.parent_instance_ref);
        rectangles(target.rects);
      }
      break;
    case 'INSTANCES':
      for (const target of value.payload.items) {
        instance(target.instance_ref);
        rectangles(target.rects);
      }
      break;
  }
  return value;
}
