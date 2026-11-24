"""Bootstrap the pinned Windows embedded runtime for the shared AST worker."""
import os
import sys

home = os.path.dirname(os.path.realpath(sys.executable))
normalize = lambda path: os.path.normcase(os.path.normpath(path))
expected = [normalize(os.path.join(home, "python314.zip")), normalize(home)]
if ([normalize(item) for item in sys.path] != expected or not sys.flags.isolated
        or not sys.flags.no_site or not sys.dont_write_bytecode):
    raise SystemExit(1)

import ast
import hashlib
import json
import re
from builtins import open as runtime_open

for module in tuple(sys.modules.values()):
    name = getattr(module, "__file__", None)
    if name and module is not sys.modules["__main__"]:
        if normalize(os.path.commonpath([home, os.path.realpath(name)])) != normalize(home):
            raise SystemExit(1)


def open(path, mode):
    # The coordinator uses a local NTFS scratch root. The extended path
    # spelling also works when capture paths exceed the Win32 MAX_PATH limit.
    root = normalize(os.getcwd())
    full = os.path.abspath(path)
    if mode != "rb" or normalize(os.path.commonpath([root, full])) != root:
        raise ValueError()
    return runtime_open("\\\\?\\" + full, mode)
