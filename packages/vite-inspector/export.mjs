import { constants } from 'node:fs';
import { open, realpath, rename, unlink } from 'node:fs/promises';
import { isAbsolute, join, relative, resolve, sep } from 'node:path';

const maxBytes = 4 << 20;

// Cooperating exporters serialize each map using its exclusive temporary
// name. Consumers only see the committed name after the flushed file moves.
export async function exportManifest(directory, sourceRoot, manifest) {
  if (!isAbsolute(directory) || resolve(directory) !== directory || await realpath(directory) !== directory)
    throw new Error('Manifest output directory must be canonical');
  const fromSource = relative(sourceRoot, directory);
  const fromOutput = relative(directory, sourceRoot);
  const inside = path => !isAbsolute(path) && path !== '..' && !path.startsWith('..' + sep);
  if (inside(fromSource) || inside(fromOutput))
    throw new Error('Manifest output must be outside the target root');

  if (!/^[a-f0-9]{64}$/.test(manifest.build_map_id)) throw new Error('Invalid build map ID');
  const body = Buffer.from(JSON.stringify(manifest) + '\n');
  if (body.length > maxBytes) throw new Error('Manifest exceeds export budget');
  const dir = await open(directory, constants.O_RDONLY | constants.O_DIRECTORY | constants.O_NOFOLLOW);
  try {
    const metadata = await dir.stat();
    if (typeof process.getuid !== 'function' || metadata.uid !== process.getuid() || metadata.mode & 0o077)
      throw new Error('Manifest output directory must be owner-only');
    const held = '/proc/self/fd/' + dir.fd;
    if (await realpath(held) !== directory) throw new Error('Manifest output moved');
    const filename = manifest.build_map_id + '.json';
    const final = join(held, filename);
    const temporary = final + '.tmp';
    if (await committed(final, body, metadata.uid)) {
      await dir.sync();
      if (await realpath(held) !== directory) throw new Error('Manifest output moved');
      return join(directory, filename);
    }
    const handle = await open(temporary, 'wx', 0o600);
    let published = false;
    try {
      try {
        await handle.writeFile(body);
        await handle.sync();
      } finally { await handle.close(); }
      if (await realpath(held) !== directory) throw new Error('Manifest output moved');
      if (!await committed(final, body, metadata.uid)) {
        await rename(temporary, final);
        published = true;
      }
    } finally {
      if (!published) await unlink(temporary);
    }
    await dir.sync();
    if (await realpath(held) !== directory) throw new Error('Manifest output moved');
    return join(directory, filename);
  } finally { await dir.close(); }
}

async function committed(path, body, uid) {
  let file;
  try {
    file = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
  } catch (error) {
    if (error?.code === 'ENOENT') return false;
    throw error;
  }
  try {
    const status = await file.stat();
    if (!status.isFile() || status.size !== body.length || status.uid !== uid || status.mode & 0o077 || status.nlink !== 1)
      throw new Error('Committed build manifest differs');
    const known = Buffer.alloc(body.length + 1);
    let length = 0;
    while (length < known.length) {
      const { bytesRead } = await file.read(known, length, known.length - length, length);
      if (!bytesRead) break;
      length += bytesRead;
    }
    if (!known.subarray(0, length).equals(body)) throw new Error('Committed build manifest differs');
    return true;
  } finally { await file.close(); }
}
