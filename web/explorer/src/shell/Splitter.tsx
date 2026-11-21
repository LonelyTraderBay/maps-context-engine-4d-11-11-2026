import { useRef } from 'react';
import type { KeyboardEvent, PointerEvent } from 'react';

const minimum = 30;
const maximum = 70;
const limit = (value: number) => Math.min(maximum, Math.max(minimum, Math.round(value)));

export function Splitter({ value, onChange }: { value: number; onChange: (width: number) => void }) {
  const drag = useRef<{ id: number; left: number; width: number } | null>(null);

  function point(event: PointerEvent<HTMLDivElement>) {
    const active = drag.current;
    if (active?.id !== event.pointerId) return;
    onChange(limit((event.clientX - active.left) * 100 / active.width));
  }

  function key(event: KeyboardEvent<HTMLDivElement>) {
    const step = event.shiftKey ? 10 : 2;
    let next: number;
    switch (event.key) {
      case 'ArrowLeft': next = value - step; break;
      case 'ArrowRight': next = value + step; break;
      case 'Home': next = minimum; break;
      case 'End': next = maximum; break;
      default: return;
    }
    event.preventDefault();
    onChange(limit(next));
  }

  return <div className="splitter" role="separator" aria-label="Độ rộng đồ thị"
    aria-orientation="vertical" aria-valuemin={minimum} aria-valuemax={maximum} aria-valuenow={value}
    aria-valuetext={value + '%'} aria-controls="graph-pane" tabIndex={0}
    onKeyDown={key} onPointerDown={event => {
      if (event.button !== 0 || !event.isPrimary) return;
      const container = event.currentTarget.parentElement;
      if (!container) return;
      const rect = container.getBoundingClientRect();
      if (rect.width <= 0) return;
      event.currentTarget.focus();
      event.currentTarget.setPointerCapture(event.pointerId);
      drag.current = { id: event.pointerId, left: rect.left, width: rect.width };
    }} onPointerMove={point}
    onPointerUp={event => {
      point(event);
      drag.current = null;
    }} onPointerCancel={() => { drag.current = null; }}
    onLostPointerCapture={() => { drag.current = null; }}>
    <span aria-hidden="true" />
  </div>;
}
