// Lightweight Python dependency helpers for notebooks: parse a cell's imports,
// parse a requirements.txt, and figure out which imported packages are not yet
// declared. This is a heuristic (no real Python parser), good enough to power a
// "package imported but missing" suggestion.

// Top-level Python 3.11 standard-library modules — never flagged as missing.
const STDLIB = new Set<string>([
  "__future__", "abc", "aifc", "argparse", "array", "ast", "asyncio", "atexit",
  "base64", "bdb", "binascii", "bisect", "builtins", "bz2", "calendar", "cgi",
  "cgitb", "chunk", "cmath", "cmd", "code", "codecs", "codeop", "collections",
  "colorsys", "compileall", "concurrent", "configparser", "contextlib",
  "contextvars", "copy", "copyreg", "csv", "ctypes", "curses", "dataclasses",
  "datetime", "dbm", "decimal", "difflib", "dis", "doctest", "email", "encodings",
  "ensurepip", "enum", "errno", "faulthandler", "fcntl", "filecmp", "fileinput",
  "fnmatch", "fractions", "ftplib", "functools", "gc", "getopt", "getpass",
  "gettext", "glob", "graphlib", "gzip", "hashlib", "heapq", "hmac", "html",
  "http", "imaplib", "imghdr", "importlib", "inspect", "io", "ipaddress",
  "itertools", "json", "keyword", "linecache", "locale", "logging", "lzma",
  "mailbox", "marshal", "math", "mimetypes", "mmap", "modulefinder", "multiprocessing",
  "netrc", "numbers", "operator", "os", "pathlib", "pdb", "pickle", "pickletools",
  "pkgutil", "platform", "plistlib", "poplib", "posixpath", "pprint", "profile",
  "pstats", "pty", "pwd", "py_compile", "pyclbr", "pydoc", "queue", "quopri",
  "random", "re", "readline", "reprlib", "resource", "runpy", "sched", "secrets",
  "select", "selectors", "shelve", "shlex", "shutil", "signal", "site", "smtplib",
  "sndhdr", "socket", "socketserver", "sqlite3", "ssl", "stat", "statistics",
  "string", "stringprep", "struct", "subprocess", "sunau", "symtable", "sys",
  "sysconfig", "syslog", "tabnanny", "tarfile", "telnetlib", "tempfile",
  "termios", "textwrap", "threading", "time", "timeit", "tkinter", "token",
  "tokenize", "tomllib", "trace", "traceback", "tracemalloc", "tty", "turtle",
  "types", "typing", "unicodedata", "unittest", "urllib", "uuid", "venv",
  "warnings", "wave", "weakref", "webbrowser", "wsgiref", "xdrlib", "xml",
  "xmlrpc", "zipapp", "zipfile", "zipimport", "zlib", "zoneinfo",
]);

// Import name → PyPI package name when they differ.
const IMPORT_TO_PACKAGE: Record<string, string> = {
  bruin: "bruin-sdk",
  bs4: "beautifulsoup4",
  cv2: "opencv-python",
  dateutil: "python-dateutil",
  dotenv: "python-dotenv",
  google: "google-cloud",
  jwt: "PyJWT",
  PIL: "Pillow",
  sklearn: "scikit-learn",
  yaml: "PyYAML",
};

/** Top-level module names imported by a Python source (relative imports skipped). */
export function parsePythonImports(source: string): string[] {
  const modules = new Set<string>();
  for (const rawLine of source.split("\n")) {
    const line = rawLine.split("#")[0].trim();
    const importMatch = line.match(/^import\s+(.+)$/);
    if (importMatch) {
      for (const part of importMatch[1].split(",")) {
        const module = part.trim().split(/\s+as\s+/)[0].trim().split(".")[0];
        if (module) {
          modules.add(module);
        }
      }
      continue;
    }
    const fromMatch = line.match(/^from\s+(\.*)([\w.]+)\s+import\s+/);
    if (fromMatch && !fromMatch[1]) {
      const module = fromMatch[2].split(".")[0];
      if (module) {
        modules.add(module);
      }
    }
  }
  return [...modules];
}

/** Package name from a PEP 508 dependency specifier (`pandas>=2.0` → `pandas`). */
export function dependencyName(spec: string): string {
  return spec.split(/[<>=!~[;( ]/)[0].trim();
}

// PEP 503 name normalization, so "scikit_learn" and "scikit-learn" match.
function canonical(name: string): string {
  return name.toLowerCase().replace(/[-_.]+/g, "-");
}

/**
 * Import names in `source` that do not resolve: not standard library, not
 * available in the notebook's virtualenv (`installedModules`, the ground truth
 * regardless of how the package is named), and not provided by a declared
 * dependency. Returns the import names — the picker searches PyPI to find the
 * package that provides each.
 */
export function missingPythonImports(
  source: string,
  declared: string[],
  installedModules: string[],
): string[] {
  const installed = new Set(installedModules);
  const declaredNames = new Set(declared.map((spec) => canonical(dependencyName(spec))));
  const missing: string[] = [];
  const seen = new Set<string>();
  for (const module of parsePythonImports(source)) {
    if (STDLIB.has(module) || installed.has(module) || seen.has(module)) {
      continue;
    }
    // Still satisfied if a declared package (by name or known alias) provides
    // it — covers a just-added dependency not yet installed into the venv.
    const declaredPackage = IMPORT_TO_PACKAGE[module] ?? module;
    if (declaredNames.has(canonical(declaredPackage))) {
      continue;
    }
    seen.add(module);
    missing.push(module);
  }
  return missing;
}

/** Add a dependency specifier to the list, skipping if already declared. */
export function addDependency(deps: string[], spec: string): string[] {
  const declared = new Set(deps.map((existing) => canonical(dependencyName(existing))));
  if (declared.has(canonical(dependencyName(spec)))) {
    return deps;
  }
  return [...deps, spec];
}
