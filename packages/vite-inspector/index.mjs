import { constants } from 'node:fs';
import { open, realpath } from 'node:fs/promises';
import { isAbsolute, relative, resolve, sep } from 'node:path';
import { createBuildManifest, instrumentSource } from './producer.mjs';
import { exportManifest } from './export.mjs';

// This opt-in plugin belongs to the user's dev process. The indexer consumes
// its manifest as read-only data and never loads the target's Vite config.
export function viteInspector(options) {
  const config = Object.freeze({ ...options });
  const modules = new Map();
  const pending = new Map();
  let root = null;
  let active = false;
  let mode = null;
  let anchorCount = 0;

  function modulePath(filename) {
    if (root === null || !isAbsolute(filename) || !/\.[jt]sx$/.test(filename) || resolve(filename) !== filename)
      return null;
    const rel = relative(root, filename);
    if (!rel || isAbsolute(rel) || rel.startsWith('..' + sep) || rel.split(sep).includes('node_modules'))
      return null;
    return rel.split(sep).join('/');
  }

  async function matchesOriginal(filename, code) {
    if (code.length > 128 << 10) return false;
    try {
      if (await realpath(filename) !== filename) return false;
      const file = await open(filename, constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
      try {
        const metadata = await file.stat();
        if (!metadata.isFile() || metadata.nlink !== 1 || metadata.size > 128 << 10) return false;
        const expected = Buffer.from(code, 'utf8');
        if (metadata.size !== expected.length) return false;
        const data = Buffer.alloc(expected.length + 1);
        let length = 0;
        while (length < data.length) {
          const { bytesRead } = await file.read(data, length, data.length - length, length);
          if (bytesRead === 0) break;
          length += bytesRead;
        }
        return data.subarray(0, length).equals(expected) && await realpath(filename) === filename;
      } finally {
        await file.close();
      }
    } catch (error) {
      if (['ENOENT', 'ENOTDIR', 'ELOOP'].includes(error?.code)) return false;
      throw error;
    }
  }

  return {
    name: 'mce4d-vite-inspector',
    enforce: 'pre',
    async configResolved(vite) {
      active = config.enabled === true && vite.command === 'serve' && vite.isProduction === false &&
        (vite.mode === 'development' || vite.mode === 'test') && !vite.build.ssr;
      mode = vite.mode;
      if (!active) return;
      root = await realpath(config.root);
      if (root !== resolve(config.root) || root !== await realpath(vite.root))
        throw new Error('Inspector requires its approved Vite root');
      await createBuildManifest({ ...config, mode }, modules);
    },
    async transform(code, filename, phase) {
      if (!active || phase?.ssr) return null;
      const path = modulePath(filename);
      if (path === null) return null;
      const marker = {};
      if (pending.size >= 64 && !pending.has(path)) throw new Error('Inspector transform limit');
      pending.set(path, marker);
      try {
        if (!await matchesOriginal(filename, code)) return null;
        const value = await instrumentSource(path, code, config.providerDigest);
        if (pending.get(path) !== marker) return null;
        const before = modules.get(path);
        const proposed = anchorCount - (before?.anchors.length ?? 0) + value.anchors.length;
        if (proposed > 8192 || !before && modules.size >= 4096) throw new Error('Inspector build coverage limit');
        modules.set(path, { source: value.source, anchors: value.anchors });
        anchorCount = proposed;
        return value.transformed;
      } finally {
        if (pending.get(path) === marker) pending.delete(path);
      }
    },
    handleHotUpdate({ file }) {
      if (!active) return;
      const path = modulePath(file);
      if (path === null) return;
      pending.delete(path);
      const previous = modules.get(path);
      if (previous) {
        modules.delete(path);
        anchorCount -= previous.anchors.length;
      }
    },
    api: {
      async buildManifest() {
        if (!active) return null;
        return createBuildManifest({ ...config, mode }, modules);
      },
      async exportManifest(directory) {
        if (!active) return null;
        return exportManifest(directory, root, await createBuildManifest({ ...config, mode }, modules));
      },
    },
  };
}
