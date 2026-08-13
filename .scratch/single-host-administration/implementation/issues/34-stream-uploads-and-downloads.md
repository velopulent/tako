# 34 — Stream uploads and downloads

**What to build:** Streaming downloads and resumable bounded uploads with disk preflight, checksums, cancellation, and recovery.

**Blocked by:** 08 — Run durable diagnostic inventory jobs; 31 — Browse Files under UNIX authority

**Status:** ready-for-agent

- [ ] Gateway never buffers whole files and every chunk/request rechecks authority.
- [ ] Range-independent download and upload restart behavior is integration tested.
- [ ] API changes, user-visible behavior, and applicable unit/UI/VM integration seams are tested and documented.
