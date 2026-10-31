# Third-party notices

CatTV is Apache-2.0. Its Go module dependencies are distributed under the
following licenses. This file is informational; the license text shipped with
each module controls.

## Go standard library

The Go standard library is distributed under the BSD 3-Clause license and is
compiled into CatTV binaries. Copyright 2009 The Go Authors. See
<https://go.dev/LICENSE> for the full text.

## golang.org/x/image

- Version: v0.47.0
- Use: WebP decoding and high-quality image scaling (`draw.CatmullRom`)
- License: BSD 3-Clause
- Upstream: <https://go.googlesource.com/image/>
- License text: <https://cs.opensource.google/go/x/image/+/master:LICENSE>

## golang.org/x/sys

- Version: v0.49.0
- Use: terminal window-size ioctl and polling
- License: BSD 3-Clause
- Upstream: <https://go.googlesource.com/sys/>
- License text: <https://cs.opensource.google/go/x/sys/+/master:LICENSE>

## golang.org/x/term

- Version: v0.47.0
- Use: TTY detection, terminal size, and raw mode for capability probing
- License: BSD 3-Clause
- Upstream: <https://go.googlesource.com/term/>
- License text: <https://cs.opensource.google/go/x/term/+/master:LICENSE>

## BSD 3-Clause license text

```text
Copyright (c) 2009 The Go Authors. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

   * Redistributions of source code must retain the above copyright notice,
     this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above copyright
     notice, this list of conditions and the following disclaimer in the
     documentation and/or other materials provided with the distribution.
   * Neither the name of Google LLC nor the names of its contributors may be
     used to endorse or promote products derived from this software without
     specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT OWNER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```
