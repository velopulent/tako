# ADR 0005: Adaptive in-memory observability

Status: accepted

Tako retains resource samples in memory for a configurable rolling window, 24 hours by default. The configured interval is one minute. When browsers request faster streams, collection temporarily follows the fastest active interval and returns to the configured interval after those subscribers disconnect. Streams support 1 second, 5 second, 15 second, 30 second, 1 minute, and 5 minute cadences. History responses are downsampled to a bounded render set.

Samples contain aggregate and per-core CPU, load, memory and swap, block-device bytes, and aggregate/per-interface network bytes. Rates are derived from consecutive monotonic counters. Browser preferences change real collection demand rather than merely hiding samples.

Per-process network accounting is an optional capability. Its future CO-RE eBPF monitor must run as a separate, on-demand, privilege-bounded helper and detach when unused. Hosts without BTF or required kernel capabilities retain procfs process monitoring and expose a clear capability reason.
