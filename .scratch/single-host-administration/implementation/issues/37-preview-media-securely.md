# 37 — Preview media securely

**What to build:** Generic Media preview for images, video, audio, sandboxed PDF, text, JSON, and unknown binary.

**Blocked by:** 31 — Browse Files under UNIX authority

**Status:** completed

- [x] HTML never renders inline; SVG/PDF active content is isolated or downloaded; `nosniff` is set.
- [x] Heavy preview components load conditionally and remain split into focused files.
- [x] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
