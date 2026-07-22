#ifndef RENART_MINGW_COMPAT_SHLOBJ_H
#define RENART_MINGW_COMPAT_SHLOBJ_H

// Arrow ADBC uses the Windows SDK filename casing. Linux-hosted MinGW
// toolchains install the same header with a lowercase filename.
#include <shlobj.h>

#endif
